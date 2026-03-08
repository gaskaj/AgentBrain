package validation

import (
	"fmt"
	"time"

	"github.com/agentbrain/agentbrain/internal/connector"
	"github.com/agentbrain/agentbrain/internal/schema"
)

// Validator defines the interface for data validation.
type Validator interface {
	ValidateRecord(record map[string]any, schema *schema.Schema) ValidationResult
	ValidateBatch(batch *connector.RecordBatch, schema *schema.Schema) BatchValidationResult
}

// ValidationResult contains the results of validating a single record.
type ValidationResult struct {
	Valid     bool                `json:"valid"`
	Errors    []ValidationError   `json:"errors,omitempty"`
	Warnings  []ValidationWarning `json:"warnings,omitempty"`
	Metrics   ValidationMetrics   `json:"metrics"`
	RecordID  string              `json:"record_id,omitempty"`
	Timestamp time.Time           `json:"timestamp"`
}

// BatchValidationResult contains the results of validating a batch of records.
type BatchValidationResult struct {
	Valid              bool                  `json:"valid"`
	TotalRecords       int                   `json:"total_records"`
	ValidRecords       int                   `json:"valid_records"`
	ErrorRecords       int                   `json:"error_records"`
	WarningRecords     int                   `json:"warning_records"`
	Errors             []ValidationError     `json:"errors,omitempty"`
	Warnings           []ValidationWarning   `json:"warnings,omitempty"`
	FieldMetrics       map[string]FieldStats `json:"field_metrics"`
	AggregatedMetrics  ValidationMetrics     `json:"aggregated_metrics"`
	ProcessingTime     time.Duration         `json:"processing_time"`
	Timestamp          time.Time             `json:"timestamp"`
}

// ValidationError represents a data validation error.
type ValidationError struct {
	Field        string      `json:"field"`
	Type         string      `json:"type"` // "type_mismatch", "null_constraint", "format_invalid", "range_violation"
	Message      string      `json:"message"`
	Value        any         `json:"value,omitempty"`
	ExpectedType string      `json:"expected_type,omitempty"`
	Constraint   string      `json:"constraint,omitempty"`
	RecordID     string      `json:"record_id,omitempty"`
	Severity     string      `json:"severity"` // "error", "warning"
}

// ValidationWarning represents a data validation warning.
type ValidationWarning struct {
	Field     string `json:"field"`
	Type      string `json:"type"` // "drift_detected", "unusual_value", "data_quality"
	Message   string `json:"message"`
	Value     any    `json:"value,omitempty"`
	RecordID  string `json:"record_id,omitempty"`
	Threshold string `json:"threshold,omitempty"`
}

// ValidationMetrics contains statistics from validation.
type ValidationMetrics struct {
	ErrorRate            float64            `json:"error_rate"`
	WarningRate          float64            `json:"warning_rate"`
	NullRate             float64            `json:"null_rate"`
	TypeMismatchRate     float64            `json:"type_mismatch_rate"`
	FieldPopulationRates map[string]float64 `json:"field_population_rates"`
	ValidatedFields      int                `json:"validated_fields"`
	TotalFields          int                `json:"total_fields"`
}

// FieldStats contains statistics for a specific field.
type FieldStats struct {
	FieldName        string             `json:"field_name"`
	PopulationRate   float64            `json:"population_rate"`
	NullCount        int                `json:"null_count"`
	NonNullCount     int                `json:"non_null_count"`
	TypeDistribution map[string]int     `json:"type_distribution"`
	UniqueValues     int                `json:"unique_values"`
	SampleValues     []any              `json:"sample_values,omitempty"`
	NumericStats     *NumericFieldStats `json:"numeric_stats,omitempty"`
}

// NumericFieldStats contains statistics for numeric fields.
type NumericFieldStats struct {
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"std_dev"`
}

// DefaultValidator implements the Validator interface.
type DefaultValidator struct {
	rules       []ValidationRule
	driftDetector *DriftDetector
	config      ValidatorConfig
}

// ValidatorConfig contains configuration for the validator.
type ValidatorConfig struct {
	ErrorThreshold   float64            `json:"error_threshold"`   // Max error rate before alerting
	StrictMode       bool               `json:"strict_mode"`       // Fail on validation errors
	CustomRules      map[string][]Rule  `json:"custom_rules"`      // Object-specific rules
	DriftThreshold   float64            `json:"drift_threshold"`   // Max drift before alerting
	SamplingRate     float64            `json:"sampling_rate"`     // Rate for detailed validation (0-1)
	MaxSampleValues  int                `json:"max_sample_values"` // Max sample values to store
}

// Rule represents a custom validation rule.
type Rule struct {
	Field     string                 `json:"field"`
	Type      string                 `json:"type"` // "range", "format", "enum", "regex"
	Min       *float64               `json:"min,omitempty"`
	Max       *float64               `json:"max,omitempty"`
	Pattern   string                 `json:"pattern,omitempty"`
	Values    []string               `json:"values,omitempty"`
	Required  bool                   `json:"required"`
	CustomFn  func(any) error        `json:"-"` // Custom validation function
}

// NewDefaultValidator creates a new validator with default rules.
func NewDefaultValidator(config ValidatorConfig) *DefaultValidator {
	validator := &DefaultValidator{
		config: config,
		rules:  make([]ValidationRule, 0),
	}
	
	// Initialize drift detector if threshold is set
	if config.DriftThreshold > 0 {
		validator.driftDetector = NewDriftDetector(DriftDetectorConfig{
			Threshold:       config.DriftThreshold,
			WindowSize:      100, // Keep last 100 validation results for drift detection
			MinSampleSize:   10,  // Minimum samples before drift detection
		})
	}
	
	// Add built-in validation rules
	validator.rules = append(validator.rules,
		NewTypeConsistencyRule(),
		NewNullConstraintRule(),
		NewFormatValidationRule(),
	)
	
	return validator
}

// ValidateRecord validates a single record against the schema.
func (v *DefaultValidator) ValidateRecord(record map[string]any, schema *schema.Schema) ValidationResult {
	start := time.Now()
	result := ValidationResult{
		Valid:     true,
		Errors:    make([]ValidationError, 0),
		Warnings:  make([]ValidationWarning, 0),
		Timestamp: start,
	}
	
	if schema == nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:    "",
			Type:     "schema_missing",
			Message:  "Schema is required for validation",
			Severity: "error",
		})
		return result
	}
	
	// Track field statistics
	fieldStats := make(map[string]FieldStats)
	populationRates := make(map[string]float64)
	
	// Validate each field in the schema
	var errorCount, warningCount int
	for _, field := range schema.Fields {
		value, exists := record[field.Name]
		
		// Initialize field stats
		stats := FieldStats{
			FieldName:        field.Name,
			TypeDistribution: make(map[string]int),
			SampleValues:     make([]any, 0),
		}
		
		if !exists || value == nil {
			stats.NullCount = 1
			populationRates[field.Name] = 0.0
			
			// Check null constraint
			if !field.Nullable {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Field:    field.Name,
					Type:     "null_constraint",
					Message:  fmt.Sprintf("Field %s cannot be null", field.Name),
					Value:    value,
					Severity: "error",
				})
				errorCount++
			}
		} else {
			stats.NonNullCount = 1
			stats.UniqueValues = 1
			populationRates[field.Name] = 1.0
			
			// Add to sample values if under limit
			if len(stats.SampleValues) < v.config.MaxSampleValues {
				stats.SampleValues = append(stats.SampleValues, value)
			}
			
			// Validate each rule
			for _, rule := range v.rules {
				if ruleErrors, ruleWarnings := rule.Validate(field, value); len(ruleErrors) > 0 || len(ruleWarnings) > 0 {
					result.Errors = append(result.Errors, ruleErrors...)
					result.Warnings = append(result.Warnings, ruleWarnings...)
					
					if len(ruleErrors) > 0 {
						result.Valid = false
						errorCount += len(ruleErrors)
					}
					if len(ruleWarnings) > 0 {
						warningCount += len(ruleWarnings)
					}
				}
			}
			
			// Apply custom rules if configured
			if customRules, exists := v.config.CustomRules[schema.ObjectName]; exists {
				for _, customRule := range customRules {
					if customRule.Field == field.Name {
						if err := v.validateCustomRule(field, value, customRule); err != nil {
							result.Valid = false
							result.Errors = append(result.Errors, ValidationError{
								Field:    field.Name,
								Type:     "custom_rule",
								Message:  err.Error(),
								Value:    value,
								Severity: "error",
							})
							errorCount++
						}
					}
				}
			}
			
			// Track type distribution
			valueType := getValueType(value)
			stats.TypeDistribution[valueType]++
		}
		
		fieldStats[field.Name] = stats
	}
	
	// Calculate metrics
	totalFields := len(schema.Fields)
	result.Metrics = ValidationMetrics{
		ErrorRate:            float64(errorCount) / float64(totalFields),
		WarningRate:          float64(warningCount) / float64(totalFields),
		FieldPopulationRates: populationRates,
		ValidatedFields:      totalFields,
		TotalFields:          totalFields,
	}
	
	// Calculate null rate
	var nullCount int
	for _, rate := range populationRates {
		if rate == 0.0 {
			nullCount++
		}
	}
	result.Metrics.NullRate = float64(nullCount) / float64(totalFields)
	
	return result
}

// ValidateBatch validates a batch of records against the schema.
func (v *DefaultValidator) ValidateBatch(batch *connector.RecordBatch, schema *schema.Schema) BatchValidationResult {
	start := time.Now()
	
	result := BatchValidationResult{
		Valid:             true,
		TotalRecords:      len(batch.Records),
		FieldMetrics:      make(map[string]FieldStats),
		Timestamp:         start,
	}
	
	if len(batch.Records) == 0 {
		result.ProcessingTime = time.Since(start)
		return result
	}
	
	// Aggregate field statistics across all records
	aggregatedFieldStats := make(map[string]FieldStats)
	for _, field := range schema.Fields {
		aggregatedFieldStats[field.Name] = FieldStats{
			FieldName:        field.Name,
			TypeDistribution: make(map[string]int),
			SampleValues:     make([]any, 0),
		}
	}
	
	var totalErrors, totalWarnings int
	var allErrors []ValidationError
	var allWarnings []ValidationWarning
	
	// Determine sampling - validate all records if sampling rate is 1.0 or batch is small
	shouldSampleAll := v.config.SamplingRate >= 1.0 || len(batch.Records) <= 100
	
	for i, record := range batch.Records {
		// Apply sampling for large batches
		if !shouldSampleAll && (float64(i) / float64(len(batch.Records))) > v.config.SamplingRate {
			continue
		}
		
		recordResult := v.ValidateRecord(record, schema)
		
		if !recordResult.Valid {
			result.Valid = false
			result.ErrorRecords++
			totalErrors += len(recordResult.Errors)
			
			// Add record context to errors
			for _, err := range recordResult.Errors {
				err.RecordID = fmt.Sprintf("record_%d", i)
				allErrors = append(allErrors, err)
			}
		} else {
			result.ValidRecords++
		}
		
		if len(recordResult.Warnings) > 0 {
			result.WarningRecords++
			totalWarnings += len(recordResult.Warnings)
			
			// Add record context to warnings
			for _, warn := range recordResult.Warnings {
				warn.RecordID = fmt.Sprintf("record_%d", i)
				allWarnings = append(allWarnings, warn)
			}
		}
		
		// Aggregate field statistics
		for fieldName, rate := range recordResult.Metrics.FieldPopulationRates {
			fieldStats := aggregatedFieldStats[fieldName]
			if rate > 0 {
				fieldStats.NonNullCount++
			} else {
				fieldStats.NullCount++
			}
			aggregatedFieldStats[fieldName] = fieldStats
		}
	}
	
	// Calculate final field metrics
	for fieldName, stats := range aggregatedFieldStats {
		total := stats.NullCount + stats.NonNullCount
		if total > 0 {
			stats.PopulationRate = float64(stats.NonNullCount) / float64(total)
		}
		result.FieldMetrics[fieldName] = stats
	}
	
	// Set aggregated results
	result.Errors = allErrors
	result.Warnings = allWarnings
	
	// Calculate aggregated metrics
	validatedRecords := result.ValidRecords + result.ErrorRecords
	if validatedRecords > 0 {
		result.AggregatedMetrics = ValidationMetrics{
			ErrorRate:       float64(result.ErrorRecords) / float64(validatedRecords),
			WarningRate:     float64(result.WarningRecords) / float64(validatedRecords),
			ValidatedFields: len(schema.Fields),
			TotalFields:     len(schema.Fields),
		}
		
		// Calculate population rates
		populationRates := make(map[string]float64)
		for fieldName, stats := range result.FieldMetrics {
			populationRates[fieldName] = stats.PopulationRate
		}
		result.AggregatedMetrics.FieldPopulationRates = populationRates
		
		// Calculate null rate
		var totalNullRate float64
		for _, rate := range populationRates {
			totalNullRate += (1.0 - rate)
		}
		result.AggregatedMetrics.NullRate = totalNullRate / float64(len(populationRates))
	}
	
	result.ProcessingTime = time.Since(start)
	
	// Update drift detector if configured
	if v.driftDetector != nil {
		v.driftDetector.UpdateStats(batch.Object, result.FieldMetrics)
	}
	
	return result
}

// HasErrors returns true if the batch validation has errors.
func (r *BatchValidationResult) HasErrors() bool {
	return len(r.Errors) > 0 || r.ErrorRecords > 0
}

// HasWarnings returns true if the batch validation has warnings.
func (r *BatchValidationResult) HasWarnings() bool {
	return len(r.Warnings) > 0 || r.WarningRecords > 0
}

// validateCustomRule validates a value against a custom rule.
func (v *DefaultValidator) validateCustomRule(field schema.Field, value any, rule Rule) error {
	// If custom function is provided, use it
	if rule.CustomFn != nil {
		return rule.CustomFn(value)
	}
	
	switch rule.Type {
	case "range":
		if numVal, ok := toFloat64(value); ok {
			if rule.Min != nil && numVal < *rule.Min {
				return fmt.Errorf("value %v is below minimum %v", numVal, *rule.Min)
			}
			if rule.Max != nil && numVal > *rule.Max {
				return fmt.Errorf("value %v is above maximum %v", numVal, *rule.Max)
			}
		} else {
			return fmt.Errorf("expected numeric value for range validation, got %T", value)
		}
	case "enum":
		if strVal, ok := value.(string); ok {
			for _, allowedVal := range rule.Values {
				if strVal == allowedVal {
					return nil
				}
			}
			return fmt.Errorf("value %s is not in allowed values %v", strVal, rule.Values)
		} else {
			return fmt.Errorf("expected string value for enum validation, got %T", value)
		}
	}
	
	return nil
}

// Helper function to get the type of a value as a string
func getValueType(value any) string {
	if value == nil {
		return "null"
	}
	
	switch value.(type) {
	case string:
		return "string"
	case int, int8, int16, int32, int64:
		return "integer"
	case float32, float64:
		return "float"
	case bool:
		return "boolean"
	case time.Time:
		return "datetime"
	default:
		return "unknown"
	}
}

// Helper function to convert a value to float64
func toFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}