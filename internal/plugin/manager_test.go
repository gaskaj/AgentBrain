package plugin

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agentbrain/agentbrain/internal/config"
	"github.com/agentbrain/agentbrain/internal/connector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_StartStop(t *testing.T) {
	// Create temporary plugin directory
	tempDir := t.TempDir()
	
	pluginConfig := &config.PluginConfig{
		Enabled:    true,
		Directory:  tempDir,
		AutoReload: false,
		WatchPaths: []string{tempDir},
	}

	registry := connector.NewRegistry()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	
	manager := NewManager(pluginConfig, registry, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test start
	err := manager.Start(ctx)
	assert.NoError(t, err)

	// Test stop
	err = manager.Stop(ctx)
	assert.NoError(t, err)
}

func TestManager_PluginLifecycle(t *testing.T) {
	// Create temporary plugin directory
	tempDir := t.TempDir()
	
	pluginConfig := &config.PluginConfig{
		Enabled:    true,
		Directory:  tempDir,
		AutoReload: false,
		WatchPaths: []string{tempDir},
	}

	registry := connector.NewRegistry()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	
	manager := NewManager(pluginConfig, registry, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop(ctx)

	// Test listing empty plugins
	plugins := manager.ListPlugins()
	assert.Empty(t, plugins)

	// Test getting non-existent plugin
	_, err = manager.GetPlugin("nonexistent")
	assert.Error(t, err)

	// Test getting factory for non-existent plugin
	_, err = manager.GetConnectorFactory("nonexistent")
	assert.Error(t, err)
}

func TestManager_Configuration(t *testing.T) {
	// Test with disabled plugin system
	pluginConfig := &config.PluginConfig{
		Enabled: false,
	}

	registry := connector.NewRegistry()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	
	manager := NewManager(pluginConfig, registry, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := manager.Start(ctx)
	assert.NoError(t, err) // Should succeed even when disabled

	err = manager.Stop(ctx)
	assert.NoError(t, err)
}

func TestManager_DirectoryCreation(t *testing.T) {
	// Test that manager creates plugin directory if it doesn't exist
	tempDir := t.TempDir()
	pluginDir := filepath.Join(tempDir, "plugins")
	
	pluginConfig := &config.PluginConfig{
		Enabled:    true,
		Directory:  pluginDir,
		AutoReload: false,
	}

	registry := connector.NewRegistry()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	
	manager := NewManager(pluginConfig, registry, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Directory shouldn't exist initially
	_, err := os.Stat(pluginDir)
	assert.True(t, os.IsNotExist(err))

	err = manager.Start(ctx)
	assert.NoError(t, err)
	defer manager.Stop(ctx)

	// Directory should exist now
	_, err = os.Stat(pluginDir)
	assert.NoError(t, err)
}

func TestPluginInfo(t *testing.T) {
	info := &PluginInfo{
		Name:       "test-plugin",
		Version:    "1.0.0",
		Path:       "/path/to/plugin.so",
		LoadTime:   time.Now(),
		Status:     PluginStatusActive,
		Metadata:   map[string]interface{}{"key": "value"},
		ErrorCount: 0,
	}

	assert.Equal(t, "test-plugin", info.Name)
	assert.Equal(t, "1.0.0", info.Version)
	assert.Equal(t, PluginStatusActive, info.Status)
	assert.Equal(t, 0, info.ErrorCount)
	assert.Contains(t, info.Metadata, "key")
}

func TestFileWatcher(t *testing.T) {
	watcher := &FileWatcher{
		Path:         "/test/path",
		LastModified: time.Now(),
		stopCh:       make(chan struct{}),
	}

	assert.Equal(t, "/test/path", watcher.Path)
	assert.NotNil(t, watcher.stopCh)

	// Test stopping the watcher
	close(watcher.stopCh)
}

func TestPluginStatus(t *testing.T) {
	tests := []PluginStatus{
		PluginStatusLoading,
		PluginStatusActive,
		PluginStatusError,
		PluginStatusDisabled,
		PluginStatusUnloading,
	}

	for _, status := range tests {
		assert.NotEmpty(t, string(status))
	}
}