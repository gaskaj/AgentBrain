package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// HTTPRecorder implements the Recorder interface for HTTP interactions
type HTTPRecorder struct {
	enabled    bool
	store      ContractStore
	apiFilters map[string]APIFilter // API name -> filter
}

// APIFilter defines which requests to record for a specific API
type APIFilter struct {
	BaseURL     string   `json:"base_url"`
	Endpoints   []string `json:"endpoints,omitempty"` // If empty, record all
	ExcludeAuth bool     `json:"exclude_auth"`        // Whether to exclude auth headers
}

// NewHTTPRecorder creates a new HTTP recorder
func NewHTTPRecorder(store ContractStore) *HTTPRecorder {
	return &HTTPRecorder{
		enabled:    false,
		store:      store,
		apiFilters: make(map[string]APIFilter),
	}
}

// EnableRecording enables or disables recording
func (r *HTTPRecorder) EnableRecording(enabled bool) {
	r.enabled = enabled
}

// IsRecordingEnabled returns the current recording state
func (r *HTTPRecorder) IsRecordingEnabled() bool {
	return r.enabled
}

// AddAPIFilter adds a filter for a specific API
func (r *HTTPRecorder) AddAPIFilter(apiName string, filter APIFilter) {
	r.apiFilters[apiName] = filter
}

// Record captures an HTTP request/response interaction
func (r *HTTPRecorder) Record(req *http.Request, resp *http.Response, duration time.Duration, err error) (*ContractRecording, error) {
	if !r.enabled {
		return nil, nil // Recording disabled
	}

	if !r.shouldRecord(req) {
		return nil, nil // Filtered out
	}

	recording := &ContractRecording{
		Timestamp: time.Now(),
		RequestID: uuid.New().String(),
		Method:    req.Method,
		URL:       req.URL.String(),
		Duration:  duration,
	}

	// Capture request details
	if err := r.captureRequest(req, recording); err != nil {
		return nil, fmt.Errorf("capture request: %w", err)
	}

	// Capture response details
	if resp != nil {
		if err := r.captureResponse(resp, recording); err != nil {
			return nil, fmt.Errorf("capture response: %w", err)
		}
	}

	// Capture error if present
	if err != nil {
		recording.Error = err.Error()
	}

	// Store the recording
	if r.store != nil {
		if err := r.store.SaveRecording(recording); err != nil {
			return nil, fmt.Errorf("save recording: %w", err)
		}
	}

	return recording, nil
}

// shouldRecord determines if a request should be recorded based on filters
func (r *HTTPRecorder) shouldRecord(req *http.Request) bool {
	if len(r.apiFilters) == 0 {
		return true // No filters means record everything
	}

	for _, filter := range r.apiFilters {
		if r.matchesFilter(req, filter) {
			return true
		}
	}

	return false
}

// matchesFilter checks if a request matches a specific API filter
func (r *HTTPRecorder) matchesFilter(req *http.Request, filter APIFilter) bool {
	// Check base URL match
	if filter.BaseURL != "" {
		reqBaseURL := fmt.Sprintf("%s://%s", req.URL.Scheme, req.URL.Host)
		if !strings.HasPrefix(reqBaseURL, filter.BaseURL) {
			return false
		}
	}

	// Check endpoint filters
	if len(filter.Endpoints) > 0 {
		matched := false
		for _, endpoint := range filter.Endpoints {
			if strings.HasSuffix(req.URL.Path, endpoint) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

// captureRequest extracts request details for recording
func (r *HTTPRecorder) captureRequest(req *http.Request, recording *ContractRecording) error {
	// Capture headers
	recording.Headers = make(map[string]string)
	for name, values := range req.Header {
		if len(values) > 0 {
			// Filter out sensitive headers if needed
			if r.shouldCaptureHeader(name) {
				recording.Headers[name] = values[0] // Take first value
			}
		}
	}

	// Capture query parameters
	recording.QueryParams = make(map[string]string)
	for name, values := range req.URL.Query() {
		if len(values) > 0 {
			recording.QueryParams[name] = values[0] // Take first value
		}
	}

	// Capture request body if present
	if req.Body != nil {
		bodyBytes, err := r.readBody(req.Body)
		if err != nil {
			return fmt.Errorf("read request body: %w", err)
		}

		// Try to parse as JSON
		if len(bodyBytes) > 0 {
			var bodyJSON map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &bodyJSON); err == nil {
				recording.RequestBody = bodyJSON
			}
		}

		// Restore body for the actual request
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	return nil
}

// captureResponse extracts response details for recording
func (r *HTTPRecorder) captureResponse(resp *http.Response, recording *ContractRecording) error {
	recording.StatusCode = resp.StatusCode

	// Capture response headers
	recording.ResponseHeaders = make(map[string]string)
	for name, values := range resp.Header {
		if len(values) > 0 {
			recording.ResponseHeaders[name] = values[0] // Take first value
		}
	}

	// Capture response body if present
	if resp.Body != nil {
		bodyBytes, err := r.readBody(resp.Body)
		if err != nil {
			return fmt.Errorf("read response body: %w", err)
		}

		// Try to parse as JSON
		if len(bodyBytes) > 0 {
			var bodyJSON map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &bodyJSON); err == nil {
				recording.ResponseBody = bodyJSON
			}
		}

		// Restore body for the caller
		resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	return nil
}

// readBody reads the entire body and returns the bytes
func (r *HTTPRecorder) readBody(body io.ReadCloser) ([]byte, error) {
	defer body.Close()
	return io.ReadAll(body)
}

// shouldCaptureHeader determines if a header should be included in the recording
func (r *HTTPRecorder) shouldCaptureHeader(headerName string) bool {
	// Skip sensitive headers
	lowerName := strings.ToLower(headerName)
	sensitiveHeaders := []string{
		"authorization",
		"cookie",
		"x-api-key",
		"x-auth-token",
	}

	for _, sensitive := range sensitiveHeaders {
		if lowerName == sensitive {
			return false
		}
	}

	return true
}

// RecordingTransport is an HTTP transport that automatically records requests/responses
type RecordingTransport struct {
	base     http.RoundTripper
	recorder *HTTPRecorder
}

// NewRecordingTransport creates a new recording transport wrapper
func NewRecordingTransport(base http.RoundTripper, recorder *HTTPRecorder) *RecordingTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &RecordingTransport{
		base:     base,
		recorder: recorder,
	}
}

// RoundTrip executes the HTTP request and records the interaction
func (t *RecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	
	// Make the actual request
	resp, err := t.base.RoundTrip(req)
	duration := time.Since(start)

	// Record the interaction
	if t.recorder != nil {
		_, recordErr := t.recorder.Record(req, resp, duration, err)
		if recordErr != nil {
			// Log recording error but don't fail the request
			// In a real implementation, you'd want proper logging
			fmt.Printf("Warning: failed to record HTTP interaction: %v\n", recordErr)
		}
	}

	return resp, err
}

// RecordingHTTPClient creates an HTTP client with automatic recording
func RecordingHTTPClient(recorder *HTTPRecorder) *http.Client {
	return &http.Client{
		Transport: NewRecordingTransport(nil, recorder),
		Timeout:   30 * time.Second,
	}
}

// MockRecorder is a simple in-memory recorder for testing
type MockRecorder struct {
	enabled    bool
	recordings []*ContractRecording
}

// NewMockRecorder creates a new mock recorder
func NewMockRecorder() *MockRecorder {
	return &MockRecorder{
		enabled:    false,
		recordings: []*ContractRecording{},
	}
}

// EnableRecording enables or disables recording
func (m *MockRecorder) EnableRecording(enabled bool) {
	m.enabled = enabled
}

// IsRecordingEnabled returns the current recording state
func (m *MockRecorder) IsRecordingEnabled() bool {
	return m.enabled
}

// Record captures a request/response for testing
func (m *MockRecorder) Record(req *http.Request, resp *http.Response, duration time.Duration, err error) (*ContractRecording, error) {
	if !m.enabled {
		return nil, nil
	}

	recording := &ContractRecording{
		Timestamp: time.Now(),
		RequestID: uuid.New().String(),
		Method:    req.Method,
		URL:       req.URL.String(),
		Duration:  duration,
		Headers:   make(map[string]string),
		QueryParams: make(map[string]string),
	}

	// Capture basic request details
	for name, values := range req.Header {
		if len(values) > 0 {
			recording.Headers[name] = values[0]
		}
	}

	for name, values := range req.URL.Query() {
		if len(values) > 0 {
			recording.QueryParams[name] = values[0]
		}
	}

	// Capture response details
	if resp != nil {
		recording.StatusCode = resp.StatusCode
		recording.ResponseHeaders = make(map[string]string)
		for name, values := range resp.Header {
			if len(values) > 0 {
				recording.ResponseHeaders[name] = values[0]
			}
		}
	}

	if err != nil {
		recording.Error = err.Error()
	}

	m.recordings = append(m.recordings, recording)
	return recording, nil
}

// GetRecordings returns all recorded interactions
func (m *MockRecorder) GetRecordings() []*ContractRecording {
	return m.recordings
}

// Clear removes all recorded interactions
func (m *MockRecorder) Clear() {
	m.recordings = []*ContractRecording{}
}