package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/agentbrain/agentbrain/internal/config"
)

// APIServer provides REST endpoints for plugin management
type APIServer struct {
	manager *Manager
	sandbox *Sandbox
	logger  *slog.Logger
	config  *config.PluginConfig
}

// PluginListResponse represents the response for listing plugins
type PluginListResponse struct {
	Plugins map[string]*PluginInfo `json:"plugins"`
	Total   int                    `json:"total"`
}

// PluginHealthResponse represents plugin health status
type PluginHealthResponse struct {
	Name      string                 `json:"name"`
	Status    PluginStatus           `json:"status"`
	Healthy   bool                   `json:"healthy"`
	LastCheck time.Time              `json:"last_check"`
	Metrics   *ConnectorMetrics      `json:"metrics,omitempty"`
	Process   *SandboxedProcess      `json:"process,omitempty"`
	Details   map[string]interface{} `json:"details"`
}

// PluginActionRequest represents a plugin action request
type PluginActionRequest struct {
	Action string            `json:"action"`
	Params map[string]string `json:"params,omitempty"`
}

// PluginActionResponse represents a plugin action response
type PluginActionResponse struct {
	Success   bool                   `json:"success"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// ErrorResponse represents an API error response
type ErrorResponse struct {
	Error     string    `json:"error"`
	Code      string    `json:"code"`
	Timestamp time.Time `json:"timestamp"`
}

// NewAPIServer creates a new plugin API server
func NewAPIServer(manager *Manager, sandbox *Sandbox, config *config.PluginConfig, logger *slog.Logger) *APIServer {
	return &APIServer{
		manager: manager,
		sandbox: sandbox,
		logger:  logger,
		config:  config,
	}
}

// RegisterRoutes registers API routes with the HTTP server
func (api *APIServer) RegisterRoutes(mux *http.ServeMux) {
	// Plugin management endpoints
	mux.HandleFunc("GET /api/v1/plugins", api.handleListPlugins)
	mux.HandleFunc("GET /api/v1/plugins/{name}", api.handleGetPlugin)
	mux.HandleFunc("POST /api/v1/plugins/{name}/reload", api.handleReloadPlugin)
	mux.HandleFunc("POST /api/v1/plugins/{name}/unload", api.handleUnloadPlugin)
	mux.HandleFunc("POST /api/v1/plugins/load", api.handleLoadPlugin)
	
	// Plugin health endpoints
	mux.HandleFunc("GET /api/v1/plugins/{name}/health", api.handleGetPluginHealth)
	mux.HandleFunc("GET /api/v1/plugins/health", api.handleGetAllPluginHealth)
	
	// Plugin process management (sandbox)
	mux.HandleFunc("GET /api/v1/plugins/{name}/process", api.handleGetPluginProcess)
	mux.HandleFunc("POST /api/v1/plugins/{name}/restart", api.handleRestartPlugin)
	
	// Plugin metrics endpoints
	mux.HandleFunc("GET /api/v1/plugins/{name}/metrics", api.handleGetPluginMetrics)
	mux.HandleFunc("GET /api/v1/plugins/metrics", api.handleGetAllPluginMetrics)
}

// handleListPlugins returns a list of all loaded plugins
func (api *APIServer) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	plugins := api.manager.ListPlugins()
	
	response := PluginListResponse{
		Plugins: plugins,
		Total:   len(plugins),
	}

	api.sendJSON(w, response)
}

// handleGetPlugin returns information about a specific plugin
func (api *APIServer) handleGetPlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		api.sendError(w, "Plugin name is required", "MISSING_PARAMETER", http.StatusBadRequest)
		return
	}

	plugin, err := api.manager.GetPlugin(name)
	if err != nil {
		api.sendError(w, fmt.Sprintf("Plugin not found: %v", err), "NOT_FOUND", http.StatusNotFound)
		return
	}

	api.sendJSON(w, plugin)
}

// handleReloadPlugin hot-reloads a specific plugin
func (api *APIServer) handleReloadPlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		api.sendError(w, "Plugin name is required", "MISSING_PARAMETER", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := api.manager.ReloadPlugin(ctx, name); err != nil {
		api.sendError(w, fmt.Sprintf("Failed to reload plugin: %v", err), "RELOAD_FAILED", http.StatusInternalServerError)
		return
	}

	response := PluginActionResponse{
		Success:   true,
		Message:   fmt.Sprintf("Plugin %s reloaded successfully", name),
		Timestamp: time.Now(),
	}

	api.sendJSON(w, response)
}

// handleUnloadPlugin unloads a specific plugin
func (api *APIServer) handleUnloadPlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		api.sendError(w, "Plugin name is required", "MISSING_PARAMETER", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := api.manager.UnloadPlugin(ctx, name); err != nil {
		api.sendError(w, fmt.Sprintf("Failed to unload plugin: %v", err), "UNLOAD_FAILED", http.StatusInternalServerError)
		return
	}

	response := PluginActionResponse{
		Success:   true,
		Message:   fmt.Sprintf("Plugin %s unloaded successfully", name),
		Timestamp: time.Now(),
	}

	api.sendJSON(w, response)
}

// handleLoadPlugin loads a new plugin
func (api *APIServer) handleLoadPlugin(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path string `json:"path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		api.sendError(w, "Invalid request body", "INVALID_JSON", http.StatusBadRequest)
		return
	}

	if request.Path == "" {
		api.sendError(w, "Plugin path is required", "MISSING_PARAMETER", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := api.manager.LoadPlugin(ctx, request.Path); err != nil {
		api.sendError(w, fmt.Sprintf("Failed to load plugin: %v", err), "LOAD_FAILED", http.StatusInternalServerError)
		return
	}

	response := PluginActionResponse{
		Success:   true,
		Message:   fmt.Sprintf("Plugin loaded successfully from %s", request.Path),
		Timestamp: time.Now(),
	}

	api.sendJSON(w, response)
}

// handleGetPluginHealth returns health status for a specific plugin
func (api *APIServer) handleGetPluginHealth(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		api.sendError(w, "Plugin name is required", "MISSING_PARAMETER", http.StatusBadRequest)
		return
	}

	health := api.getPluginHealth(name)
	if health == nil {
		api.sendError(w, "Plugin not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	api.sendJSON(w, health)
}

// handleGetAllPluginHealth returns health status for all plugins
func (api *APIServer) handleGetAllPluginHealth(w http.ResponseWriter, r *http.Request) {
	plugins := api.manager.ListPlugins()
	healthMap := make(map[string]*PluginHealthResponse)

	for name := range plugins {
		if health := api.getPluginHealth(name); health != nil {
			healthMap[name] = health
		}
	}

	api.sendJSON(w, map[string]interface{}{
		"plugins": healthMap,
		"total":   len(healthMap),
	})
}

// handleGetPluginProcess returns process information for a plugin
func (api *APIServer) handleGetPluginProcess(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		api.sendError(w, "Plugin name is required", "MISSING_PARAMETER", http.StatusBadRequest)
		return
	}

	if api.sandbox == nil {
		api.sendError(w, "Sandbox not available", "SANDBOX_DISABLED", http.StatusServiceUnavailable)
		return
	}

	process, err := api.sandbox.GetProcess(name)
	if err != nil {
		api.sendError(w, fmt.Sprintf("Process not found: %v", err), "NOT_FOUND", http.StatusNotFound)
		return
	}

	api.sendJSON(w, process)
}

// handleRestartPlugin restarts a plugin process
func (api *APIServer) handleRestartPlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		api.sendError(w, "Plugin name is required", "MISSING_PARAMETER", http.StatusBadRequest)
		return
	}

	if api.sandbox == nil {
		api.sendError(w, "Sandbox not available", "SANDBOX_DISABLED", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := api.sandbox.RestartPlugin(ctx, name); err != nil {
		api.sendError(w, fmt.Sprintf("Failed to restart plugin: %v", err), "RESTART_FAILED", http.StatusInternalServerError)
		return
	}

	response := PluginActionResponse{
		Success:   true,
		Message:   fmt.Sprintf("Plugin %s restarted successfully", name),
		Timestamp: time.Now(),
	}

	api.sendJSON(w, response)
}

// handleGetPluginMetrics returns metrics for a specific plugin
func (api *APIServer) handleGetPluginMetrics(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		api.sendError(w, "Plugin name is required", "MISSING_PARAMETER", http.StatusBadRequest)
		return
	}

	// This is a simplified implementation
	// In a full implementation, you would collect detailed metrics
	plugin, err := api.manager.GetPlugin(name)
	if err != nil {
		api.sendError(w, fmt.Sprintf("Plugin not found: %v", err), "NOT_FOUND", http.StatusNotFound)
		return
	}

	metrics := map[string]interface{}{
		"plugin_name":  plugin.Name,
		"status":       plugin.Status,
		"load_time":    plugin.LoadTime,
		"last_used":    plugin.LastUsed,
		"error_count":  plugin.ErrorCount,
		"version":      plugin.Version,
	}

	// Add process metrics if available
	if api.sandbox != nil {
		if process, err := api.sandbox.GetProcess(name); err == nil {
			metrics["process"] = map[string]interface{}{
				"pid":            process.PID,
				"start_time":     process.StartTime,
				"restart_count":  process.RestartCount,
				"resource_usage": process.ResourceUsage,
			}
		}
	}

	api.sendJSON(w, metrics)
}

// handleGetAllPluginMetrics returns metrics for all plugins
func (api *APIServer) handleGetAllPluginMetrics(w http.ResponseWriter, r *http.Request) {
	plugins := api.manager.ListPlugins()
	metricsMap := make(map[string]interface{})

	for name := range plugins {
		// Get basic plugin metrics
		if plugin, err := api.manager.GetPlugin(name); err == nil {
			metrics := map[string]interface{}{
				"plugin_name": plugin.Name,
				"status":      plugin.Status,
				"load_time":   plugin.LoadTime,
				"last_used":   plugin.LastUsed,
				"error_count": plugin.ErrorCount,
				"version":     plugin.Version,
			}

			// Add process metrics if available
			if api.sandbox != nil {
				if process, err := api.sandbox.GetProcess(name); err == nil {
					metrics["process"] = map[string]interface{}{
						"pid":            process.PID,
						"start_time":     process.StartTime,
						"restart_count":  process.RestartCount,
						"resource_usage": process.ResourceUsage,
					}
				}
			}

			metricsMap[name] = metrics
		}
	}

	api.sendJSON(w, map[string]interface{}{
		"plugins": metricsMap,
		"total":   len(metricsMap),
	})
}

// getPluginHealth returns health information for a plugin
func (api *APIServer) getPluginHealth(name string) *PluginHealthResponse {
	plugin, err := api.manager.GetPlugin(name)
	if err != nil {
		return nil
	}

	health := &PluginHealthResponse{
		Name:      plugin.Name,
		Status:    plugin.Status,
		Healthy:   plugin.Status == PluginStatusActive,
		LastCheck: time.Now(),
		Details:   make(map[string]interface{}),
	}

	// Add error information if plugin has errors
	if plugin.ErrorCount > 0 {
		health.Healthy = false
		health.Details["error_count"] = plugin.ErrorCount
		health.Details["last_error"] = plugin.LastError
	}

	// Add process information if available
	if api.sandbox != nil {
		if process, err := api.sandbox.GetProcess(name); err == nil {
			health.Process = process
			health.Details["process_status"] = process.Status
			health.Details["restart_count"] = process.RestartCount
		}
	}

	// Add load time and usage information
	health.Details["load_time"] = plugin.LoadTime
	health.Details["last_used"] = plugin.LastUsed
	health.Details["version"] = plugin.Version

	return health
}

// sendJSON sends a JSON response
func (api *APIServer) sendJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	if err := json.NewEncoder(w).Encode(data); err != nil {
		api.logger.Error("Failed to encode JSON response", "error", err)
	}
}

// sendError sends an error response
func (api *APIServer) sendError(w http.ResponseWriter, message, code string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	errorResponse := ErrorResponse{
		Error:     message,
		Code:      code,
		Timestamp: time.Now(),
	}
	
	if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
		api.logger.Error("Failed to encode error response", "error", err)
	}

	api.logger.Debug("API error response", 
		"code", code, 
		"message", message, 
		"status", statusCode)
}