package profiler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"
)

// ProfilerConfig holds profiler configuration
type ProfilerConfig struct {
	Enabled              bool          `yaml:"enabled"`
	SampleRate           float64       `yaml:"sample_rate"`
	OutputDir            string        `yaml:"output_dir"`
	CPUProfileDuration   time.Duration `yaml:"cpu_profile_duration"`
	MemoryProfileInterval time.Duration `yaml:"memory_profile_interval"`
	GoroutineThreshold   int           `yaml:"goroutine_threshold"`
}

// Profiler provides performance profiling capabilities
type Profiler struct {
	config   ProfilerConfig
	cpuFile  *os.File
	monitor  *ResourceMonitor
	analytics *Analytics
	mu       sync.RWMutex
	running  bool
	stopChan chan struct{}
}

// New creates a new Profiler instance
func New(config ProfilerConfig) (*Profiler, error) {
	if !config.Enabled {
		return &Profiler{config: config}, nil
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("create profiler output directory: %w", err)
	}

	monitor := NewResourceMonitor(config)
	analytics := NewAnalytics(config)

	return &Profiler{
		config:    config,
		monitor:   monitor,
		analytics: analytics,
		stopChan:  make(chan struct{}),
	}, nil
}

// Start begins profiling operations
func (p *Profiler) Start(ctx context.Context) error {
	if !p.config.Enabled {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("profiler is already running")
	}

	// Start resource monitoring
	if err := p.monitor.Start(ctx); err != nil {
		return fmt.Errorf("start resource monitor: %w", err)
	}

	// Start analytics
	if err := p.analytics.Start(ctx); err != nil {
		return fmt.Errorf("start analytics: %w", err)
	}

	p.running = true

	// Start periodic memory profiling
	go p.periodicMemoryProfiling(ctx)

	// Start goroutine monitoring
	go p.monitorGoroutines(ctx)

	return nil
}

// Stop ends profiling operations
func (p *Profiler) Stop() error {
	if !p.config.Enabled {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	close(p.stopChan)

	// Stop CPU profiling if active
	if err := p.StopCPUProfile(); err != nil {
		return fmt.Errorf("stop CPU profile: %w", err)
	}

	// Stop resource monitoring
	if err := p.monitor.Stop(); err != nil {
		return fmt.Errorf("stop resource monitor: %w", err)
	}

	// Stop analytics
	if err := p.analytics.Stop(); err != nil {
		return fmt.Errorf("stop analytics: %w", err)
	}

	p.running = false
	return nil
}

// StartCPUProfile begins CPU profiling
func (p *Profiler) StartCPUProfile() error {
	if !p.config.Enabled {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cpuFile != nil {
		return fmt.Errorf("CPU profiling is already active")
	}

	filename := filepath.Join(p.config.OutputDir, fmt.Sprintf("cpu-%d.prof", time.Now().Unix()))
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("create CPU profile file: %w", err)
	}

	if err := pprof.StartCPUProfile(file); err != nil {
		file.Close()
		return fmt.Errorf("start CPU profiling: %w", err)
	}

	p.cpuFile = file
	return nil
}

// StopCPUProfile ends CPU profiling
func (p *Profiler) StopCPUProfile() error {
	if !p.config.Enabled {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cpuFile == nil {
		return nil
	}

	pprof.StopCPUProfile()
	if err := p.cpuFile.Close(); err != nil {
		return fmt.Errorf("close CPU profile file: %w", err)
	}

	p.cpuFile = nil
	return nil
}

// CaptureMemProfile captures a memory profile
func (p *Profiler) CaptureMemProfile() error {
	if !p.config.Enabled {
		return nil
	}

	filename := filepath.Join(p.config.OutputDir, fmt.Sprintf("mem-%d.prof", time.Now().Unix()))
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("create memory profile file: %w", err)
	}
	defer file.Close()

	runtime.GC() // Force GC before capturing memory profile
	if err := pprof.WriteHeapProfile(file); err != nil {
		return fmt.Errorf("write heap profile: %w", err)
	}

	return nil
}

// CaptureGoroutineProfile captures a goroutine profile
func (p *Profiler) CaptureGoroutineProfile() error {
	if !p.config.Enabled {
		return nil
	}

	filename := filepath.Join(p.config.OutputDir, fmt.Sprintf("goroutine-%d.prof", time.Now().Unix()))
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("create goroutine profile file: %w", err)
	}
	defer file.Close()

	profile := pprof.Lookup("goroutine")
	if profile == nil {
		return fmt.Errorf("goroutine profile not available")
	}

	if err := profile.WriteTo(file, 0); err != nil {
		return fmt.Errorf("write goroutine profile: %w", err)
	}

	return nil
}

// GetResourceMonitor returns the resource monitor instance
func (p *Profiler) GetResourceMonitor() *ResourceMonitor {
	return p.monitor
}

// GetAnalytics returns the analytics instance
func (p *Profiler) GetAnalytics() *Analytics {
	return p.analytics
}

// TrackOperation tracks a generic operation
func (p *Profiler) TrackOperation(name string, duration time.Duration, metadata map[string]interface{}) {
	if !p.config.Enabled {
		return
	}

	p.analytics.TrackOperation(name, duration, metadata)
}

// periodicMemoryProfiling captures memory profiles at regular intervals
func (p *Profiler) periodicMemoryProfiling(ctx context.Context) {
	ticker := time.NewTicker(p.config.MemoryProfileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopChan:
			return
		case <-ticker.C:
			if err := p.CaptureMemProfile(); err != nil {
				// Log error but continue profiling
				fmt.Printf("Error capturing memory profile: %v\n", err)
			}
		}
	}
}

// monitorGoroutines monitors goroutine count and alerts when threshold is exceeded
func (p *Profiler) monitorGoroutines(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopChan:
			return
		case <-ticker.C:
			numGoroutines := runtime.NumGoroutine()
			if numGoroutines > p.config.GoroutineThreshold {
				// Capture goroutine profile for analysis
				if err := p.CaptureGoroutineProfile(); err != nil {
					fmt.Printf("Error capturing goroutine profile: %v\n", err)
				}
				fmt.Printf("Warning: High goroutine count detected: %d (threshold: %d)\n", 
					numGoroutines, p.config.GoroutineThreshold)
			}
		}
	}
}

// IsEnabled returns whether profiling is enabled
func (p *Profiler) IsEnabled() bool {
	return p.config.Enabled
}

// IsRunning returns whether profiling is currently running
func (p *Profiler) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}