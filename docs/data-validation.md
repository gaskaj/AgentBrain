# Data Validation and Schema Drift Detection

This document describes the data validation framework and schema drift detection system that ensures data quality and monitors changes in data patterns over time.

## Overview

The validation framework provides comprehensive data quality assurance by:

1. **Validating data types and constraints** before writing to storage
2. **Detecting format issues** in common field types (emails, phones, URLs)
3. **Monitoring schema drift** to identify upstream system changes
4. **Generating alerts** when validation thresholds are exceeded
5. **Collecting metrics** for trend analysis and reporting

## Architecture

### Core Components

#### Validator (`internal/validation/validator.go`)
- **DefaultValidator**: Main implementation that applies validation rules to records and batches
- **ValidationResult**: Contains results for single record validation
- **BatchValidationResult**: Contains aggregated results for batch validation

#### Validation Rules (`internal/validation/rules.go`)
- **TypeConsistencyRule**: Validates field types match schema declarations
- **NullConstraintRule**: Ensures non-nullable fields don't contain null values
- **FormatValidationRule**: Checks format patterns for emails, phones, URLs, etc.
- **RangeValidationRule**: Validates numeric values are within expected ranges
- **DataQualityRule**: Detects common data quality issues

#### Drift Detector (`internal/validation/drift_detector.go`)
- **DriftDetector**: Monitors field statistics over time to detect pattern changes
- **DriftReport**: Contains drift analysis results and recommendations
- **FieldSnapshot**: Point-in-time statistics for field patterns

#### Metrics Collector (`internal/validation/metrics.go`)
- **MetricsCollector**: Aggregates validation results and generates reports
- **ValidationReport**: Comprehensive validation results for a sync run
- **ValidationAlert**: Alerts triggered by validation threshold violations

### Integration Points

The validation framework integrates with the sync engine at the batch processing level:

```
Extract Records → Validate Batch → Write to Parquet → Commit to Delta
```

Validation occurs after record extraction but before writing to storage, allowing the system to:
- Catch data quality issues early
- Optionally fail syncs in strict mode
- Log validation metrics for monitoring
- Generate drift detection alerts

## Configuration

### Basic Validation Configuration

```yaml
sources:
  salesforce_prod:
    validation:
      enabled: true
      error_threshold: 0.05      # Alert if >5% of records have errors
      drift_threshold: 0.10      # Alert if field patterns change >10%
      strict_mode: false         # Log warnings vs fail sync
```

### Custom Validation Rules

Object-specific validation rules can be configured:

```yaml
sources:
  salesforce_prod:
    validation:
      enabled: true
      error_threshold: 0.05
      custom_rules:
        Account:
          - field: "AnnualRevenue"
            type: "range"
            min: 0
            max: 1000000000
          - field: "Type"
            type: "enum"
            values: ["Customer", "Partner", "Prospect"]
        Contact:
          - field: "Email"
            type: "format"
            pattern: "email"
          - field: "Age"
            type: "range"
            min: 0
            max: 150
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `false` | Enable data validation |
| `error_threshold` | float | `0.05` | Error rate threshold for alerts (0-1) |
| `drift_threshold` | float | `0.10` | Drift score threshold for alerts (0-1) |
| `strict_mode` | boolean | `false` | Fail sync on validation errors |
| `custom_rules` | map | `{}` | Object-specific validation rules |

## Built-in Validation Rules

### Type Consistency
Validates that field values match their declared schema types:
- String fields contain strings
- Numeric fields contain numbers
- Boolean fields contain booleans
- Date fields contain valid date values

### Null Constraints
Ensures non-nullable fields don't contain null values based on schema declarations.

### Format Validation
Automatically detects and validates common field formats based on field names:
- **Email fields**: Must match email format pattern
- **Phone fields**: Must match phone number patterns
- **URL fields**: Must match URL format
- **UUID fields**: Must match UUID format

### Range Validation
Validates numeric values are within reasonable ranges:
- **Age fields**: 0-150
- **Percentage fields**: 0-100
- **Revenue/Amount fields**: >= 0

### Data Quality
Detects common data quality issues:
- Empty strings that should be null
- Suspicious placeholder values ("N/A", "test", "unknown")
- Excessive whitespace
- Unusually long values for name fields

## Custom Validation Rules

### Range Rules
Validate numeric fields are within specified bounds:

```yaml
custom_rules:
  Product:
    - field: "Price"
      type: "range"
      min: 0.01
      max: 10000.00
```

### Enum Rules
Validate string fields contain only allowed values:

```yaml
custom_rules:
  Order:
    - field: "Status"
      type: "enum"
      values: ["pending", "shipped", "delivered", "cancelled"]
```

### Format Rules
Validate string fields match specific patterns:

```yaml
custom_rules:
  Customer:
    - field: "CustomerID"
      type: "format"
      pattern: "^CUST-[0-9]{6}$"
```

## Schema Drift Detection

### How It Works

The drift detector monitors field statistics over time:
1. **Field population rates**: Percentage of records with non-null values
2. **Type distributions**: Percentage of each data type found
3. **Statistical patterns**: Mean, min, max for numeric fields
4. **Value patterns**: Unique value counts and sample values

### Drift Types

#### Population Drift
Detects changes in field population rates:
- Previously populated fields becoming sparse
- Previously sparse fields becoming more populated

#### Type Drift  
Detects changes in field type distributions:
- String fields starting to contain numbers
- Numeric fields containing string values
- New data types appearing

#### Statistical Drift
Detects changes in numeric field statistics:
- Significant changes in mean values
- Range expansions or contractions
- Distribution shape changes

### Drift Thresholds

Drift severity is categorized based on the magnitude of change:
- **Low**: 10-20% change from historical patterns
- **Medium**: 20-50% change from historical patterns  
- **High**: >50% change from historical patterns

### Recommendations

The system generates actionable recommendations based on drift patterns:
- Investigate specific fields with high drift
- Check for upstream system changes
- Review data transformation logic
- Validate connector configuration

## Validation Reports

### Report Structure

Validation reports contain comprehensive results for each sync run:

```json
{
  "source": "salesforce_prod",
  "sync_id": "sync_1672531200",
  "timestamp": "2023-01-01T00:00:00Z",
  "summary": {
    "total_objects": 5,
    "successful_objects": 3,
    "warning_objects": 1,
    "error_objects": 1,
    "total_records": 10000,
    "error_rate": 0.02,
    "warning_rate": 0.05,
    "overall_status": "warning"
  },
  "object_reports": {
    "Account": {
      "total_records": 5000,
      "error_records": 100,
      "warning_records": 250,
      "status": "warning",
      "aggregated_metrics": {
        "error_rate": 0.02,
        "warning_rate": 0.05
      }
    }
  },
  "alerts": [
    {
      "type": "error_threshold",
      "severity": "medium",
      "object_name": "Contact",
      "message": "Error rate 7.5% exceeds threshold 5.0%",
      "threshold": 0.05,
      "actual_value": 0.075
    }
  ]
}
```

### Report Storage

Reports are stored in S3 at:
- `validation/{source}/reports/{sync_id}.json`

Historical reports can be retrieved for trend analysis.

## Monitoring Integration

### Validation Alerts

The monitoring system generates alerts based on:
1. **Error rate thresholds**: When validation errors exceed configured rates
2. **Drift detection**: When schema drift is detected
3. **Data quality issues**: When significant quality problems are found

### Alert Configuration

```yaml
monitoring:
  rules:
    validation_error_rate:
      enabled: true
      threshold: 0.05
      severity: "warning"
    schema_drift:
      enabled: true
      drift_threshold: 0.10
      severity: "warning"
```

### Metrics

Key validation metrics tracked:
- Validation error rates by object
- Schema drift scores
- Field population rates
- Type distribution changes
- Processing time for validation

## Best Practices

### Threshold Tuning
- Start with permissive thresholds (5-10% error rates)
- Monitor for 1-2 weeks to establish baselines
- Gradually tighten thresholds based on historical patterns
- Consider different thresholds per object type

### Custom Rules
- Add custom rules for business-critical fields
- Use range validation for numeric business values
- Implement enum validation for status/category fields
- Add format validation for identifiers and codes

### Drift Detection
- Enable drift detection for production sources
- Review drift reports weekly
- Investigate high-severity drift immediately
- Update validation rules based on legitimate schema changes

### Performance Considerations
- Validation adds 5-10% overhead to sync time
- Use sampling for very large objects (>1M records)
- Consider running detailed validation on schedules
- Balance validation depth with sync performance requirements

## Troubleshooting

### High Error Rates
1. Check connector configuration for data type mappings
2. Review source system recent changes
3. Validate schema evolution handling
4. Consider if validation rules are too strict

### False Drift Alerts
1. Verify drift thresholds are appropriate
2. Check for legitimate business changes
3. Review data transformation logic changes
4. Consider seasonal or cyclical data patterns

### Performance Impact
1. Monitor validation processing time
2. Consider reducing sampling rates
3. Optimize custom validation rules
4. Review field selection for validation

### Missing Validations
1. Ensure validation is enabled in configuration
2. Check validator initialization in engine
3. Verify schema availability for objects
4. Review logs for validation errors

## API Reference

### Validator Interface

```go
type Validator interface {
    ValidateRecord(record map[string]any, schema *schema.Schema) ValidationResult
    ValidateBatch(batch *connector.RecordBatch, schema *schema.Schema) BatchValidationResult
}
```

### Configuration Types

```go
type ValidationConfig struct {
    Enabled         bool
    ErrorThreshold  float64
    DriftThreshold  float64
    StrictMode      bool
    CustomRules     map[string][]CustomRule
}

type CustomRule struct {
    Field    string
    Type     string
    Min      *float64
    Max      *float64
    Pattern  string
    Values   []string
    Required bool
}
```

### Drift Detection

```go
type DriftDetector interface {
    UpdateStats(objectName string, fieldStats map[string]FieldStats)
    DetectDrift(objectName string) (*DriftReport, error)
    SaveDriftMetrics(ctx context.Context, source, object string, report *DriftReport) error
}
```