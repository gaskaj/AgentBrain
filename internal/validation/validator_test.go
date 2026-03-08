package validation

import (
	"testing"
	"time"

	"github.com/agentbrain/agentbrain/internal/connector"
	"github.com/agentbrain/agentbrain/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultValidator_ValidateRecord(t *testing.T) {
	config := ValidatorConfig{
		ErrorThreshold:  0.1,
		StrictMode:      false,
		MaxSampleValues: 5,
	}
	validator := NewDefaultValidator(config)
	
	// Create a test schema
	testSchema := schema.NewSchema("TestObject", []schema.Field{
		{Name: "id", Type: schema.FieldTypeString, Nullable: false},
		{Name: "name", Type: schema.FieldTypeString, Nullable: true},
		{Name: "age", Type: schema.FieldTypeInt, Nullable: true},
		{Name: "email", Type: schema.FieldTypeString, Nullable: true},
		{Name: "salary", Type: schema.FieldTypeDouble, Nullable: true},
		{Name: "is_active", Type: schema.FieldTypeBoolean, Nullable: false},
	}, 1)
	
	tests := []struct {
		name           string
		record         map[string]any
		expectValid    bool
		expectErrors   int
		expectWarnings int
		errorTypes     []string
		warningTypes   []string
	}{
		{
			name: "valid_record",
			record: map[string]any{
				"id":        "123",
				"name":      "John Doe",
				"age":       30,
				"email":     "john@example.com",
				"salary":    50000.0,
				"is_active": true,
			},
			expectValid:    true,
			expectErrors:   0,
			expectWarnings: 0,
		},
		{
			name: "null_constraint_violation",
			record: map[string]any{
				"id":        nil,
				"name":      "John Doe",
				"age":       30,
				"email":     "john@example.com",
				"salary":    50000.0,
				"is_active": true,
			},
			expectValid:  false,
			expectErrors: 1,
			errorTypes:   []string{"null_constraint"},
		},
		{
			name: "type_mismatch",
			record: map[string]any{
				"id":        "123",
				"name":      "John Doe",
				"age":       "thirty", // string instead of int
				"email":     "john@example.com",
				"salary":    50000.0,
				"is_active": true,
			},
			expectValid:  false,
			expectErrors: 1,
			errorTypes:   []string{"type_mismatch"},
		},
		{
			name: "format_warning_invalid_email",
			record: map[string]any{
				"id":        "123",
				"name":      "John Doe",
				"age":       30,
				"email":     "not-an-email",
				"salary":    50000.0,
				"is_active": true,
			},
			expectValid:    true,
			expectErrors:   0,
			expectWarnings: 1,
			warningTypes:   []string{"format_invalid"},
		},
		{
			name: "multiple_violations",
			record: map[string]any{
				"id":        nil,        // null constraint violation
				"name":      "John Doe",
				"age":       "thirty",   // type mismatch
				"email":     "not-email", // format warning
				"salary":    50000.0,
				"is_active": "yes", // type mismatch
			},
			expectValid:    false,
			expectErrors:   3, // id null, age type, is_active type
			expectWarnings: 1, // email format
		},
		{
			name: "nullable_fields_with_nulls",
			record: map[string]any{
				"id":        "123",
				"name":      nil,  // nullable, should be ok
				"age":       nil,  // nullable, should be ok
				"email":     nil,  // nullable, should be ok
				"salary":    nil,  // nullable, should be ok
				"is_active": true,
			},
			expectValid:    true,
			expectErrors:   0,
			expectWarnings: 0,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.ValidateRecord(tt.record, testSchema)
			
			assert.Equal(t, tt.expectValid, result.Valid, "Expected valid: %v, got: %v", tt.expectValid, result.Valid)
			assert.Equal(t, tt.expectErrors, len(result.Errors), "Expected %d errors, got %d", tt.expectErrors, len(result.Errors))
			assert.Equal(t, tt.expectWarnings, len(result.Warnings), "Expected %d warnings, got %d", tt.expectWarnings, len(result.Warnings))
			
			// Check error types
			for i, expectedType := range tt.errorTypes {
				if i < len(result.Errors) {
					assert.Equal(t, expectedType, result.Errors[i].Type, "Expected error type %s, got %s", expectedType, result.Errors[i].Type)
				}
			}
			
			// Check warning types
			for i, expectedType := range tt.warningTypes {
				if i < len(result.Warnings) {
					assert.Equal(t, expectedType, result.Warnings[i].Type, "Expected warning type %s, got %s", expectedType, result.Warnings[i].Type)
				}
			}
			
			// Validate metrics
			assert.NotZero(t, result.Timestamp)
			assert.Equal(t, len(testSchema.Fields), result.Metrics.ValidatedFields)
			assert.Equal(t, len(testSchema.Fields), result.Metrics.TotalFields)
			assert.LessOrEqual(t, result.Metrics.ErrorRate, 1.0)
			assert.GreaterOrEqual(t, result.Metrics.ErrorRate, 0.0)
		})
	}
}

func TestDefaultValidator_ValidateBatch(t *testing.T) {
	config := ValidatorConfig{
		ErrorThreshold:  0.2, // 20% error threshold
		SamplingRate:    1.0, // Validate all records
		MaxSampleValues: 3,
	}
	validator := NewDefaultValidator(config)
	
	testSchema := schema.NewSchema("TestObject", []schema.Field{
		{Name: "id", Type: schema.FieldTypeString, Nullable: false},
		{Name: "value", Type: schema.FieldTypeInt, Nullable: true},
	}, 1)
	
	// Create test batch with mixed valid/invalid records
	batch := &connector.RecordBatch{
		Object: "TestObject",
		Records: []map[string]any{
			{"id": "1", "value": 100},      // valid
			{"id": "2", "value": 200},      // valid
			{"id": nil, "value": 300},      // invalid - null id
			{"id": "4", "value": "text"},   // invalid - wrong type
			{"id": "5", "value": 500},      // valid
		},
	}
	
	result := validator.ValidateBatch(batch, testSchema)
	
	assert.False(t, result.Valid, "Batch should be invalid due to errors")
	assert.Equal(t, 5, result.TotalRecords)
	assert.Equal(t, 3, result.ValidRecords)
	assert.Equal(t, 2, result.ErrorRecords)
	assert.Equal(t, 0, result.WarningRecords)
	assert.True(t, len(result.Errors) > 0)
	assert.True(t, result.HasErrors())
	assert.False(t, result.HasWarnings())
	
	// Check aggregated metrics
	assert.InDelta(t, 0.4, result.AggregatedMetrics.ErrorRate, 0.01) // 2/5 = 0.4
	assert.Contains(t, result.FieldMetrics, "id")
	assert.Contains(t, result.FieldMetrics, "value")
	
	// Check field metrics
	idStats := result.FieldMetrics["id"]
	assert.Equal(t, 4, idStats.NonNullCount) // 4 non-null id values
	assert.Equal(t, 1, idStats.NullCount)    // 1 null id value
	assert.InDelta(t, 0.8, idStats.PopulationRate, 0.01) // 4/5 = 0.8
}

func TestValidationRules(t *testing.T) {
	t.Run("TypeConsistencyRule", func(t *testing.T) {
		rule := NewTypeConsistencyRule()
		
		tests := []struct {
			name         string
			field        schema.Field
			value        any
			expectErrors int
		}{
			{
				name:         "string_field_string_value",
				field:        schema.Field{Name: "name", Type: schema.FieldTypeString},
				value:        "John",
				expectErrors: 0,
			},
			{
				name:         "string_field_int_value",
				field:        schema.Field{Name: "name", Type: schema.FieldTypeString},
				value:        123,
				expectErrors: 1,
			},
			{
				name:         "int_field_int_value",
				field:        schema.Field{Name: "age", Type: schema.FieldTypeInt},
				value:        25,
				expectErrors: 0,
			},
			{
				name:         "int_field_float_value_integer",
				field:        schema.Field{Name: "age", Type: schema.FieldTypeInt},
				value:        25.0, // Integer as float should be ok
				expectErrors: 0,
			},
			{
				name:         "int_field_float_value_decimal",
				field:        schema.Field{Name: "age", Type: schema.FieldTypeInt},
				value:        25.5, // Decimal should fail
				expectErrors: 1,
			},
		}
		
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				errors, warnings := rule.Validate(tt.field, tt.value)
				assert.Equal(t, tt.expectErrors, len(errors))
				assert.Equal(t, 0, len(warnings)) // Type rule doesn't generate warnings
			})
		}
	})
	
	t.Run("NullConstraintRule", func(t *testing.T) {
		rule := NewNullConstraintRule()
		
		tests := []struct {
			name         string
			field        schema.Field
			value        any
			expectErrors int
		}{
			{
				name:         "nullable_field_null_value",
				field:        schema.Field{Name: "description", Type: schema.FieldTypeString, Nullable: true},
				value:        nil,
				expectErrors: 0,
			},
			{
				name:         "non_nullable_field_null_value",
				field:        schema.Field{Name: "id", Type: schema.FieldTypeString, Nullable: false},
				value:        nil,
				expectErrors: 1,
			},
			{
				name:         "non_nullable_field_non_null_value",
				field:        schema.Field{Name: "id", Type: schema.FieldTypeString, Nullable: false},
				value:        "123",
				expectErrors: 0,
			},
		}
		
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				errors, warnings := rule.Validate(tt.field, tt.value)
				assert.Equal(t, tt.expectErrors, len(errors))
				assert.Equal(t, 0, len(warnings))
			})
		}
	})
	
	t.Run("FormatValidationRule", func(t *testing.T) {
		rule := NewFormatValidationRule()
		
		tests := []struct {
			name           string
			field          schema.Field
			value          any
			expectWarnings int
		}{
			{
				name:           "email_field_valid_email",
				field:          schema.Field{Name: "email", Type: schema.FieldTypeString},
				value:          "user@example.com",
				expectWarnings: 0,
			},
			{
				name:           "email_field_invalid_email",
				field:          schema.Field{Name: "email", Type: schema.FieldTypeString},
				value:          "not-an-email",
				expectWarnings: 1,
			},
			{
				name:           "phone_field_valid_phone",
				field:          schema.Field{Name: "phone", Type: schema.FieldTypeString},
				value:          "+1234567890",
				expectWarnings: 0,
			},
			{
				name:           "phone_field_invalid_phone",
				field:          schema.Field{Name: "phone", Type: schema.FieldTypeString},
				value:          "not-a-phone",
				expectWarnings: 1,
			},
			{
				name:           "non_string_field",
				field:          schema.Field{Name: "age", Type: schema.FieldTypeInt},
				value:          25,
				expectWarnings: 0, // Non-string fields are ignored
			},
		}
		
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				errors, warnings := rule.Validate(tt.field, tt.value)
				assert.Equal(t, 0, len(errors)) // Format rule only generates warnings
				assert.Equal(t, tt.expectWarnings, len(warnings))
			})
		}
	})
}

func TestCustomValidationRules(t *testing.T) {
	config := ValidatorConfig{
		CustomRules: map[string][]Rule{
			"TestObject": {
				{
					Field: "age",
					Type:  "range",
					Min:   &[]float64{0}[0],
					Max:   &[]float64{120}[0],
				},
				{
					Field:  "status",
					Type:   "enum",
					Values: []string{"active", "inactive", "pending"},
				},
			},
		},
		MaxSampleValues: 5,
	}
	validator := NewDefaultValidator(config)
	
	testSchema := schema.NewSchema("TestObject", []schema.Field{
		{Name: "id", Type: schema.FieldTypeString, Nullable: false},
		{Name: "age", Type: schema.FieldTypeInt, Nullable: true},
		{Name: "status", Type: schema.FieldTypeString, Nullable: false},
	}, 1)
	
	tests := []struct {
		name         string
		record       map[string]any
		expectValid  bool
		expectErrors int
	}{
		{
			name: "valid_custom_rules",
			record: map[string]any{
				"id":     "123",
				"age":    25,
				"status": "active",
			},
			expectValid:  true,
			expectErrors: 0,
		},
		{
			name: "age_below_range",
			record: map[string]any{
				"id":     "123",
				"age":    -5,
				"status": "active",
			},
			expectValid:  false,
			expectErrors: 1,
		},
		{
			name: "age_above_range",
			record: map[string]any{
				"id":     "123",
				"age":    150,
				"status": "active",
			},
			expectValid:  false,
			expectErrors: 1,
		},
		{
			name: "invalid_status_enum",
			record: map[string]any{
				"id":     "123",
				"age":    25,
				"status": "unknown",
			},
			expectValid:  false,
			expectErrors: 1,
		},
		{
			name: "multiple_custom_rule_violations",
			record: map[string]any{
				"id":     "123",
				"age":    -5,      // below range
				"status": "wrong", // invalid enum
			},
			expectValid:  false,
			expectErrors: 2,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.ValidateRecord(tt.record, testSchema)
			assert.Equal(t, tt.expectValid, result.Valid)
			assert.Equal(t, tt.expectErrors, len(result.Errors))
		})
	}
}

func TestValidatorWithSampling(t *testing.T) {
	config := ValidatorConfig{
		SamplingRate:    0.5, // Validate 50% of records
		MaxSampleValues: 5,
	}
	validator := NewDefaultValidator(config)
	
	testSchema := schema.NewSchema("TestObject", []schema.Field{
		{Name: "id", Type: schema.FieldTypeString, Nullable: false},
	}, 1)
	
	// Create a large batch to test sampling
	records := make([]map[string]any, 1000)
	for i := 0; i < 1000; i++ {
		records[i] = map[string]any{"id": "test"}
	}
	
	batch := &connector.RecordBatch{
		Object:  "TestObject",
		Records: records,
	}
	
	result := validator.ValidateBatch(batch, testSchema)
	
	assert.Equal(t, 1000, result.TotalRecords)
	assert.True(t, result.Valid)
	assert.Equal(t, 0, result.ErrorRecords)
	assert.True(t, result.ProcessingTime > 0)
}

func TestDriftDetector(t *testing.T) {
	config := DriftDetectorConfig{
		Threshold:     0.1,
		WindowSize:    10,
		MinSampleSize: 3,
	}
	detector := NewDriftDetector(config)
	
	// Simulate field statistics over time
	fieldStats1 := map[string]FieldStats{
		"name": {
			FieldName:        "name",
			PopulationRate:   0.95,
			NullCount:        5,
			NonNullCount:     95,
			TypeDistribution: map[string]int{"string": 95},
			UniqueValues:     85,
		},
	}
	
	fieldStats2 := map[string]FieldStats{
		"name": {
			FieldName:        "name",
			PopulationRate:   0.90, // Population rate decreased
			NullCount:        10,
			NonNullCount:     90,
			TypeDistribution: map[string]int{"string": 90},
			UniqueValues:     80,
		},
	}
	
	fieldStats3 := map[string]FieldStats{
		"name": {
			FieldName:        "name",
			PopulationRate:   0.70, // Significant drop
			NullCount:        30,
			NonNullCount:     70,
			TypeDistribution: map[string]int{"string": 65, "integer": 5}, // Type drift
			UniqueValues:     60,
		},
	}
	
	// Update detector with statistics over time
	detector.UpdateStats("TestObject", fieldStats1)
	time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	detector.UpdateStats("TestObject", fieldStats2)
	time.Sleep(10 * time.Millisecond)
	detector.UpdateStats("TestObject", fieldStats3)
	
	// Detect drift
	report, err := detector.DetectDrift("TestObject")
	require.NoError(t, err)
	require.NotNil(t, report)
	
	assert.Equal(t, "TestObject", report.ObjectName)
	assert.True(t, report.DriftDetected)
	assert.True(t, len(report.FieldDrifts) > 0)
	assert.True(t, report.OverallScore > 0)
	assert.True(t, len(report.Recommendations) > 0)
	
	// Check for population drift
	found := false
	for _, drift := range report.FieldDrifts {
		if drift.FieldName == "name" && drift.DriftType == "population" {
			found = true
			assert.InDelta(t, 0.70, drift.CurrentValue, 0.01)
			assert.True(t, drift.ChangePercent > 0)
		}
	}
	assert.True(t, found, "Expected to find population drift for name field")
}

func TestMetricsCollector(t *testing.T) {
	collector := NewMetricsCollector(nil, nil) // No S3 client for this test
	
	// Create a validation report
	report := collector.CreateReport("test_source", "sync_123")
	assert.Equal(t, "test_source", report.Source)
	assert.Equal(t, "sync_123", report.SyncID)
	assert.NotZero(t, report.Timestamp)
	
	// Create sample batch results
	batchResults := []BatchValidationResult{
		{
			Valid:         false,
			TotalRecords:  100,
			ValidRecords:  80,
			ErrorRecords:  20,
			WarningRecords: 10,
			FieldMetrics: map[string]FieldStats{
				"id": {
					FieldName:      "id",
					PopulationRate: 0.95,
					NullCount:      5,
					NonNullCount:   95,
				},
			},
		},
		{
			Valid:         true,
			TotalRecords:  50,
			ValidRecords:  50,
			ErrorRecords:  0,
			WarningRecords: 5,
			FieldMetrics: map[string]FieldStats{
				"id": {
					FieldName:      "id",
					PopulationRate: 1.0,
					NullCount:      0,
					NonNullCount:   50,
				},
			},
		},
	}
	
	// Add object validation
	collector.AddObjectValidation(report, "TestObject", batchResults)
	
	assert.Contains(t, report.ObjectReports, "TestObject")
	objectReport := report.ObjectReports["TestObject"]
	assert.Equal(t, int64(150), objectReport.TotalRecords)
	// Corrected understanding: ValidRecords + ErrorRecords = TotalRecords for each batch
	// Batch 1: 80 valid + 20 error = 100 total
	// Batch 2: 50 valid + 0 error = 50 total  
	// So processedRecords = totalRecords = 150
	assert.Equal(t, int64(150), objectReport.TotalRecords)
	assert.Equal(t, int64(150), objectReport.ProcessedRecords) // Same as totalRecords since all records are processed
	assert.Equal(t, int64(20), objectReport.ErrorRecords)
	assert.Equal(t, "error", objectReport.Status) // Has errors
	
	// Generate alerts with threshold
	config := ValidatorConfig{ErrorThreshold: 0.1} // 10% threshold
	collector.GenerateAlerts(report, config)
	
	// Should have alert since error rate is 20/150 = 13.3% > 10%
	assert.True(t, len(report.Alerts) > 0)
	
	// Finalize report
	collector.FinalizeReport(report)
	
	assert.Equal(t, 1, report.Summary.TotalObjects)
	assert.Equal(t, 0, report.Summary.SuccessfulObjects)
	assert.Equal(t, 0, report.Summary.WarningObjects)
	assert.Equal(t, 1, report.Summary.ErrorObjects)
	assert.Equal(t, "error", report.Summary.OverallStatus)
	assert.InDelta(t, 20.0/150.0, report.Summary.ErrorRate, 0.01)
}