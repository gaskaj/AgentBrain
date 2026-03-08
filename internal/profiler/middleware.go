package profiler

import (
	"context"
	"time"
)

// Middleware provides profiling integration for existing components
type Middleware struct {
	profiler *Profiler
}

// NewMiddleware creates profiling middleware
func NewMiddleware(profiler *Profiler) *Middleware {
	return &Middleware{
		profiler: profiler,
	}
}

// TrackOperation wraps an operation with profiling
func (m *Middleware) TrackOperation(name string, operation func() error) error {
	return m.TrackOperationWithContext(context.Background(), name, func(ctx context.Context) error {
		return operation()
	})
}

// TrackOperationWithContext wraps an operation with profiling and context
func (m *Middleware) TrackOperationWithContext(ctx context.Context, name string, operation func(context.Context) error) error {
	if m.profiler == nil || !m.profiler.IsEnabled() {
		return operation(ctx)
	}

	start := time.Now()
	err := operation(ctx)
	duration := time.Since(start)

	metadata := map[string]interface{}{
		"success": err == nil,
	}

	if err != nil {
		m.profiler.analytics.TrackError(name)
		metadata["error"] = err.Error()
	}

	m.profiler.TrackOperation(name, duration, metadata)
	return err
}

// TrackLLMCall specifically tracks LLM API calls
func (m *Middleware) TrackLLMCall(provider string, messageCount int, operation func() error) error {
	if m.profiler == nil || !m.profiler.IsEnabled() {
		return operation()
	}

	start := time.Now()
	err := operation()
	duration := time.Since(start)

	operationName := "llm_call_" + provider
	metadata := map[string]interface{}{
		"provider":      provider,
		"message_count": messageCount,
		"success":       err == nil,
	}

	if err != nil {
		m.profiler.analytics.TrackError(operationName)
		metadata["error"] = err.Error()
	}

	m.profiler.TrackOperation(operationName, duration, metadata)
	return err
}

// TrackWorkspaceOperation tracks workspace-related operations
func (m *Middleware) TrackWorkspaceOperation(operation string, operation_func func() error) error {
	if m.profiler == nil || !m.profiler.IsEnabled() {
		return operation_func()
	}

	start := time.Now()
	err := operation_func()
	duration := time.Since(start)

	operationName := "workspace_" + operation
	metadata := map[string]interface{}{
		"operation": operation,
		"success":   err == nil,
	}

	if err != nil {
		m.profiler.analytics.TrackError(operationName)
		metadata["error"] = err.Error()
	}

	m.profiler.TrackOperation(operationName, duration, metadata)
	return err
}

// TrackGitOperation tracks Git-related operations
func (m *Middleware) TrackGitOperation(operation string, repoSize int64, operation_func func() error) error {
	if m.profiler == nil || !m.profiler.IsEnabled() {
		return operation_func()
	}

	start := time.Now()
	err := operation_func()
	duration := time.Since(start)

	operationName := "git_" + operation
	metadata := map[string]interface{}{
		"operation":     operation,
		"repo_size_mb":  repoSize / (1024 * 1024),
		"success":       err == nil,
	}

	if err != nil {
		m.profiler.analytics.TrackError(operationName)
		metadata["error"] = err.Error()
	}

	m.profiler.TrackOperation(operationName, duration, metadata)
	return err
}

// TrackToolExecution tracks tool execution in the agent loop
func (m *Middleware) TrackToolExecution(toolName string, operation func() error) error {
	if m.profiler == nil || !m.profiler.IsEnabled() {
		return operation()
	}

	start := time.Now()
	err := operation()
	duration := time.Since(start)

	operationName := "tool_" + toolName
	metadata := map[string]interface{}{
		"tool":    toolName,
		"success": err == nil,
	}

	if err != nil {
		m.profiler.analytics.TrackError(operationName)
		metadata["error"] = err.Error()
	}

	m.profiler.TrackOperation(operationName, duration, metadata)
	return err
}

// TrackAPICall tracks external API calls
func (m *Middleware) TrackAPICall(api string, endpoint string, operation func() error) error {
	if m.profiler == nil || !m.profiler.IsEnabled() {
		return operation()
	}

	start := time.Now()
	err := operation()
	duration := time.Since(start)

	operationName := "api_" + api
	metadata := map[string]interface{}{
		"api":      api,
		"endpoint": endpoint,
		"success":  err == nil,
	}

	if err != nil {
		m.profiler.analytics.TrackError(operationName)
		metadata["error"] = err.Error()
	}

	m.profiler.TrackOperation(operationName, duration, metadata)
	return err
}

// TrackFileOperation tracks file I/O operations
func (m *Middleware) TrackFileOperation(operation string, filePath string, fileSize int64, operation_func func() error) error {
	if m.profiler == nil || !m.profiler.IsEnabled() {
		return operation_func()
	}

	start := time.Now()
	err := operation_func()
	duration := time.Since(start)

	operationName := "file_" + operation
	metadata := map[string]interface{}{
		"operation":    operation,
		"file_path":    filePath,
		"file_size_kb": fileSize / 1024,
		"success":      err == nil,
	}

	if err != nil {
		m.profiler.analytics.TrackError(operationName)
		metadata["error"] = err.Error()
	}

	m.profiler.TrackOperation(operationName, duration, metadata)
	return err
}

// StartTransaction begins a profiling transaction that can span multiple operations
func (m *Middleware) StartTransaction(name string) *Transaction {
	return &Transaction{
		middleware: m,
		name:       name,
		startTime:  time.Now(),
		metadata:   make(map[string]interface{}),
	}
}

// Transaction represents a profiling transaction
type Transaction struct {
	middleware *Middleware
	name       string
	startTime  time.Time
	metadata   map[string]interface{}
}

// AddMetadata adds metadata to the transaction
func (t *Transaction) AddMetadata(key string, value interface{}) {
	t.metadata[key] = value
}

// Finish completes the transaction and records metrics
func (t *Transaction) Finish(err error) {
	if t.middleware.profiler == nil || !t.middleware.profiler.IsEnabled() {
		return
	}

	duration := time.Since(t.startTime)
	t.metadata["success"] = err == nil

	if err != nil {
		t.middleware.profiler.analytics.TrackError(t.name)
		t.metadata["error"] = err.Error()
	}

	t.middleware.profiler.TrackOperation(t.name, duration, t.metadata)
}

// WithProfiling is a helper function to wrap any operation with profiling
func WithProfiling(profiler *Profiler, name string, operation func() error) error {
	if profiler == nil || !profiler.IsEnabled() {
		return operation()
	}

	middleware := NewMiddleware(profiler)
	return middleware.TrackOperation(name, operation)
}