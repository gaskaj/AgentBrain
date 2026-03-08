package profiler

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewResourceMonitor(t *testing.T) {
	config := ProfilerConfig{
		Enabled:               true,
		MemoryProfileInterval: 1 * time.Second,
		GoroutineThreshold:    1000,
	}

	monitor := NewResourceMonitor(config)
	assert.NotNil(t, monitor)
	assert.False(t, monitor.IsRunning())
}

func TestResourceMonitorStartStop(t *testing.T) {
	config := ProfilerConfig{
		Enabled: true,
	}

	monitor := NewResourceMonitor(config)
	ctx := context.Background()

	// Test start
	err := monitor.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, monitor.IsRunning())

	// Test double start (should be no-op)
	err = monitor.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, monitor.IsRunning())

	// Test stop
	err = monitor.Stop()
	assert.NoError(t, err)
	assert.False(t, monitor.IsRunning())

	// Test double stop (should be no-op)
	err = monitor.Stop()
	assert.NoError(t, err)
	assert.False(t, monitor.IsRunning())
}

func TestGetCurrentMetrics(t *testing.T) {
	config := ProfilerConfig{Enabled: true}
	monitor := NewResourceMonitor(config)

	metrics := monitor.GetCurrentMetrics()
	
	// Verify basic metrics are populated
	assert.NotZero(t, metrics.Timestamp)
	assert.NotZero(t, metrics.MemoryUsage.HeapAlloc)
	assert.NotZero(t, metrics.MemoryUsage.HeapSys)
	assert.Greater(t, metrics.GoroutineCount, 0)
	
	// Verify timestamp is recent
	assert.WithinDuration(t, time.Now(), metrics.Timestamp, 1*time.Second)
}

func TestMetricsCollection(t *testing.T) {
	config := ProfilerConfig{Enabled: true}
	monitor := NewResourceMonitor(config)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := monitor.Start(ctx)
	require.NoError(t, err)

	// Wait for some metrics to be collected
	time.Sleep(50 * time.Millisecond)

	err = monitor.Stop()
	require.NoError(t, err)

	history := monitor.GetMetricsHistory()
	// Should have collected at least one sample (initial + periodic)
	assert.GreaterOrEqual(t, len(history), 1)
	
	for _, metrics := range history {
		assert.NotZero(t, metrics.MemoryUsage.HeapAlloc)
		assert.Greater(t, metrics.GoroutineCount, 0)
	}
}

func TestMemoryTrendAnalysis(t *testing.T) {
	config := ProfilerConfig{Enabled: true}
	monitor := NewResourceMonitor(config)

	// Add some test metrics manually for trend analysis
	baseTime := time.Now().Add(-1 * time.Hour)
	
	testMetrics := []ResourceMetrics{
		{
			Timestamp:      baseTime,
			MemoryUsage:    MemoryMetrics{HeapAlloc: 1024 * 1024}, // 1MB
			GoroutineCount: 10,
		},
		{
			Timestamp:      baseTime.Add(30 * time.Minute),
			MemoryUsage:    MemoryMetrics{HeapAlloc: 2 * 1024 * 1024}, // 2MB
			GoroutineCount: 15,
		},
		{
			Timestamp:      baseTime.Add(60 * time.Minute),
			MemoryUsage:    MemoryMetrics{HeapAlloc: 3 * 1024 * 1024}, // 3MB
			GoroutineCount: 20,
		},
	}

	// Manually add metrics to the monitor
	monitor.mu.Lock()
	monitor.metrics = testMetrics
	monitor.mu.Unlock()

	trend := monitor.GetMemoryTrend(2 * time.Hour)
	
	assert.Equal(t, "increasing", trend.Direction)
	assert.Greater(t, trend.Rate, 0.0) // Should be positive rate
	assert.Equal(t, uint64(3*1024*1024), trend.Peak)
	assert.Equal(t, uint64(3*1024*1024), trend.Current)
	assert.Equal(t, uint64(2*1024*1024), trend.Average)
}

func TestMemoryTrendStable(t *testing.T) {
	config := ProfilerConfig{Enabled: true}
	monitor := NewResourceMonitor(config)

	// Add stable memory usage metrics
	baseTime := time.Now().Add(-30 * time.Minute)
	
	testMetrics := []ResourceMetrics{
		{
			Timestamp:   baseTime,
			MemoryUsage: MemoryMetrics{HeapAlloc: 1024 * 1024}, // 1MB
		},
		{
			Timestamp:   baseTime.Add(10 * time.Minute),
			MemoryUsage: MemoryMetrics{HeapAlloc: 1024*1024 + 1000}, // ~1MB
		},
		{
			Timestamp:   baseTime.Add(20 * time.Minute),
			MemoryUsage: MemoryMetrics{HeapAlloc: 1024*1024 - 1000}, // ~1MB
		},
	}

	monitor.mu.Lock()
	monitor.metrics = testMetrics
	monitor.mu.Unlock()

	trend := monitor.GetMemoryTrend(1 * time.Hour)
	assert.Equal(t, "stable", trend.Direction)
}

func TestMemoryTrendSingleSample(t *testing.T) {
	config := ProfilerConfig{Enabled: true}
	monitor := NewResourceMonitor(config)

	// Add single metric
	testMetrics := []ResourceMetrics{
		{
			Timestamp:   time.Now(),
			MemoryUsage: MemoryMetrics{HeapAlloc: 1024 * 1024}, // 1MB
		},
	}

	monitor.mu.Lock()
	monitor.metrics = testMetrics
	monitor.mu.Unlock()

	trend := monitor.GetMemoryTrend(1 * time.Hour)
	assert.Equal(t, "stable", trend.Direction)
	assert.Equal(t, uint64(1024*1024), trend.Current)
	assert.Equal(t, uint64(1024*1024), trend.Average)
	assert.Equal(t, uint64(1024*1024), trend.Peak)
}

func TestMemoryTrendNoSamples(t *testing.T) {
	config := ProfilerConfig{Enabled: true}
	monitor := NewResourceMonitor(config)

	trend := monitor.GetMemoryTrend(1 * time.Hour)
	assert.Equal(t, "unknown", trend.Direction)
}

func TestDetectMemoryLeak(t *testing.T) {
	config := ProfilerConfig{Enabled: true}
	monitor := NewResourceMonitor(config)

	// Add metrics showing memory leak pattern
	baseTime := time.Now().Add(-1 * time.Hour)
	
	// Increasing memory usage at 2MB per hour = ~555 bytes/second
	testMetrics := []ResourceMetrics{
		{
			Timestamp:   baseTime,
			MemoryUsage: MemoryMetrics{HeapAlloc: 1024 * 1024}, // 1MB
		},
		{
			Timestamp:   baseTime.Add(30 * time.Minute),
			MemoryUsage: MemoryMetrics{HeapAlloc: 2 * 1024 * 1024}, // 2MB
		},
		{
			Timestamp:   baseTime.Add(60 * time.Minute),
			MemoryUsage: MemoryMetrics{HeapAlloc: 3 * 1024 * 1024}, // 3MB
		},
	}

	monitor.mu.Lock()
	monitor.metrics = testMetrics
	monitor.mu.Unlock()

	// Test with low threshold (should detect leak)
	isLeak := monitor.DetectMemoryLeak(2*time.Hour, 500) // 500 bytes/second threshold
	assert.True(t, isLeak)

	// Test with high threshold (should not detect leak)
	isLeak = monitor.DetectMemoryLeak(2*time.Hour, 1000000) // 1MB/second threshold
	assert.False(t, isLeak)
}

func TestMetricsHistoryLimit(t *testing.T) {
	config := ProfilerConfig{Enabled: true}
	monitor := NewResourceMonitor(config)

	// Add more than 1000 metrics to test the limit
	for i := 0; i < 1200; i++ {
		metrics := ResourceMetrics{
			Timestamp:      time.Now().Add(-time.Duration(i) * time.Second),
			MemoryUsage:    MemoryMetrics{HeapAlloc: uint64(1024 * (1000 + i))},
			GoroutineCount: 10 + i,
		}
		monitor.addMetrics(metrics)
	}

	history := monitor.GetMetricsHistory()
	// Should be limited to 1000 entries
	assert.LessOrEqual(t, len(history), 1000)
}

func TestRealResourceMetrics(t *testing.T) {
	config := ProfilerConfig{Enabled: true}
	monitor := NewResourceMonitor(config)

	// Allocate some memory to see changes in metrics
	initialMetrics := monitor.GetCurrentMetrics()
	
	// Allocate memory
	data := make([]byte, 1024*1024) // 1MB
	_ = data
	
	runtime.GC() // Force garbage collection
	
	afterAllocMetrics := monitor.GetCurrentMetrics()
	
	// Memory usage should have changed
	assert.NotEqual(t, initialMetrics.MemoryUsage.HeapAlloc, afterAllocMetrics.MemoryUsage.HeapAlloc)
	assert.NotEqual(t, initialMetrics.MemoryUsage.NumGC, afterAllocMetrics.MemoryUsage.NumGC)
	
	// Timestamps should be different
	assert.True(t, afterAllocMetrics.Timestamp.After(initialMetrics.Timestamp))
}

func TestConcurrentAccess(t *testing.T) {
	config := ProfilerConfig{Enabled: true}
	monitor := NewResourceMonitor(config)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start monitoring
	err := monitor.Start(ctx)
	require.NoError(t, err)

	// Concurrent access to metrics
	done := make(chan bool, 3)
	
	// Reader 1
	go func() {
		for i := 0; i < 10; i++ {
			_ = monitor.GetCurrentMetrics()
			_ = monitor.GetMetricsHistory()
			time.Sleep(5 * time.Millisecond)
		}
		done <- true
	}()

	// Reader 2
	go func() {
		for i := 0; i < 10; i++ {
			_ = monitor.GetMemoryTrend(1 * time.Minute)
			time.Sleep(5 * time.Millisecond)
		}
		done <- true
	}()

	// Reader 3
	go func() {
		for i := 0; i < 10; i++ {
			_ = monitor.DetectMemoryLeak(1*time.Minute, 1024)
			time.Sleep(5 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for all readers to complete
	for i := 0; i < 3; i++ {
		<-done
	}

	err = monitor.Stop()
	assert.NoError(t, err)
}