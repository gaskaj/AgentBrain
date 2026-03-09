package resource

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/parquet-go/parquet-go"
)

// HTTPClientPoolImpl implements HTTPClientPool for managing HTTP client connections
type HTTPClientPoolImpl struct {
	config    PoolConfig
	clients   chan *http.Client
	active    map[*http.Client]time.Time
	metrics   PoolHealth
	logger    *slog.Logger
	mu        sync.RWMutex
	stopCh    chan struct{}
	doneCh    chan struct{}
	closed    bool
}

// NewHTTPClientPool creates a new HTTP client pool
func NewHTTPClientPool(config PoolConfig, logger *slog.Logger) (*HTTPClientPoolImpl, error) {
	if config.MaxSize <= 0 {
		config.MaxSize = 20
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 5 * time.Minute
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 1 * time.Minute
	}

	pool := &HTTPClientPoolImpl{
		config:  config,
		clients: make(chan *http.Client, config.MaxSize),
		active:  make(map[*http.Client]time.Time),
		logger:  logger,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
		metrics: PoolHealth{
			MaxSize:   config.MaxSize,
			LastReset: time.Now(),
		},
	}

	// Pre-populate with initial clients
	initialSize := config.InitialSize
	if initialSize == 0 {
		initialSize = config.MaxSize / 4
		if initialSize < 1 {
			initialSize = 1
		}
	}

	for i := 0; i < initialSize; i++ {
		client := pool.createClient()
		select {
		case pool.clients <- client:
			pool.metrics.Idle++
		default:
			break
		}
	}

	// Start cleanup routine
	go pool.cleanupLoop()

	logger.Info("HTTP client pool created", 
		"max_size", config.MaxSize,
		"initial_size", initialSize,
		"idle_timeout", config.IdleTimeout)

	return pool, nil
}

// Acquire implements ResourcePool interface
func (p *HTTPClientPoolImpl) Acquire(ctx context.Context, priority Priority) (interface{}, error) {
	return p.AcquireHTTPClient(ctx, priority)
}

// AcquireHTTPClient gets an HTTP client from the pool
func (p *HTTPClientPoolImpl) AcquireHTTPClient(ctx context.Context, priority Priority) (*http.Client, error) {
	if p.closed {
		return nil, fmt.Errorf("pool is closed")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Try to get from pool first
	select {
	case client := <-p.clients:
		p.active[client] = time.Now()
		p.metrics.Active++
		p.metrics.Idle--
		p.metrics.Hits++
		return client, nil
	default:
		// Pool is empty
	}

	// Check if we can create new client
	if len(p.active)+len(p.clients) < p.config.MaxSize {
		client := p.createClient()
		p.active[client] = time.Now()
		p.metrics.Active++
		p.metrics.Misses++
		return client, nil
	}

	// Pool is full, wait with timeout based on priority
	timeout := 5 * time.Second
	switch priority {
	case PriorityCritical:
		timeout = 30 * time.Second
	case PriorityHigh:
		timeout = 15 * time.Second
	case PriorityNormal:
		timeout = 10 * time.Second
	case PriorityLow:
		timeout = 5 * time.Second
	}

	p.mu.Unlock()
	defer p.mu.Lock()

	select {
	case client := <-p.clients:
		p.active[client] = time.Now()
		p.metrics.Active++
		p.metrics.Idle--
		p.metrics.Hits++
		return client, nil
	case <-ctx.Done():
		p.metrics.Timeouts++
		return nil, ctx.Err()
	case <-time.After(timeout):
		p.metrics.Timeouts++
		return nil, fmt.Errorf("timeout acquiring HTTP client after %v", timeout)
	}
}

// Release implements ResourcePool interface
func (p *HTTPClientPoolImpl) Release(resource interface{}) {
	if client, ok := resource.(*http.Client); ok {
		p.ReleaseHTTPClient(client)
	}
}

// ReleaseHTTPClient returns an HTTP client to the pool
func (p *HTTPClientPoolImpl) ReleaseHTTPClient(client *http.Client) {
	if p.closed || client == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Remove from active tracking
	delete(p.active, client)
	p.metrics.Active--

	// Try to return to pool
	select {
	case p.clients <- client:
		p.metrics.Idle++
	default:
		// Pool is full, discard the client
	}
}

// Health returns pool health metrics
func (p *HTTPClientPoolImpl) Health() PoolHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()

	health := p.metrics
	health.Total = health.Active + health.Idle
	return health
}

// Close shuts down the pool and closes all clients
func (p *HTTPClientPoolImpl) Close() error {
	if p.closed {
		return nil
	}

	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()

	close(p.stopCh)
	<-p.doneCh

	// Close all clients
	close(p.clients)
	for client := range p.clients {
		p.closeClient(client)
	}

	p.mu.Lock()
	for client := range p.active {
		p.closeClient(client)
	}
	p.active = make(map[*http.Client]time.Time)
	p.mu.Unlock()

	p.logger.Info("HTTP client pool closed")
	return nil
}

func (p *HTTPClientPoolImpl) createClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

func (p *HTTPClientPoolImpl) closeClient(client *http.Client) {
	if client != nil && client.Transport != nil {
		if transport, ok := client.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}
}

func (p *HTTPClientPoolImpl) cleanupLoop() {
	defer close(p.doneCh)

	ticker := time.NewTicker(p.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.performCleanup()
		}
	}
}

func (p *HTTPClientPoolImpl) performCleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	
	// Clean up idle clients that have exceeded idle timeout
	currentIdle := len(p.clients)
	for i := 0; i < currentIdle; i++ {
		select {
		case client := <-p.clients:
			// For idle clients, we'll just check if we should keep them
			// In a more sophisticated implementation, we'd track when they were last used
			if p.config.MaxLifetime > 0 && time.Since(now) > p.config.MaxLifetime {
				p.closeClient(client)
				p.metrics.Idle--
			} else {
				// Put it back
				select {
				case p.clients <- client:
				default:
					// Pool somehow got full, discard
					p.closeClient(client)
					p.metrics.Idle--
				}
			}
		default:
			break
		}
	}
}

// ParquetWriterPoolImpl implements ParquetWriterPool for managing Parquet writers
type ParquetWriterPoolImpl struct {
	config    PoolConfig
	writers   map[string]chan *parquet.GenericWriter[any] // keyed by schema hash
	active    map[*parquet.GenericWriter[any]]time.Time
	metrics   PoolHealth
	logger    *slog.Logger
	mu        sync.RWMutex
	stopCh    chan struct{}
	doneCh    chan struct{}
	closed    bool
}

// NewParquetWriterPool creates a new Parquet writer pool
func NewParquetWriterPool(config PoolConfig, logger *slog.Logger) (*ParquetWriterPoolImpl, error) {
	if config.MaxSize <= 0 {
		config.MaxSize = 10
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 2 * time.Minute
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 30 * time.Second
	}

	pool := &ParquetWriterPoolImpl{
		config:  config,
		writers: make(map[string]chan *parquet.GenericWriter[any]),
		active:  make(map[*parquet.GenericWriter[any]]time.Time),
		logger:  logger,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
		metrics: PoolHealth{
			MaxSize:   config.MaxSize,
			LastReset: time.Now(),
		},
	}

	// Start cleanup routine
	go pool.cleanupLoop()

	logger.Info("Parquet writer pool created", "max_size", config.MaxSize)
	return pool, nil
}

// Acquire gets a Parquet writer from the pool for a specific schema
func (p *ParquetWriterPoolImpl) Acquire(ctx context.Context, schema *parquet.Schema) (*parquet.GenericWriter[any], error) {
	if p.closed {
		return nil, fmt.Errorf("pool is closed")
	}

	schemaKey := p.getSchemaKey(schema)
	
	p.mu.Lock()
	defer p.mu.Unlock()

	// Get or create channel for this schema
	writerChan, exists := p.writers[schemaKey]
	if !exists {
		writerChan = make(chan *parquet.GenericWriter[any], p.config.MaxSize)
		p.writers[schemaKey] = writerChan
	}

	// Try to get from pool first
	select {
	case writer := <-writerChan:
		p.active[writer] = time.Now()
		p.metrics.Active++
		p.metrics.Idle--
		p.metrics.Hits++
		return writer, nil
	default:
		// Pool is empty
	}

	// Check if we can create new writer
	totalActive := len(p.active)
	totalIdle := 0
	for _, ch := range p.writers {
		totalIdle += len(ch)
	}
	
	if totalActive+totalIdle < p.config.MaxSize {
		writer := p.createWriter(schema)
		if writer != nil {
			p.active[writer] = time.Now()
			p.metrics.Active++
			p.metrics.Misses++
			return writer, nil
		}
	}

	p.metrics.Errors++
	return nil, fmt.Errorf("unable to acquire parquet writer: pool full or creation failed")
}

// Release returns a Parquet writer to the pool
func (p *ParquetWriterPoolImpl) Release(writer *parquet.GenericWriter[any]) {
	if p.closed || writer == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Remove from active tracking
	delete(p.active, writer)
	p.metrics.Active--

	// We can't easily determine which schema channel this belongs to,
	// so we'll just close the writer. In a real implementation, you'd
	// track the schema association.
	p.closeWriter(writer)
}

// Health returns pool health metrics
func (p *ParquetWriterPoolImpl) Health() PoolHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()

	health := p.metrics
	health.Total = health.Active + health.Idle
	return health
}

// Close shuts down the pool and closes all writers
func (p *ParquetWriterPoolImpl) Close() error {
	if p.closed {
		return nil
	}

	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()

	close(p.stopCh)
	<-p.doneCh

	// Close all writers
	for _, writerChan := range p.writers {
		close(writerChan)
		for writer := range writerChan {
			p.closeWriter(writer)
		}
	}

	p.mu.Lock()
	for writer := range p.active {
		p.closeWriter(writer)
	}
	p.active = make(map[*parquet.GenericWriter[any]]time.Time)
	p.mu.Unlock()

	p.logger.Info("Parquet writer pool closed")
	return nil
}

func (p *ParquetWriterPoolImpl) createWriter(schema *parquet.Schema) *parquet.GenericWriter[any] {
	// In a real implementation, you'd create a writer with a buffer
	// For now, return nil as we can't create a writer without output
	return nil
}

func (p *ParquetWriterPoolImpl) closeWriter(writer *parquet.GenericWriter[any]) {
	if writer != nil {
		_ = writer.Close() // Best effort close
	}
}

func (p *ParquetWriterPoolImpl) getSchemaKey(schema *parquet.Schema) string {
	// Simple schema key generation - in reality you'd want a proper hash
	return fmt.Sprintf("schema_%p", schema)
}

func (p *ParquetWriterPoolImpl) cleanupLoop() {
	defer close(p.doneCh)

	ticker := time.NewTicker(p.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.performCleanup()
		}
	}
}

func (p *ParquetWriterPoolImpl) performCleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Clean up idle writers that have exceeded idle timeout
	for schemaKey, writerChan := range p.writers {
		currentIdle := len(writerChan)
		for i := 0; i < currentIdle; i++ {
			select {
			case writer := <-writerChan:
				// Close idle writers (simplified cleanup)
				p.closeWriter(writer)
				p.metrics.Idle--
			default:
				break
			}
		}
		
		// Remove empty channels
		if len(writerChan) == 0 {
			delete(p.writers, schemaKey)
		}
	}
}