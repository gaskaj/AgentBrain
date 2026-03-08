package contracts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ContractVersion represents a versioned API contract
type ContractVersion struct {
	Version     string    `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	Description string    `json:"description,omitempty"`
}

// APIContract defines the expected behavior of an external API
type APIContract struct {
	Name        string                     `json:"name"`
	BaseURL     string                     `json:"base_url"`
	Version     ContractVersion            `json:"version"`
	Endpoints   map[string]EndpointContract `json:"endpoints"`
	Headers     map[string]string          `json:"common_headers,omitempty"`
	Auth        AuthContract               `json:"auth,omitempty"`
}

// EndpointContract defines expectations for a specific API endpoint
type EndpointContract struct {
	Method          string                 `json:"method"`
	Path            string                 `json:"path"`
	QueryParams     map[string]ParamSpec   `json:"query_params,omitempty"`
	RequestHeaders  map[string]string      `json:"request_headers,omitempty"`
	RequestSchema   *JSONSchema            `json:"request_schema,omitempty"`
	ResponseSchemas map[int]*JSONSchema    `json:"response_schemas"` // status code -> schema
	Examples        []ExampleRequest       `json:"examples,omitempty"`
	Timeout         time.Duration          `json:"timeout,omitempty"`
	RateLimiting    *RateLimitSpec         `json:"rate_limiting,omitempty"`
}

// ParamSpec defines a query parameter specification
type ParamSpec struct {
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Default     string   `json:"default,omitempty"`
	EnumValues  []string `json:"enum_values,omitempty"`
	Description string   `json:"description,omitempty"`
}

// JSONSchema represents a simplified JSON schema for request/response validation
type JSONSchema struct {
	Type        string                 `json:"type"`
	Properties  map[string]*JSONSchema `json:"properties,omitempty"`
	Items       *JSONSchema            `json:"items,omitempty"`
	Required    []string               `json:"required,omitempty"`
	Enum        []interface{}          `json:"enum,omitempty"`
	Pattern     string                 `json:"pattern,omitempty"`
	MinLength   *int                   `json:"min_length,omitempty"`
	MaxLength   *int                   `json:"max_length,omitempty"`
	Minimum     *float64               `json:"minimum,omitempty"`
	Maximum     *float64               `json:"maximum,omitempty"`
	Description string                 `json:"description,omitempty"`
}

// AuthContract defines authentication requirements
type AuthContract struct {
	Type   string            `json:"type"` // "bearer", "api_key", "oauth2", "basic"
	Config map[string]string `json:"config,omitempty"`
}

// RateLimitSpec defines rate limiting expectations
type RateLimitSpec struct {
	RequestsPerSecond int           `json:"requests_per_second"`
	BurstSize         int           `json:"burst_size,omitempty"`
	ResetWindow       time.Duration `json:"reset_window,omitempty"`
}

// ExampleRequest provides concrete examples for testing
type ExampleRequest struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	Request      RequestExample         `json:"request"`
	Response     ResponseExample        `json:"response"`
	Prerequisites []string              `json:"prerequisites,omitempty"`
}

// RequestExample represents an example API request
type RequestExample struct {
	QueryParams map[string]string      `json:"query_params,omitempty"`
	Headers     map[string]string      `json:"headers,omitempty"`
	Body        map[string]interface{} `json:"body,omitempty"`
}

// ResponseExample represents an expected API response
type ResponseExample struct {
	StatusCode int                    `json:"status_code"`
	Headers    map[string]string      `json:"headers,omitempty"`
	Body       map[string]interface{} `json:"body,omitempty"`
}

// ValidationResult represents the outcome of contract validation
type ValidationResult struct {
	ContractName string                    `json:"contract_name"`
	Endpoint     string                    `json:"endpoint"`
	Success      bool                      `json:"success"`
	Timestamp    time.Time                 `json:"timestamp"`
	Violations   []ContractViolation       `json:"violations,omitempty"`
	Metrics      ValidationMetrics         `json:"metrics"`
}

// ContractViolation represents a specific contract violation
type ContractViolation struct {
	Type        ViolationType `json:"type"`
	Field       string        `json:"field,omitempty"`
	Expected    string        `json:"expected"`
	Actual      string        `json:"actual"`
	Description string        `json:"description"`
	Severity    Severity      `json:"severity"`
}

// ViolationType categorizes different types of contract violations
type ViolationType string

const (
	ViolationTypeSchema       ViolationType = "schema"
	ViolationTypeStatusCode   ViolationType = "status_code"
	ViolationTypeHeaders      ViolationType = "headers"
	ViolationTypeTimeout      ViolationType = "timeout"
	ViolationTypeRateLimit    ViolationType = "rate_limit"
	ViolationTypeAuth         ViolationType = "auth"
	ViolationTypeConnectivity ViolationType = "connectivity"
)

// Severity indicates the impact level of a violation
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// ValidationMetrics captures performance and reliability metrics
type ValidationMetrics struct {
	ResponseTime  time.Duration `json:"response_time"`
	RequestSize   int64         `json:"request_size"`
	ResponseSize  int64         `json:"response_size"`
	RetryCount    int           `json:"retry_count"`
	ErrorRate     float64       `json:"error_rate"`
}

// ContractRecording represents a recorded API interaction
type ContractRecording struct {
	Timestamp   time.Time              `json:"timestamp"`
	RequestID   string                 `json:"request_id"`
	Method      string                 `json:"method"`
	URL         string                 `json:"url"`
	Headers     map[string]string      `json:"headers"`
	QueryParams map[string]string      `json:"query_params"`
	RequestBody map[string]interface{} `json:"request_body,omitempty"`
	StatusCode  int                    `json:"status_code"`
	ResponseHeaders map[string]string      `json:"response_headers"`
	ResponseBody    map[string]interface{} `json:"response_body,omitempty"`
	Duration    time.Duration          `json:"duration"`
	Error       string                 `json:"error,omitempty"`
}

// ContractDiff represents differences between contract versions
type ContractDiff struct {
	FromVersion ContractVersion   `json:"from_version"`
	ToVersion   ContractVersion   `json:"to_version"`
	Changes     []ContractChange  `json:"changes"`
	Summary     DiffSummary       `json:"summary"`
}

// ContractChange represents a specific change between contract versions
type ContractChange struct {
	Type        ChangeType `json:"type"`
	Path        string     `json:"path"`
	Description string     `json:"description"`
	OldValue    string     `json:"old_value,omitempty"`
	NewValue    string     `json:"new_value,omitempty"`
	Impact      Impact     `json:"impact"`
}

// ChangeType categorizes different types of contract changes
type ChangeType string

const (
	ChangeTypeAdded     ChangeType = "added"
	ChangeTypeRemoved   ChangeType = "removed"
	ChangeTypeModified  ChangeType = "modified"
	ChangeTypeDeprecated ChangeType = "deprecated"
)

// Impact indicates the potential impact of a contract change
type Impact string

const (
	ImpactBreaking    Impact = "breaking"
	ImpactNonBreaking Impact = "non-breaking"
	ImpactUnknown     Impact = "unknown"
)

// DiffSummary provides a high-level overview of contract changes
type DiffSummary struct {
	TotalChanges    int `json:"total_changes"`
	BreakingChanges int `json:"breaking_changes"`
	AddedEndpoints  int `json:"added_endpoints"`
	RemovedEndpoints int `json:"removed_endpoints"`
	ModifiedEndpoints int `json:"modified_endpoints"`
}

// Validator defines the interface for contract validation
type Validator interface {
	ValidateContract(contract *APIContract, recording *ContractRecording) (*ValidationResult, error)
	ValidateEndpoint(endpoint *EndpointContract, recording *ContractRecording) (*ValidationResult, error)
	ValidateSchema(schema *JSONSchema, data map[string]interface{}) []ContractViolation
}

// Recorder defines the interface for recording API interactions
type Recorder interface {
	Record(req *http.Request, resp *http.Response, duration time.Duration, err error) (*ContractRecording, error)
	EnableRecording(enabled bool)
	IsRecordingEnabled() bool
}

// ContractStore defines the interface for storing and retrieving contracts
type ContractStore interface {
	SaveContract(contract *APIContract) error
	LoadContract(name, version string) (*APIContract, error)
	ListContracts() ([]APIContract, error)
	SaveRecording(recording *ContractRecording) error
	LoadRecordings(contractName string, limit int) ([]*ContractRecording, error)
}

// MockValidator defines the interface for mock validation against contracts
type MockValidator interface {
	ValidateMockFidelity(contractName string, mockResponses map[string]interface{}) (*ValidationResult, error)
	UpdateMockFromContract(contractName string) error
}

// String returns a human-readable representation of the violation
func (v ContractViolation) String() string {
	if v.Field != "" {
		return fmt.Sprintf("%s violation in field '%s': expected %s, got %s", v.Type, v.Field, v.Expected, v.Actual)
	}
	return fmt.Sprintf("%s violation: expected %s, got %s", v.Type, v.Expected, v.Actual)
}

// IsBreaking returns true if the contract change is breaking
func (c ContractChange) IsBreaking() bool {
	return c.Impact == ImpactBreaking
}

// HasBreakingChanges returns true if the diff contains breaking changes
func (d ContractDiff) HasBreakingChanges() bool {
	return d.Summary.BreakingChanges > 0
}