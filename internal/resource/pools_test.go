package resource

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPClientPool(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("NewHTTPClientPool", func(t *testing.T) {
		config := PoolConfig{
			MaxSize:         10,
			InitialSize:     2,
			IdleTimeout:     5 * time.Minute,
			CleanupInterval: 1 * time.Minute,
		}

		pool, err := NewHTTPClientPool(config, logger)
		require.NoError(t, err)
		assert.NotNil(t, pool)

		defer pool.Close()

		health := pool.Health()
		assert.Equal(t, 10, health.MaxSize)
		assert.True(t, health.Idle >= 0 && health.Idle <= 2) // Initial size
	})

	t.Run("AcquireAndRelease", func(t *testing.T) {
		config := PoolConfig{
			MaxSize:     5,
			InitialSize: 1,
		}

		pool, err := NewHTTPClientPool(config, logger)
		require.NoError(t, err)
		defer pool.Close()

		ctx := context.Background()

		// Acquire a client
		client, err := pool.AcquireHTTPClient(ctx, PriorityNormal)
		require.NoError(t, err)
		assert.NotNil(t, client)

		health := pool.Health()
		assert.Equal(t, 1, health.Active)

		// Release the client
		pool.ReleaseHTTPClient(client)

		health = pool.Health()
		assert.Equal(t, 0, health.Active)
	})

	t.Run("PoolExhaustion", func(t *testing.T) {
		config := PoolConfig{
			MaxSize:     2,
			InitialSize: 0,
		}

		pool, err := NewHTTPClientPool(config, logger)
		require.NoError(t, err)
		defer pool.Close()

		ctx := context.Background()

		// Acquire all available clients
		client1, err := pool.AcquireHTTPClient(ctx, PriorityNormal)
		require.NoError(t, err)

		client2, err := pool.AcquireHTTPClient(ctx, PriorityNormal)
		require.NoError(t, err)

		// Next acquisition should timeout
		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		_, err = pool.AcquireHTTPClient(ctx, PriorityLow)
		assert.Error(t, err)

		health := pool.Health()
		assert.True(t, health.Timeouts > 0)

		// Release clients
		pool.ReleaseHTTPClient(client1)
		pool.ReleaseHTTPClient(client2)
	})

	t.Run("PriorityHandling", func(t *testing.T) {
		config := PoolConfig{
			MaxSize:     1,
			InitialSize: 0,
		}

		pool, err := NewHTTPClientPool(config, logger)
		require.NoError(t, err)
		defer pool.Close()

		ctx := context.Background()

		// Acquire the only client
		client, err := pool.AcquireHTTPClient(ctx, PriorityNormal)
		require.NoError(t, err)

		// Test different priority timeouts
		priorities := []Priority{PriorityLow, PriorityNormal, PriorityHigh, PriorityCritical}
		expectedTimeouts := []time.Duration{5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second}

		for i, priority := range priorities {
			start := time.Now()
			ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
			_, err := pool.AcquireHTTPClient(ctx, priority)
			elapsed := time.Since(start)
			cancel()

			assert.Error(t, err)
			// Should timeout before the full priority timeout
			assert.True(t, elapsed < expectedTimeouts[i])
		}

		pool.ReleaseHTTPClient(client)
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		config := PoolConfig{
			MaxSize:     10,
			InitialSize: 2,
		}

		pool, err := NewHTTPClientPool(config, logger)
		require.NoError(t, err)
		defer pool.Close()

		ctx := context.Background()
		var wg sync.WaitGroup
		const goroutines = 20
		acquired := make([]interface{}, goroutines)

		// Acquire clients concurrently
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				client, err := pool.AcquireHTTPClient(ctx, PriorityNormal)
				if err == nil {
					acquired[idx] = client
				}
			}(i)
		}

		wg.Wait()

		successfulAcquisitions := 0
		for _, client := range acquired {
			if client != nil {
				successfulAcquisitions++
			}
		}

		// Should not exceed max size
		health := pool.Health()
		assert.True(t, health.Active <= config.MaxSize)
		assert.True(t, successfulAcquisitions <= config.MaxSize)

		// Release all acquired clients
		for _, client := range acquired {
			if client != nil {
				if httpClient, ok := client.(*http.Client); ok {
					pool.ReleaseHTTPClient(httpClient)
				}
			}
		}
	})
}

func TestParquetWriterPool(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("NewParquetWriterPool", func(t *testing.T) {
		config := PoolConfig{
			MaxSize:         5,
			CleanupInterval: 30 * time.Second,
		}

		pool, err := NewParquetWriterPool(config, logger)
		require.NoError(t, err)
		assert.NotNil(t, pool)

		defer pool.Close()

		health := pool.Health()
		assert.Equal(t, 5, health.MaxSize)
	})

	t.Run("PoolLimits", func(t *testing.T) {
		config := PoolConfig{
			MaxSize: 2,
		}

		pool, err := NewParquetWriterPool(config, logger)
		require.NoError(t, err)
		defer pool.Close()

		ctx := context.Background()

		// Try to acquire more writers than the pool allows
		// Since we can't easily create actual parquet writers in tests,
		// this test mainly verifies the pool doesn't crash
		_, err = pool.Acquire(ctx, nil) // nil schema should be handled gracefully
		// We expect this to fail since we can't create real writers without proper setup
		assert.Error(t, err)
	})

	t.Run("HealthMetrics", func(t *testing.T) {
		config := PoolConfig{
			MaxSize: 3,
		}

		pool, err := NewParquetWriterPool(config, logger)
		require.NoError(t, err)
		defer pool.Close()

		health := pool.Health()
		assert.Equal(t, 3, health.MaxSize)
		assert.Equal(t, 0, health.Active)
		assert.Equal(t, 0, health.Idle)
		assert.True(t, health.LastReset.Before(time.Now()) || health.LastReset.Equal(time.Now()))
	})

	t.Run("CleanupLoop", func(t *testing.T) {
		config := PoolConfig{
			MaxSize:         2,
			CleanupInterval: 50 * time.Millisecond, // Fast cleanup for testing
		}

		pool, err := NewParquetWriterPool(config, logger)
		require.NoError(t, err)

		// Wait for at least one cleanup cycle
		time.Sleep(100 * time.Millisecond)

		err = pool.Close()
		assert.NoError(t, err)
	})
}

func TestPoolDefaults(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("HTTPClientPoolDefaults", func(t *testing.T) {
		// Test with empty config to verify defaults
		config := PoolConfig{}

		pool, err := NewHTTPClientPool(config, logger)
		require.NoError(t, err)
		defer pool.Close()

		health := pool.Health()
		assert.Equal(t, 20, health.MaxSize) // Default MaxSize
		assert.True(t, health.Idle >= 0)     // Should have some initial clients
	})

	t.Run("ParquetWriterPoolDefaults", func(t *testing.T) {
		config := PoolConfig{}

		pool, err := NewParquetWriterPool(config, logger)
		require.NoError(t, err)
		defer pool.Close()

		health := pool.Health()
		assert.Equal(t, 10, health.MaxSize) // Default MaxSize
	})
}