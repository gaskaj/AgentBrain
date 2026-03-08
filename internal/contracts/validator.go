package contracts

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultValidator implements the Validator interface with comprehensive validation logic
type DefaultValidator struct {
	config ValidationConfig
}

// ValidationConfig configures the validation behavior
type ValidationConfig struct {
	StrictMode      bool          `yaml:"strict_mode"`
	TimeoutTolerance time.Duration `yaml:"timeout_tolerance"`
	MaxRetries      int           `yaml:"max_retries"`
	IgnoredFields   []string      `yaml:"ignored_fields"`
}

// NewValidator creates a new DefaultValidator with the provided configuration
func NewValidator(config ValidationConfig) *DefaultValidator {
	return &DefaultValidator{
		config: config,
	}
}

// ValidateContract validates a complete API contract against recorded interactions
func (v *DefaultValidator) ValidateContract(contract *APIContract, recording *ContractRecording) (*ValidationResult, error) {
	if contract == nil {
		return nil, fmt.Errorf("contract cannot be nil")
	}
	if recording == nil {
		return nil, fmt.Errorf("recording cannot be nil")
	}

	result := &ValidationResult{
		ContractName: contract.Name,
		Endpoint:     fmt.Sprintf("%s %s", recording.Method, recording.URL),
		Success:      true,
		Timestamp:    time.Now(),
		Violations:   []ContractViolation{},
		Metrics: ValidationMetrics{
			ResponseTime: recording.Duration,
			RequestSize:  int64(len(fmt.Sprintf("%v", recording.RequestBody))),
			ResponseSize: int64(len(fmt.Sprintf("%v", recording.ResponseBody))),
		},
	}

	// Find the matching endpoint contract
	endpointContract := v.findMatchingEndpoint(contract, recording)
	if endpointContract == nil {
		violation := ContractViolation{
			Type:        ViolationTypeConnectivity,
			Expected:    "matching endpoint in contract",
			Actual:      fmt.Sprintf("%s %s", recording.Method, recording.URL),
			Description: "no matching endpoint found in contract",
			Severity:    SeverityCritical,
		}
		result.Violations = append(result.Violations, violation)
		result.Success = false
		return result, nil
	}

	// Validate the endpoint
	endpointResult, err := v.ValidateEndpoint(endpointContract, recording)
	if err != nil {
		return nil, fmt.Errorf("validate endpoint: %w", err)
	}

	// Merge results
	result.Violations = append(result.Violations, endpointResult.Violations...)
	if !endpointResult.Success {
		result.Success = false
	}

	return result, nil
}

// ValidateEndpoint validates a specific endpoint contract against a recording
func (v *DefaultValidator) ValidateEndpoint(endpoint *EndpointContract, recording *ContractRecording) (*ValidationResult, error) {
	result := &ValidationResult{
		Endpoint:  fmt.Sprintf("%s %s", endpoint.Method, endpoint.Path),
		Success:   true,
		Timestamp: time.Now(),
		Violations: []ContractViolation{},
		Metrics: ValidationMetrics{
			ResponseTime: recording.Duration,
		},
	}

	// Validate HTTP method
	if strings.ToUpper(endpoint.Method) != strings.ToUpper(recording.Method) {
		violation := ContractViolation{
			Type:        ViolationTypeSchema,
			Field:       "method",
			Expected:    endpoint.Method,
			Actual:      recording.Method,
			Description: "HTTP method mismatch",
			Severity:    SeverityHigh,
		}
		result.Violations = append(result.Violations, violation)
		result.Success = false
	}

	// Validate status code
	violations := v.validateStatusCode(endpoint, recording)
	result.Violations = append(result.Violations, violations...)

	// Validate response headers
	violations = v.validateResponseHeaders(endpoint, recording)
	result.Violations = append(result.Violations, violations...)

	// Validate response schema
	violations = v.validateResponseSchema(endpoint, recording)
	result.Violations = append(result.Violations, violations...)

	// Validate timeout
	violations = v.validateTimeout(endpoint, recording)
	result.Violations = append(result.Violations, violations...)

	// Update success status
	for _, violation := range result.Violations {
		if violation.Severity == SeverityHigh || violation.Severity == SeverityCritical {
			result.Success = false
			break
		}
	}

	return result, nil
}

// ValidateSchema validates data against a JSON schema
func (v *DefaultValidator) ValidateSchema(schema *JSONSchema, data map[string]interface{}) []ContractViolation {
	var violations []ContractViolation
	
	if schema == nil {
		return violations
	}

	violations = append(violations, v.validateSchemaRecursive("", schema, data)...)
	return violations
}

// validateSchemaRecursive performs recursive schema validation
func (v *DefaultValidator) validateSchemaRecursive(path string, schema *JSONSchema, data interface{}) []ContractViolation {
	var violations []ContractViolation

	if schema == nil {
		return violations
	}

	// Validate type
	if schema.Type != "" {
		expectedType := schema.Type
		actualType := v.getJSONType(data)
		
		if expectedType != actualType {
			violations = append(violations, ContractViolation{
				Type:        ViolationTypeSchema,
				Field:       path,
				Expected:    fmt.Sprintf("type: %s", expectedType),
				Actual:      fmt.Sprintf("type: %s", actualType),
				Description: fmt.Sprintf("type mismatch at path %s", path),
				Severity:    SeverityMedium,
			})
			return violations // Skip further validation if type is wrong
		}
	}

	// Validate enum values
	if len(schema.Enum) > 0 {
		found := false
		for _, enumValue := range schema.Enum {
			if reflect.DeepEqual(data, enumValue) {
				found = true
				break
			}
		}
		if !found {
			violations = append(violations, ContractViolation{
				Type:        ViolationTypeSchema,
				Field:       path,
				Expected:    fmt.Sprintf("one of %v", schema.Enum),
				Actual:      fmt.Sprintf("%v", data),
				Description: fmt.Sprintf("value not in enum at path %s", path),
				Severity:    SeverityMedium,
			})
		}
	}

	// Type-specific validations
	switch schema.Type {
	case "object":
		violations = append(violations, v.validateObjectSchema(path, schema, data)...)
	case "array":
		violations = append(violations, v.validateArraySchema(path, schema, data)...)
	case "string":
		violations = append(violations, v.validateStringSchema(path, schema, data)...)
	case "number", "integer":
		violations = append(violations, v.validateNumberSchema(path, schema, data)...)
	}

	return violations
}

// validateObjectSchema validates object schema constraints
func (v *DefaultValidator) validateObjectSchema(path string, schema *JSONSchema, data interface{}) []ContractViolation {
	var violations []ContractViolation

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return violations // Type validation handled elsewhere
	}

	// Validate required properties
	for _, required := range schema.Required {
		if _, exists := dataMap[required]; !exists {
			violations = append(violations, ContractViolation{
				Type:        ViolationTypeSchema,
				Field:       v.joinPath(path, required),
				Expected:    "required property",
				Actual:      "missing",
				Description: fmt.Sprintf("required property %s is missing", required),
				Severity:    SeverityHigh,
			})
		}
	}

	// Validate property schemas
	for propName, propSchema := range schema.Properties {
		if propValue, exists := dataMap[propName]; exists {
			propPath := v.joinPath(path, propName)
			violations = append(violations, v.validateSchemaRecursive(propPath, propSchema, propValue)...)
		}
	}

	// In strict mode, check for unexpected properties
	if v.config.StrictMode {
		for propName := range dataMap {
			if _, expected := schema.Properties[propName]; !expected {
				violations = append(violations, ContractViolation{
					Type:        ViolationTypeSchema,
					Field:       v.joinPath(path, propName),
					Expected:    "property defined in schema",
					Actual:      "unexpected property",
					Description: fmt.Sprintf("unexpected property %s", propName),
					Severity:    SeverityLow,
				})
			}
		}
	}

	return violations
}

// validateArraySchema validates array schema constraints
func (v *DefaultValidator) validateArraySchema(path string, schema *JSONSchema, data interface{}) []ContractViolation {
	var violations []ContractViolation

	dataArray, ok := data.([]interface{})
	if !ok {
		return violations // Type validation handled elsewhere
	}

	// Validate items schema
	if schema.Items != nil {
		for i, item := range dataArray {
			itemPath := fmt.Sprintf("%s[%d]", path, i)
			violations = append(violations, v.validateSchemaRecursive(itemPath, schema.Items, item)...)
		}
	}

	return violations
}

// validateStringSchema validates string schema constraints
func (v *DefaultValidator) validateStringSchema(path string, schema *JSONSchema, data interface{}) []ContractViolation {
	var violations []ContractViolation

	str, ok := data.(string)
	if !ok {
		return violations // Type validation handled elsewhere
	}

	// Validate pattern
	if schema.Pattern != "" {
		matched, err := regexp.MatchString(schema.Pattern, str)
		if err == nil && !matched {
			violations = append(violations, ContractViolation{
				Type:        ViolationTypeSchema,
				Field:       path,
				Expected:    fmt.Sprintf("pattern: %s", schema.Pattern),
				Actual:      str,
				Description: fmt.Sprintf("string does not match pattern at path %s", path),
				Severity:    SeverityMedium,
			})
		}
	}

	// Validate length constraints
	if schema.MinLength != nil && len(str) < *schema.MinLength {
		violations = append(violations, ContractViolation{
			Type:        ViolationTypeSchema,
			Field:       path,
			Expected:    fmt.Sprintf("min length: %d", *schema.MinLength),
			Actual:      fmt.Sprintf("length: %d", len(str)),
			Description: fmt.Sprintf("string too short at path %s", path),
			Severity:    SeverityMedium,
		})
	}

	if schema.MaxLength != nil && len(str) > *schema.MaxLength {
		violations = append(violations, ContractViolation{
			Type:        ViolationTypeSchema,
			Field:       path,
			Expected:    fmt.Sprintf("max length: %d", *schema.MaxLength),
			Actual:      fmt.Sprintf("length: %d", len(str)),
			Description: fmt.Sprintf("string too long at path %s", path),
			Severity:    SeverityMedium,
		})
	}

	return violations
}

// validateNumberSchema validates number schema constraints
func (v *DefaultValidator) validateNumberSchema(path string, schema *JSONSchema, data interface{}) []ContractViolation {
	var violations []ContractViolation

	var num float64
	var ok bool

	switch v := data.(type) {
	case float64:
		num = v
		ok = true
	case int:
		num = float64(v)
		ok = true
	case int64:
		num = float64(v)
		ok = true
	}

	if !ok {
		return violations // Type validation handled elsewhere
	}

	// Validate minimum
	if schema.Minimum != nil && num < *schema.Minimum {
		violations = append(violations, ContractViolation{
			Type:        ViolationTypeSchema,
			Field:       path,
			Expected:    fmt.Sprintf("minimum: %f", *schema.Minimum),
			Actual:      fmt.Sprintf("value: %f", num),
			Description: fmt.Sprintf("number below minimum at path %s", path),
			Severity:    SeverityMedium,
		})
	}

	// Validate maximum
	if schema.Maximum != nil && num > *schema.Maximum {
		violations = append(violations, ContractViolation{
			Type:        ViolationTypeSchema,
			Field:       path,
			Expected:    fmt.Sprintf("maximum: %f", *schema.Maximum),
			Actual:      fmt.Sprintf("value: %f", num),
			Description: fmt.Sprintf("number above maximum at path %s", path),
			Severity:    SeverityMedium,
		})
	}

	return violations
}

// validateStatusCode validates the HTTP status code
func (v *DefaultValidator) validateStatusCode(endpoint *EndpointContract, recording *ContractRecording) []ContractViolation {
	var violations []ContractViolation

	// Check if the status code is defined in the contract
	if _, exists := endpoint.ResponseSchemas[recording.StatusCode]; !exists {
		violations = append(violations, ContractViolation{
			Type:        ViolationTypeStatusCode,
			Expected:    fmt.Sprintf("one of %v", v.getExpectedStatusCodes(endpoint)),
			Actual:      strconv.Itoa(recording.StatusCode),
			Description: "unexpected status code",
			Severity:    SeverityHigh,
		})
	}

	return violations
}

// validateResponseHeaders validates response headers
func (v *DefaultValidator) validateResponseHeaders(endpoint *EndpointContract, recording *ContractRecording) []ContractViolation {
	var violations []ContractViolation

	// For now, we just check that expected headers are present
	// More sophisticated header validation could be added later
	for expectedHeader, expectedValue := range endpoint.RequestHeaders {
		if actualValue, exists := recording.ResponseHeaders[expectedHeader]; !exists {
			violations = append(violations, ContractViolation{
				Type:        ViolationTypeHeaders,
				Field:       expectedHeader,
				Expected:    "header present",
				Actual:      "header missing",
				Description: fmt.Sprintf("expected header %s is missing", expectedHeader),
				Severity:    SeverityMedium,
			})
		} else if expectedValue != "" && expectedValue != actualValue {
			violations = append(violations, ContractViolation{
				Type:        ViolationTypeHeaders,
				Field:       expectedHeader,
				Expected:    expectedValue,
				Actual:      actualValue,
				Description: fmt.Sprintf("header %s value mismatch", expectedHeader),
				Severity:    SeverityMedium,
			})
		}
	}

	return violations
}

// validateResponseSchema validates the response body against the schema
func (v *DefaultValidator) validateResponseSchema(endpoint *EndpointContract, recording *ContractRecording) []ContractViolation {
	schema := endpoint.ResponseSchemas[recording.StatusCode]
	if schema == nil {
		return []ContractViolation{} // No schema defined for this status code
	}

	return v.ValidateSchema(schema, recording.ResponseBody)
}

// validateTimeout validates response time against timeout constraints
func (v *DefaultValidator) validateTimeout(endpoint *EndpointContract, recording *ContractRecording) []ContractViolation {
	var violations []ContractViolation

	if endpoint.Timeout > 0 {
		maxDuration := endpoint.Timeout + v.config.TimeoutTolerance
		if recording.Duration > maxDuration {
			violations = append(violations, ContractViolation{
				Type:        ViolationTypeTimeout,
				Expected:    endpoint.Timeout.String(),
				Actual:      recording.Duration.String(),
				Description: "response time exceeded timeout threshold",
				Severity:    SeverityMedium,
			})
		}
	}

	return violations
}

// Helper methods

func (v *DefaultValidator) findMatchingEndpoint(contract *APIContract, recording *ContractRecording) *EndpointContract {
	for _, endpoint := range contract.Endpoints {
		if v.endpointMatches(&endpoint, recording) {
			return &endpoint
		}
	}
	return nil
}

func (v *DefaultValidator) endpointMatches(endpoint *EndpointContract, recording *ContractRecording) bool {
	// Simple matching logic - could be enhanced with path parameter support
	methodMatch := strings.ToUpper(endpoint.Method) == strings.ToUpper(recording.Method)
	pathMatch := v.pathMatches(endpoint.Path, recording.URL)
	
	return methodMatch && pathMatch
}

func (v *DefaultValidator) pathMatches(contractPath, recordingURL string) bool {
	// Extract path from URL
	// This is a simplified implementation
	// In production, you'd want more sophisticated URL parsing
	return strings.HasSuffix(recordingURL, contractPath)
}

func (v *DefaultValidator) getJSONType(data interface{}) string {
	if data == nil {
		return "null"
	}

	switch data.(type) {
	case bool:
		return "boolean"
	case float64, int, int64:
		return "number"
	case string:
		return "string"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return "unknown"
	}
}

func (v *DefaultValidator) joinPath(base, field string) string {
	if base == "" {
		return field
	}
	return base + "." + field
}

func (v *DefaultValidator) getExpectedStatusCodes(endpoint *EndpointContract) []int {
	var codes []int
	for code := range endpoint.ResponseSchemas {
		codes = append(codes, code)
	}
	return codes
}

// DefaultValidationConfig returns a sensible default configuration
func DefaultValidationConfig() ValidationConfig {
	return ValidationConfig{
		StrictMode:       false,
		TimeoutTolerance: 5 * time.Second,
		MaxRetries:       3,
		IgnoredFields:    []string{},
	}
}