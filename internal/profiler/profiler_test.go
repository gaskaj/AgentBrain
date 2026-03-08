package profiler

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		config  ProfilerConfig
		wantErr bool
	}{
		{
			name: "disabled profiler",
			config: ProfilerConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "enabled profiler with valid config",
			config: ProfilerConfig{
				Enabled:               true,
				SampleRate:            0.1,
				OutputDir:             t.TempDir(),
				CPUProfileDuration:    30 * time.Second,
				MemoryProfileInterval: 5 * time.Minute,
				GoroutineThreshold:    1000,
			},
			wantErr: false,
		},
		{
			name: "enabled profiler with invalid output dir",
			config: ProfilerConfig{
				Enabled:   true,
				OutputDir: "/invalid/path/that/cannot/be/created",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profiler, err := New(tt.config)
			
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			
			require.NoError(t, err)
			assert.NotNil(t, profiler)
			assert.Equal(t, tt.config.Enabled, profiler.IsEnabled())
			
			if tt.config.Enabled {
				assert.NotNil(t, profiler.monitor)
				assert.NotNil(t, profiler.analytics)
			}
		})
	}
}

func TestProfilerStartStop(t *testing.T) {
	outputDir := t.TempDir()
	config := ProfilerConfig{
		Enabled:               true,
		OutputDir:             outputDir,
		CPUProfileDuration:    1 * time.Second,
		MemoryProfileInterval: 100 * time.Millisecond,
		GoroutineThreshold:    1000,
	}

	profiler, err := New(config)
	require.NoError(t, err)

	ctx := context.Background()

	// Test start
	err = profiler.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, profiler.IsRunning())

	// Test double start
	err = profiler.Start(ctx)
	assert.Error(t, err)

	// Test stop
	err = profiler.Stop()
	assert.NoError(t, err)
	assert.False(t, profiler.IsRunning())

	// Test double stop
	err = profiler.Stop()
	assert.NoError(t, err)
}

func TestDisabledProfiler(t *testing.T) {
	config := ProfilerConfig{Enabled: false}
	profiler, err := New(config)
	require.NoError(t, err)

	ctx := context.Background()

	// All operations should be no-ops for disabled profiler
	err = profiler.Start(ctx)
	assert.NoError(t, err)

	err = profiler.StartCPUProfile()
	assert.NoError(t, err)

	err = profiler.StopCPUProfile()
	assert.NoError(t, err)

	err = profiler.CaptureMemProfile()
	assert.NoError(t, err)

	err = profiler.CaptureGoroutineProfile()
	assert.NoError(t, err)

	err = profiler.Stop()
	assert.NoError(t, err)

	// TrackOperation should be a no-op
	profiler.TrackOperation("test", time.Millisecond, nil)
}

func TestCPUProfiling(t *testing.T) {
	outputDir := t.TempDir()
	config := ProfilerConfig{
		Enabled:   true,
		OutputDir: outputDir,
	}

	profiler, err := New(config)
	require.NoError(t, err)

	// Test CPU profile start/stop
	err = profiler.StartCPUProfile()
	assert.NoError(t, err)

	// Double start should error
	err = profiler.StartCPUProfile()
	assert.Error(t, err)

	err = profiler.StopCPUProfile()
	assert.NoError(t, err)

	// Check that profile file was created
	files, err := os.ReadDir(outputDir)
	require.NoError(t, err)
	
	found := false
	for _, file := range files {
		if filepath.Ext(file.Name()) == ".prof" && 
		   filepath.Base(file.Name())[:4] == "cpu-" {
			found = true
			break
		}
	}
	assert.True(t, found, "CPU profile file should be created")
}

func TestMemoryProfiling(t *testing.T) {
	outputDir := t.TempDir()
	config := ProfilerConfig{
		Enabled:   true,
		OutputDir: outputDir,
	}

	profiler, err := New(config)
	require.NoError(t, err)

	err = profiler.CaptureMemProfile()
	assert.NoError(t, err)

	// Check that profile file was created
	files, err := os.ReadDir(outputDir)
	require.NoError(t, err)
	
	found := false
	for _, file := range files {
		if filepath.Ext(file.Name()) == ".prof" && 
		   filepath.Base(file.Name())[:4] == "mem-" {
			found = true
			break
		}
	}
	assert.True(t, found, "Memory profile file should be created")
}

func TestGoroutineProfiling(t *testing.T) {
	outputDir := t.TempDir()
	config := ProfilerConfig{
		Enabled:   true,
		OutputDir: outputDir,
	}

	profiler, err := New(config)
	require.NoError(t, err)

	err = profiler.CaptureGoroutineProfile()
	assert.NoError(t, err)

	// Check that profile file was created
	files, err := os.ReadDir(outputDir)
	require.NoError(t, err)
	
	found := false
	for _, file := range files {
		if filepath.Ext(file.Name()) == ".prof" && 
		   filepath.Base(file.Name())[:10] == "goroutine-" {
			found = true
			break
		}
	}
	assert.True(t, found, "Goroutine profile file should be created")
}

func TestTrackOperation(t *testing.T) {
	config := ProfilerConfig{
		Enabled:   true,
		OutputDir: t.TempDir(),
	}

	profiler, err := New(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = profiler.Start(ctx)
	require.NoError(t, err)
	defer profiler.Stop()

	// Track some operations
	metadata := map[string]interface{}{
		"test": true,
		"size": 1024,
	}
	
	profiler.TrackOperation("test_operation", 100*time.Millisecond, metadata)
	profiler.TrackOperation("test_operation", 200*time.Millisecond, metadata)

	// Verify analytics received the data
	analytics := profiler.GetAnalytics()
	metrics, exists := analytics.GetOperationMetrics("test_operation")
	require.True(t, exists)
	assert.Equal(t, int64(2), metrics.Count)
	assert.Equal(t, 300*time.Millisecond, metrics.TotalTime)
	assert.Equal(t, 100*time.Millisecond, metrics.MinTime)
	assert.Equal(t, 200*time.Millisecond, metrics.MaxTime)
}

func TestGetComponents(t *testing.T) {
	config := ProfilerConfig{
		Enabled:   true,
		OutputDir: t.TempDir(),
	}

	profiler, err := New(config)
	require.NoError(t, err)

	monitor := profiler.GetResourceMonitor()
	assert.NotNil(t, monitor)

	analytics := profiler.GetAnalytics()
	assert.NotNil(t, analytics)
}

func TestPeriodicMemoryProfiling(t *testing.T) {
	outputDir := t.TempDir()
	config := ProfilerConfig{
		Enabled:               true,
		OutputDir:             outputDir,
		MemoryProfileInterval: 50 * time.Millisecond, // Short interval for testing
	}

	profiler, err := New(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err = profiler.Start(ctx)
	require.NoError(t, err)

	// Wait for periodic profiling to run
	time.Sleep(200 * time.Millisecond)

	err = profiler.Stop()
	require.NoError(t, err)

	// Check that at least one memory profile was created
	files, err := os.ReadDir(outputDir)
	require.NoError(t, err)

	memoryProfiles := 0
	for _, file := range files {
		if filepath.Ext(file.Name()) == ".prof" && 
		   filepath.Base(file.Name())[:4] == "mem-" {
			memoryProfiles++
		}
	}
	
	// Should have created at least one profile during the interval
	assert.GreaterOrEqual(t, memoryProfiles, 1)
}

func TestWithProfiling(t *testing.T) {
	config := ProfilerConfig{
		Enabled:   true,
		OutputDir: t.TempDir(),
	}

	profiler, err := New(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = profiler.Start(ctx)
	require.NoError(t, err)
	defer profiler.Stop()

	// Test successful operation
	err = WithProfiling(profiler, "test_operation", func() error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	assert.NoError(t, err)

	// Test failed operation
	testErr := assert.AnError
	err = WithProfiling(profiler, "test_operation_error", func() error {
		return testErr
	})
	assert.Equal(t, testErr, err)

	// Verify analytics tracked both operations
	analytics := profiler.GetAnalytics()
	
	metrics, exists := analytics.GetOperationMetrics("test_operation")
	assert.True(t, exists)
	assert.Equal(t, int64(1), metrics.Count)
	assert.Equal(t, int64(0), metrics.Errors)

	errorMetrics, exists := analytics.GetOperationMetrics("test_operation_error")
	assert.True(t, exists)
	assert.Equal(t, int64(1), errorMetrics.Count)
	assert.Equal(t, int64(1), errorMetrics.Errors)
}

func TestDisabledWithProfiling(t *testing.T) {
	// Test with nil profiler
	err := WithProfiling(nil, "test", func() error {
		return nil
	})
	assert.NoError(t, err)

	// Test with disabled profiler
	config := ProfilerConfig{Enabled: false}
	profiler, err := New(config)
	require.NoError(t, err)

	err = WithProfiling(profiler, "test", func() error {
		return nil
	})
	assert.NoError(t, err)
}