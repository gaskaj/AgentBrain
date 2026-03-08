package plugin

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"plugin"
	"strings"
	"sync"
	"time"

	"github.com/agentbrain/agentbrain/internal/config"
	"github.com/agentbrain/agentbrain/internal/connector"
	"github.com/robfig/cron/v3"
)

// Manager handles plugin lifecycle and hot-reload capabilities
type Manager struct {
	mu           sync.RWMutex
	config       *config.PluginConfig
	plugins      map[string]*PluginInfo
	watchers     map[string]*FileWatcher
	registry     *connector.Registry
	logger       *slog.Logger
	scheduler    *cron.Cron
	shutdownCh   chan struct{}
	healthChecker *HealthChecker
}

// PluginInfo contains metadata and state for a loaded plugin
type PluginInfo struct {
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Path         string                 `json:"path"`
	LoadTime     time.Time              `json:"load_time"`
	LastUsed     time.Time              `json:"last_used"`
	Status       PluginStatus           `json:"status"`
	Plugin       *plugin.Plugin         `json:"-"`
	Connector    connector.Connector    `json:"-"`
	Factory      connector.Factory      `json:"-"`
	Metadata     map[string]interface{} `json:"metadata"`
	ErrorCount   int                    `json:"error_count"`
	LastError    string                 `json:"last_error,omitempty"`
	ProcessID    int                    `json:"process_id,omitempty"`
}

// PluginStatus represents the current state of a plugin
type PluginStatus string

const (
	PluginStatusLoading   PluginStatus = "loading"
	PluginStatusActive    PluginStatus = "active"
	PluginStatusError     PluginStatus = "error"
	PluginStatusDisabled  PluginStatus = "disabled"
	PluginStatusUnloading PluginStatus = "unloading"
)

// FileWatcher monitors plugin directory for changes
type FileWatcher struct {
	Path       string
	LastModified time.Time
	stopCh     chan struct{}
}

// NewManager creates a new plugin manager
func NewManager(cfg *config.PluginConfig, registry *connector.Registry, logger *slog.Logger) *Manager {
	return &Manager{
		config:        cfg,
		plugins:       make(map[string]*PluginInfo),
		watchers:      make(map[string]*FileWatcher),
		registry:      registry,
		logger:        logger,
		scheduler:     cron.New(),
		shutdownCh:    make(chan struct{}),
		healthChecker: NewHealthChecker(logger),
	}
}

// Start initializes the plugin manager and loads initial plugins
func (m *Manager) Start(ctx context.Context) error {
	if !m.config.Enabled {
		m.logger.Info("Plugin system disabled")
		return nil
	}

	m.logger.Info("Starting plugin manager", "directory", m.config.Directory)

	// Create plugin directory if it doesn't exist
	if err := os.MkdirAll(m.config.Directory, 0755); err != nil {
		return fmt.Errorf("create plugin directory: %w", err)
	}

	// Load initial plugins
	if err := m.loadInitialPlugins(ctx); err != nil {
		return fmt.Errorf("load initial plugins: %w", err)
	}

	// Start file watchers if auto-reload is enabled
	if m.config.AutoReload {
		if err := m.startWatchers(); err != nil {
			return fmt.Errorf("start watchers: %w", err)
		}
	}

	// Start health checker
	m.healthChecker.Start(ctx)

	// Start cleanup scheduler
	m.scheduler.AddFunc("@every 5m", m.cleanupUnusedPlugins)
	m.scheduler.Start()

	m.logger.Info("Plugin manager started successfully")
	return nil
}

// Stop gracefully shuts down the plugin manager
func (m *Manager) Stop(ctx context.Context) error {
	m.logger.Info("Stopping plugin manager")

	close(m.shutdownCh)
	
	// Stop scheduler
	m.scheduler.Stop()

	// Stop watchers
	m.mu.Lock()
	for _, watcher := range m.watchers {
		close(watcher.stopCh)
	}
	m.watchers = make(map[string]*FileWatcher)
	m.mu.Unlock()

	// Stop health checker
	m.healthChecker.Stop()

	// Unload all plugins
	m.mu.Lock()
	defer m.mu.Unlock()

	for name := range m.plugins {
		if err := m.unloadPluginUnsafe(name); err != nil {
			m.logger.Error("Error unloading plugin during shutdown", "name", name, "error", err)
		}
		delete(m.plugins, name)
	}

	m.logger.Info("Plugin manager stopped")
	return nil
}

// LoadPlugin loads a plugin from the specified path
func (m *Manager) LoadPlugin(ctx context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := m.getPluginNameFromPath(path)
	
	m.logger.Info("Loading plugin", "name", name, "path", path)

	// Check if plugin is already loaded
	if existing, exists := m.plugins[name]; exists {
		if existing.Status == PluginStatusActive {
			return fmt.Errorf("plugin %s is already loaded", name)
		}
		// Unload the existing plugin first
		if err := m.unloadPluginUnsafe(name); err != nil {
			return fmt.Errorf("unload existing plugin %s: %w", name, err)
		}
	}

	// Create plugin info
	pluginInfo := &PluginInfo{
		Name:     name,
		Path:     path,
		LoadTime: time.Now(),
		Status:   PluginStatusLoading,
		Metadata: make(map[string]interface{}),
	}
	m.plugins[name] = pluginInfo

	// Load the plugin
	if err := m.loadPluginUnsafe(ctx, pluginInfo); err != nil {
		pluginInfo.Status = PluginStatusError
		pluginInfo.LastError = err.Error()
		pluginInfo.ErrorCount++
		return fmt.Errorf("load plugin %s: %w", name, err)
	}

	pluginInfo.Status = PluginStatusActive
	m.logger.Info("Plugin loaded successfully", "name", name)

	return nil
}

// UnloadPlugin unloads a specific plugin
func (m *Manager) UnloadPlugin(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Unloading plugin", "name", name)

	pluginInfo, exists := m.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	pluginInfo.Status = PluginStatusUnloading

	if err := m.unloadPluginUnsafe(name); err != nil {
		pluginInfo.Status = PluginStatusError
		pluginInfo.LastError = err.Error()
		pluginInfo.ErrorCount++
		return fmt.Errorf("unload plugin %s: %w", name, err)
	}

	delete(m.plugins, name)
	m.logger.Info("Plugin unloaded successfully", "name", name)

	return nil
}

// ReloadPlugin hot-reloads a specific plugin
func (m *Manager) ReloadPlugin(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Reloading plugin", "name", name)

	pluginInfo, exists := m.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	// Unload existing plugin
	if err := m.unloadPluginUnsafe(name); err != nil {
		m.logger.Warn("Error unloading plugin for reload", "name", name, "error", err)
	}

	// Reset plugin info
	pluginInfo.LoadTime = time.Now()
	pluginInfo.Status = PluginStatusLoading
	pluginInfo.LastError = ""

	// Reload the plugin
	if err := m.loadPluginUnsafe(ctx, pluginInfo); err != nil {
		pluginInfo.Status = PluginStatusError
		pluginInfo.LastError = err.Error()
		pluginInfo.ErrorCount++
		return fmt.Errorf("reload plugin %s: %w", name, err)
	}

	pluginInfo.Status = PluginStatusActive
	m.logger.Info("Plugin reloaded successfully", "name", name)

	return nil
}

// GetPlugin returns plugin information
func (m *Manager) GetPlugin(name string) (*PluginInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pluginInfo, exists := m.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin %s not found", name)
	}

	// Create a copy to avoid concurrent access issues
	info := *pluginInfo
	return &info, nil
}

// ListPlugins returns information about all loaded plugins
func (m *Manager) ListPlugins() map[string]*PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*PluginInfo)
	for name, pluginInfo := range m.plugins {
		// Create a copy to avoid concurrent access issues
		info := *pluginInfo
		result[name] = &info
	}

	return result
}

// GetConnectorFactory returns a connector factory for the specified plugin
func (m *Manager) GetConnectorFactory(pluginName string) (connector.Factory, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pluginInfo, exists := m.plugins[pluginName]
	if !exists {
		return nil, fmt.Errorf("plugin %s not found", pluginName)
	}

	if pluginInfo.Status != PluginStatusActive {
		return nil, fmt.Errorf("plugin %s is not active (status: %s)", pluginName, pluginInfo.Status)
	}

	if pluginInfo.Factory == nil {
		return nil, fmt.Errorf("plugin %s does not provide a connector factory", pluginName)
	}

	// Update last used time
	pluginInfo.LastUsed = time.Now()

	return pluginInfo.Factory, nil
}

// loadInitialPlugins loads all plugins found in the plugin directory
func (m *Manager) loadInitialPlugins(ctx context.Context) error {
	return filepath.WalkDir(m.config.Directory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// Only load .so files
		if !strings.HasSuffix(path, ".so") {
			return nil
		}

		if err := m.LoadPlugin(ctx, path); err != nil {
			m.logger.Warn("Failed to load plugin", "path", path, "error", err)
			// Continue loading other plugins even if one fails
		}

		return nil
	})
}

// loadPluginUnsafe loads a plugin without acquiring locks (must be called with lock held)
func (m *Manager) loadPluginUnsafe(ctx context.Context, pluginInfo *PluginInfo) error {
	// Open the plugin file
	p, err := plugin.Open(pluginInfo.Path)
	if err != nil {
		return fmt.Errorf("open plugin file: %w", err)
	}

	pluginInfo.Plugin = p

	// Look for the required symbols
	if err := m.loadPluginSymbols(pluginInfo); err != nil {
		return fmt.Errorf("load plugin symbols: %w", err)
	}

	// Register the connector with the registry
	if pluginInfo.Factory != nil {
		m.registry.Register("plugin:"+pluginInfo.Name, pluginInfo.Factory)
	}

	return nil
}

// loadPluginSymbols loads required symbols from the plugin
func (m *Manager) loadPluginSymbols(pluginInfo *PluginInfo) error {
	p := pluginInfo.Plugin

	// Load plugin metadata
	metadataSymbol, err := p.Lookup("PluginMetadata")
	if err != nil {
		return fmt.Errorf("plugin metadata not found: %w", err)
	}

	metadata, ok := metadataSymbol.(*map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid plugin metadata type")
	}
	pluginInfo.Metadata = *metadata

	// Extract version from metadata
	if version, ok := (*metadata)["version"].(string); ok {
		pluginInfo.Version = version
	}

	// Load connector factory
	factorySymbol, err := p.Lookup("NewConnector")
	if err != nil {
		m.logger.Warn("Plugin does not export connector factory", "name", pluginInfo.Name)
		return nil // Not all plugins need to be connectors
	}

	factory, ok := factorySymbol.(func(*config.SourceConfig) (connector.Connector, error))
	if !ok {
		return fmt.Errorf("invalid connector factory signature")
	}

	pluginInfo.Factory = factory

	return nil
}

// unloadPluginUnsafe unloads a plugin without acquiring locks (must be called with lock held)
func (m *Manager) unloadPluginUnsafe(name string) error {
	pluginInfo, exists := m.plugins[name]
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	// Close connector if it exists
	if pluginInfo.Connector != nil {
		if err := pluginInfo.Connector.Close(); err != nil {
			m.logger.Warn("Error closing connector", "name", name, "error", err)
		}
		pluginInfo.Connector = nil
	}

	// Note: Go doesn't support unloading plugins, so we just clear our references
	pluginInfo.Plugin = nil
	pluginInfo.Factory = nil

	return nil
}

// startWatchers starts file system watchers for auto-reload
func (m *Manager) startWatchers() error {
	for _, watchPath := range m.config.WatchPaths {
		watcher := &FileWatcher{
			Path:   watchPath,
			stopCh: make(chan struct{}),
		}

		go m.watchDirectory(watcher)
		m.watchers[watchPath] = watcher
	}

	return nil
}

// watchDirectory monitors a directory for changes
func (m *Manager) watchDirectory(watcher *FileWatcher) {
	ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.checkDirectoryChanges(watcher); err != nil {
				m.logger.Error("Error checking directory changes", "path", watcher.Path, "error", err)
			}
		case <-watcher.stopCh:
			return
		case <-m.shutdownCh:
			return
		}
	}
}

// checkDirectoryChanges checks for changes in the watched directory
func (m *Manager) checkDirectoryChanges(watcher *FileWatcher) error {
	return filepath.WalkDir(watcher.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(path, ".so") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		// Check if file was modified
		if info.ModTime().After(watcher.LastModified) {
			m.logger.Info("Detected plugin file change", "path", path)

			// Try to reload the plugin
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := m.LoadPlugin(ctx, path); err != nil {
				m.logger.Error("Failed to reload changed plugin", "path", path, "error", err)
			}

			watcher.LastModified = info.ModTime()
		}

		return nil
	})
}

// cleanupUnusedPlugins removes plugins that haven't been used recently
func (m *Manager) cleanupUnusedPlugins() {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-1 * time.Hour) // Remove plugins unused for 1 hour

	for name, pluginInfo := range m.plugins {
		if pluginInfo.LastUsed.IsZero() {
			continue // Never used, keep for now
		}

		if pluginInfo.LastUsed.Before(cutoff) && pluginInfo.Status != PluginStatusActive {
			m.logger.Info("Cleaning up unused plugin", "name", name)
			if err := m.unloadPluginUnsafe(name); err != nil {
				m.logger.Error("Error cleaning up plugin", "name", name, "error", err)
			} else {
				delete(m.plugins, name)
			}
		}
	}
}

// getPluginNameFromPath extracts plugin name from file path
func (m *Manager) getPluginNameFromPath(path string) string {
	name := filepath.Base(path)
	// Remove .so extension
	if strings.HasSuffix(name, ".so") {
		name = name[:len(name)-3]
	}
	return name
}