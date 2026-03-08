package profiler

import (
	"context"
	"runtime"
	"sync"
	"time"
)

// ResourceMetrics holds resource usage metrics
type ResourceMetrics struct {
	Timestamp       time.Time
	MemoryUsage     MemoryMetrics
	GoroutineCount  int
	CPUUsage        float64
	DiskIOBytes     uint64
	NetworkIOBytes  uint64
}

// MemoryMetrics holds memory-related metrics
type MemoryMetrics struct {
	HeapAlloc      uint64  // bytes allocated and not yet freed
	HeapSys        uint64  // bytes obtained from system
	HeapIdle       uint64  // bytes in idle spans
	HeapInuse      uint64  // bytes in non-idle span
	HeapReleased   uint64  // bytes released to the OS
	HeapObjects    uint64  // total number of allocated objects
	StackInuse     uint64  // bytes used by stack spans
	StackSys       uint64  // bytes obtained from system for stack
	MSpanInuse     uint64  // bytes used by mspan structures
	MSpanSys       uint64  // bytes obtained from system for mspan
	MCacheInuse    uint64  // bytes used by mcache structures
	MCacheSys      uint64  // bytes obtained from system for mcache
	GCSys          uint64  // bytes used for garbage collection system metadata
	OtherSys       uint64  // bytes used for other system allocations
	NextGC         uint64  // target heap size of next GC cycle
	LastGC         time.Time // time of last garbage collection
	NumGC          uint32  // number of completed GC cycles
	GCPauseTotal   time.Duration // total pause time in GC
}

// ResourceMonitor monitors system resource usage
type ResourceMonitor struct {
	config   ProfilerConfig
	metrics  []ResourceMetrics
	mu       sync.RWMutex
	running  bool
	stopChan chan struct{}
}

// NewResourceMonitor creates a new resource monitor
func NewResourceMonitor(config ProfilerConfig) *ResourceMonitor {
	return &ResourceMonitor{
		config:   config,
		metrics:  make([]ResourceMetrics, 0, 1000), // Pre-allocate for performance
		stopChan: make(chan struct{}),
	}
}

// Start begins resource monitoring
func (rm *ResourceMonitor) Start(ctx context.Context) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.running {
		return nil
	}

	rm.running = true
	go rm.collectMetrics(ctx)
	return nil
}

// Stop ends resource monitoring
func (rm *ResourceMonitor) Stop() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if !rm.running {
		return nil
	}

	close(rm.stopChan)
	rm.running = false
	return nil
}

// GetCurrentMetrics returns the current resource metrics
func (rm *ResourceMonitor) GetCurrentMetrics() ResourceMetrics {
	return rm.collectCurrentMetrics()
}

// GetMetricsHistory returns the collected metrics history
func (rm *ResourceMonitor) GetMetricsHistory() []ResourceMetrics {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// Return a copy to prevent race conditions
	history := make([]ResourceMetrics, len(rm.metrics))
	copy(history, rm.metrics)
	return history
}

// GetMemoryTrend analyzes memory usage trends
func (rm *ResourceMonitor) GetMemoryTrend(duration time.Duration) MemoryTrend {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	cutoff := time.Now().Add(-duration)
	var samples []ResourceMetrics

	for i := len(rm.metrics) - 1; i >= 0; i-- {
		if rm.metrics[i].Timestamp.Before(cutoff) {
			break
		}
		samples = append([]ResourceMetrics{rm.metrics[i]}, samples...)
	}

	return rm.analyzeMemoryTrend(samples)
}

// MemoryTrend represents memory usage trend analysis
type MemoryTrend struct {
	Direction    string    // "increasing", "decreasing", "stable"
	Rate         float64   // bytes per second
	Peak         uint64    // peak memory usage
	Average      uint64    // average memory usage
	Current      uint64    // current memory usage
	LastUpdated  time.Time
}

// collectMetrics runs the metrics collection loop
func (rm *ResourceMonitor) collectMetrics(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second) // Collect metrics every 10 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rm.stopChan:
			return
		case <-ticker.C:
			metrics := rm.collectCurrentMetrics()
			rm.addMetrics(metrics)
		}
	}
}

// collectCurrentMetrics collects current system metrics
func (rm *ResourceMonitor) collectCurrentMetrics() ResourceMetrics {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return ResourceMetrics{
		Timestamp:      time.Now(),
		GoroutineCount: runtime.NumGoroutine(),
		MemoryUsage: MemoryMetrics{
			HeapAlloc:    memStats.HeapAlloc,
			HeapSys:      memStats.HeapSys,
			HeapIdle:     memStats.HeapIdle,
			HeapInuse:    memStats.HeapInuse,
			HeapReleased: memStats.HeapReleased,
			HeapObjects:  memStats.HeapObjects,
			StackInuse:   memStats.StackInuse,
			StackSys:     memStats.StackSys,
			MSpanInuse:   memStats.MSpanInuse,
			MSpanSys:     memStats.MSpanSys,
			MCacheInuse:  memStats.MCacheInuse,
			MCacheSys:    memStats.MCacheSys,
			GCSys:        memStats.GCSys,
			OtherSys:     memStats.OtherSys,
			NextGC:       memStats.NextGC,
			LastGC:       time.Unix(0, int64(memStats.LastGC)),
			NumGC:        memStats.NumGC,
			GCPauseTotal: time.Duration(memStats.PauseTotalNs),
		},
	}
}

// addMetrics adds metrics to the history
func (rm *ResourceMonitor) addMetrics(metrics ResourceMetrics) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.metrics = append(rm.metrics, metrics)

	// Keep only last 1000 entries to prevent unbounded growth
	if len(rm.metrics) > 1000 {
		rm.metrics = rm.metrics[len(rm.metrics)-1000:]
	}
}

// analyzeMemoryTrend analyzes memory usage trend from samples
func (rm *ResourceMonitor) analyzeMemoryTrend(samples []ResourceMetrics) MemoryTrend {
	if len(samples) == 0 {
		return MemoryTrend{Direction: "unknown"}
	}

	if len(samples) == 1 {
		return MemoryTrend{
			Direction:   "stable",
			Current:     samples[0].MemoryUsage.HeapAlloc,
			Average:     samples[0].MemoryUsage.HeapAlloc,
			Peak:        samples[0].MemoryUsage.HeapAlloc,
			LastUpdated: samples[0].Timestamp,
		}
	}

	// Calculate trend
	var totalMemory uint64
	var peak uint64
	first := samples[0]
	last := samples[len(samples)-1]

	for _, sample := range samples {
		totalMemory += sample.MemoryUsage.HeapAlloc
		if sample.MemoryUsage.HeapAlloc > peak {
			peak = sample.MemoryUsage.HeapAlloc
		}
	}

	average := totalMemory / uint64(len(samples))
	duration := last.Timestamp.Sub(first.Timestamp)
	
	var direction string
	var rate float64
	
	if duration > 0 {
		memoryDiff := int64(last.MemoryUsage.HeapAlloc) - int64(first.MemoryUsage.HeapAlloc)
		rate = float64(memoryDiff) / duration.Seconds()
		
		if rate > 100*1024 { // More than 100KB/s increase
			direction = "increasing"
		} else if rate < -100*1024 { // More than 100KB/s decrease
			direction = "decreasing"
		} else {
			direction = "stable"
		}
	} else {
		direction = "stable"
	}

	return MemoryTrend{
		Direction:   direction,
		Rate:        rate,
		Peak:        peak,
		Average:     average,
		Current:     last.MemoryUsage.HeapAlloc,
		LastUpdated: last.Timestamp,
	}
}

// DetectMemoryLeak analyzes metrics for potential memory leaks
func (rm *ResourceMonitor) DetectMemoryLeak(duration time.Duration, threshold float64) bool {
	trend := rm.GetMemoryTrend(duration)
	
	// Consider it a potential leak if memory is consistently increasing
	// at a rate above the threshold (bytes per second)
	return trend.Direction == "increasing" && trend.Rate > threshold
}

// IsRunning returns whether the monitor is currently running
func (rm *ResourceMonitor) IsRunning() bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.running
}