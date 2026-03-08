package validation

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/agentbrain/agentbrain/internal/schema"
)

// ValidationRule defines the interface for validation rules.
type ValidationRule interface {
	Name() string
	Validate(field schema.Field, value any) ([]ValidationError, []ValidationWarning)
}

// TypeConsistencyRule validates that values match their declared schema types.
type TypeConsistencyRule struct{}

// NewTypeConsistencyRule creates a new type consistency validation rule.
func NewTypeConsistencyRule() *TypeConsistencyRule {
	return &TypeConsistencyRule{}
}

func (r *TypeConsistencyRule) Name() string {
	return "type_consistency"
}

func (r *TypeConsistencyRule) Validate(field schema.Field, value any) ([]ValidationError, []ValidationWarning) {
	var errors []ValidationError
	var warnings []ValidationWarning
	
	if value == nil {
		return errors, warnings // Null values are handled by null constraint rule
	}
	
	expectedType := field.Type
	actualType := getValueType(value)
	
	// Check if the actual type matches the expected type
	if !r.isTypeCompatible(expectedType, actualType, value) {
		errors = append(errors, ValidationError{
			Field:        field.Name,
			Type:         "type_mismatch",
			Message:      fmt.Sprintf("Expected %s but got %s", expectedType, actualType),
			Value:        value,
			ExpectedType: string(expectedType),
			Severity:     "error",
		})
	}
	
	return errors, warnings
}

func (r *TypeConsistencyRule) isTypeCompatible(expected schema.FieldType, actual string, value any) bool {
	switch expected {
	case schema.FieldTypeString:
		return actual == "string"
	case schema.FieldTypeInt:
		return actual == "integer" || actual == "float" && r.isIntegerValue(value)
	case schema.FieldTypeLong:
		return actual == "integer" || (actual == "float" && r.isIntegerValue(value))
	case schema.FieldTypeDouble:
		return actual == "float" || actual == "integer"
	case schema.FieldTypeBoolean:
		return actual == "boolean"
	case schema.FieldTypeDate, schema.FieldTypeDatetime:
		return actual == "datetime" || (actual == "string" && r.isDateString(value))
	case schema.FieldTypeBinary:
		return actual == "string" // Binary is typically encoded as string
	default:
		return false
	}
}

func (r *TypeConsistencyRule) isIntegerValue(value any) bool {
	switch v := value.(type) {
	case float32:
		return v == float32(int32(v))
	case float64:
		return v == float64(int64(v))
	default:
		return false
	}
}

func (r *TypeConsistencyRule) isDateString(value any) bool {
	if str, ok := value.(string); ok {
		// Try common date formats
		formats := []string{
			time.RFC3339,
			"2006-01-02",
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
		}
		
		for _, format := range formats {
			if _, err := time.Parse(format, str); err == nil {
				return true
			}
		}
	}
	return false
}

// NullConstraintRule validates that non-nullable fields don't contain null values.
type NullConstraintRule struct{}

// NewNullConstraintRule creates a new null constraint validation rule.
func NewNullConstraintRule() *NullConstraintRule {
	return &NullConstraintRule{}
}

func (r *NullConstraintRule) Name() string {
	return "null_constraint"
}

func (r *NullConstraintRule) Validate(field schema.Field, value any) ([]ValidationError, []ValidationWarning) {
	var errors []ValidationError
	var warnings []ValidationWarning
	
	if !field.Nullable && value == nil {
		errors = append(errors, ValidationError{
			Field:      field.Name,
			Type:       "null_constraint",
			Message:    fmt.Sprintf("Field %s is marked as non-nullable but contains null value", field.Name),
			Value:      value,
			Constraint: "NOT NULL",
			Severity:   "error",
		})
	}
	
	return errors, warnings
}

// FormatValidationRule validates field formats (email, phone, etc.).
type FormatValidationRule struct {
	patterns map[string]*regexp.Regexp
}

// NewFormatValidationRule creates a new format validation rule.
func NewFormatValidationRule() *FormatValidationRule {
	return &FormatValidationRule{
		patterns: map[string]*regexp.Regexp{
			"email":       regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`),
			"phone":       regexp.MustCompile(`^\+?[1-9]\d{1,14}$`),
			"url":         regexp.MustCompile(`^https?://[^\s/$.?#].[^\s]*$`),
			"uuid":        regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`),
			"postal_code": regexp.MustCompile(`^\d{5}(-\d{4})?$`),
		},
	}
}

func (r *FormatValidationRule) Name() string {
	return "format_validation"
}

func (r *FormatValidationRule) Validate(field schema.Field, value any) ([]ValidationError, []ValidationWarning) {
	var errors []ValidationError
	var warnings []ValidationWarning
	
	if value == nil {
		return errors, warnings
	}
	
	strValue, ok := value.(string)
	if !ok {
		return errors, warnings // Only validate string fields
	}
	
	// Check if field name suggests a specific format
	fieldNameLower := strings.ToLower(field.Name)
	
	if strings.Contains(fieldNameLower, "email") {
		if pattern, exists := r.patterns["email"]; exists && !pattern.MatchString(strValue) {
			warnings = append(warnings, ValidationWarning{
				Field:   field.Name,
				Type:    "format_invalid",
				Message: fmt.Sprintf("Field %s appears to be an email but doesn't match email format", field.Name),
				Value:   value,
			})
		}
	} else if strings.Contains(fieldNameLower, "phone") {
		if pattern, exists := r.patterns["phone"]; exists && !pattern.MatchString(strValue) {
			warnings = append(warnings, ValidationWarning{
				Field:   field.Name,
				Type:    "format_invalid",
				Message: fmt.Sprintf("Field %s appears to be a phone number but doesn't match phone format", field.Name),
				Value:   value,
			})
		}
	} else if strings.Contains(fieldNameLower, "url") || strings.Contains(fieldNameLower, "website") {
		if pattern, exists := r.patterns["url"]; exists && !pattern.MatchString(strValue) {
			warnings = append(warnings, ValidationWarning{
				Field:   field.Name,
				Type:    "format_invalid",
				Message: fmt.Sprintf("Field %s appears to be a URL but doesn't match URL format", field.Name),
				Value:   value,
			})
		}
	} else if strings.Contains(fieldNameLower, "id") && len(strValue) == 36 {
		// Check if it looks like a UUID
		if pattern, exists := r.patterns["uuid"]; exists && !pattern.MatchString(strValue) {
			warnings = append(warnings, ValidationWarning{
				Field:   field.Name,
				Type:    "format_invalid",
				Message: fmt.Sprintf("Field %s appears to be a UUID but doesn't match UUID format", field.Name),
				Value:   value,
			})
		}
	}
	
	return errors, warnings
}

// RangeValidationRule validates numeric values are within expected ranges.
type RangeValidationRule struct {
	ranges map[string]NumericRange
}

// NumericRange defines min/max bounds for a field.
type NumericRange struct {
	Min        *float64
	Max        *float64
	FieldNames []string // Field names this range applies to
}

// NewRangeValidationRule creates a new range validation rule.
func NewRangeValidationRule(ranges map[string]NumericRange) *RangeValidationRule {
	if ranges == nil {
		ranges = make(map[string]NumericRange)
	}
	
	// Add some common sense ranges
	ranges["percentage"] = NumericRange{
		Min:        &[]float64{0}[0],
		Max:        &[]float64{100}[0],
		FieldNames: []string{"percent", "percentage", "rate"},
	}
	ranges["age"] = NumericRange{
		Min:        &[]float64{0}[0],
		Max:        &[]float64{150}[0],
		FieldNames: []string{"age"},
	}
	ranges["revenue"] = NumericRange{
		Min:        &[]float64{0}[0],
		FieldNames: []string{"revenue", "amount", "price", "cost"},
	}
	
	return &RangeValidationRule{ranges: ranges}
}

func (r *RangeValidationRule) Name() string {
	return "range_validation"
}

func (r *RangeValidationRule) Validate(field schema.Field, value any) ([]ValidationError, []ValidationWarning) {
	var errors []ValidationError
	var warnings []ValidationWarning
	
	if value == nil {
		return errors, warnings
	}
	
	numValue, ok := toFloat64(value)
	if !ok {
		return errors, warnings // Only validate numeric fields
	}
	
	fieldNameLower := strings.ToLower(field.Name)
	
	// Check each range to see if it applies to this field
	for rangeName, rangeConfig := range r.ranges {
		applies := false
		
		// Check if field name matches any pattern
		for _, pattern := range rangeConfig.FieldNames {
			if strings.Contains(fieldNameLower, pattern) {
				applies = true
				break
			}
		}
		
		if !applies {
			continue
		}
		
		// Validate range
		if rangeConfig.Min != nil && numValue < *rangeConfig.Min {
			warnings = append(warnings, ValidationWarning{
				Field:     field.Name,
				Type:      "range_violation",
				Message:   fmt.Sprintf("Value %v is below expected minimum %v for %s", numValue, *rangeConfig.Min, rangeName),
				Value:     value,
				Threshold: fmt.Sprintf("min: %v", *rangeConfig.Min),
			})
		}
		
		if rangeConfig.Max != nil && numValue > *rangeConfig.Max {
			warnings = append(warnings, ValidationWarning{
				Field:     field.Name,
				Type:      "range_violation",
				Message:   fmt.Sprintf("Value %v is above expected maximum %v for %s", numValue, *rangeConfig.Max, rangeName),
				Value:     value,
				Threshold: fmt.Sprintf("max: %v", *rangeConfig.Max),
			})
		}
	}
	
	return errors, warnings
}

// DataQualityRule validates data quality patterns.
type DataQualityRule struct{}

// NewDataQualityRule creates a new data quality validation rule.
func NewDataQualityRule() *DataQualityRule {
	return &DataQualityRule{}
}

func (r *DataQualityRule) Name() string {
	return "data_quality"
}

func (r *DataQualityRule) Validate(field schema.Field, value any) ([]ValidationError, []ValidationWarning) {
	var errors []ValidationError
	var warnings []ValidationWarning
	
	if value == nil {
		return errors, warnings
	}
	
	strValue, ok := value.(string)
	if !ok {
		return errors, warnings
	}
	
	// Check for common data quality issues
	
	// Empty strings that should probably be null
	if strings.TrimSpace(strValue) == "" {
		warnings = append(warnings, ValidationWarning{
			Field:   field.Name,
			Type:    "data_quality",
			Message: fmt.Sprintf("Field %s contains empty string, consider null value", field.Name),
			Value:   value,
		})
	}
	
	// Suspicious placeholder values
	suspiciousValues := []string{"n/a", "na", "null", "none", "unknown", "test", "example", "dummy"}
	lowerValue := strings.ToLower(strings.TrimSpace(strValue))
	
	for _, suspicious := range suspiciousValues {
		if lowerValue == suspicious {
			warnings = append(warnings, ValidationWarning{
				Field:   field.Name,
				Type:    "data_quality",
				Message: fmt.Sprintf("Field %s contains suspicious placeholder value: %s", field.Name, strValue),
				Value:   value,
			})
			break
		}
	}
	
	// Check for excessive whitespace
	if len(strValue) != len(strings.TrimSpace(strValue)) && len(strings.TrimSpace(strValue)) > 0 {
		warnings = append(warnings, ValidationWarning{
			Field:   field.Name,
			Type:    "data_quality",
			Message: fmt.Sprintf("Field %s contains leading/trailing whitespace", field.Name),
			Value:   value,
		})
	}
	
	// Check for unusual length for common field types
	fieldNameLower := strings.ToLower(field.Name)
	if strings.Contains(fieldNameLower, "name") && len(strValue) > 100 {
		warnings = append(warnings, ValidationWarning{
			Field:   field.Name,
			Type:    "data_quality",
			Message: fmt.Sprintf("Field %s has unusually long value (%d characters) for a name field", field.Name, len(strValue)),
			Value:   fmt.Sprintf("%.50s...", strValue), // Truncate for display
		})
	}
	
	return errors, warnings
}