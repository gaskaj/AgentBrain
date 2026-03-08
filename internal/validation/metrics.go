package validation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/agentbrain/agentbrain/internal/storage"
)

// MetricsCollector aggregates and stores validation metrics.
type MetricsCollector struct {
	s3Client      *storage.S3Client
	storageLayout storage.Layout
	logger        *slog.Logger
}

// ValidationReport contains comprehensive validation results for a sync run.
type ValidationReport struct {
	Source        string                           `json:"source"`
	SyncID        string                           `json:"sync_id"`
	Timestamp     time.Time                        `json:"timestamp"`
	ObjectReports map[string]*ObjectValidationReport `json:"object_reports"`
	Summary       ValidationSummary                `json:"summary"`
	DriftReports  map[string]*DriftReport          `json:"drift_reports,omitempty"`
	Alerts        []ValidationAlert                `json:"alerts,omitempty"`
}

// ObjectValidationReport contains validation results for a single object.
type ObjectValidationReport struct {
	ObjectName         string                `json:"object_name"`
	BatchResults       []BatchValidationResult `json:"batch_results"`
	AggregatedMetrics  ValidationMetrics     `json:"aggregated_metrics"`
	TotalRecords       int64                 `json:"total_records"`
	ProcessedRecords   int64                 `json:"processed_records"`
	ErrorRecords       int64                 `json:"error_records"`
	WarningRecords     int64                 `json:"warning_records"`
	ProcessingTime     time.Duration         `json:"processing_time"`
	Status             string                `json:"status"` // "success", "warning", "error"
}

// ValidationSummary provides high-level validation metrics across all objects.
type ValidationSummary struct {
	TotalObjects      int     `json:"total_objects"`
	SuccessfulObjects int     `json:"successful_objects"`
	WarningObjects    int     `json:"warning_objects"`
	ErrorObjects      int     `json:"error_objects"`
	TotalRecords      int64   `json:"total_records"`
	ErrorRate         float64 `json:"error_rate"`
	WarningRate       float64 `json:"warning_rate"`
	OverallStatus     string  `json:"overall_status"` // "success", "warning", "error"
	ProcessingTime    time.Duration `json:"processing_time"`
}

// ValidationAlert represents an alert triggered by validation results.
type ValidationAlert struct {
	Type        string    `json:"type"`         // "error_threshold", "drift_detected", "data_quality"
	Severity    string    `json:"severity"`     // "low", "medium", "high", "critical"
	ObjectName  string    `json:"object_name"`
	Message     string    `json:"message"`
	Threshold   float64   `json:"threshold,omitempty"`
	ActualValue float64   `json:"actual_value,omitempty"`
	FieldName   string    `json:"field_name,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// NewMetricsCollector creates a new validation metrics collector.
func NewMetricsCollector(s3Client *storage.S3Client, logger *slog.Logger) *MetricsCollector {
	return &MetricsCollector{
		s3Client:      s3Client,
		storageLayout: storage.Layout{},
		logger:        logger,
	}
}

// CreateReport creates a new validation report for a sync run.
func (m *MetricsCollector) CreateReport(source, syncID string) *ValidationReport {
	return &ValidationReport{
		Source:        source,
		SyncID:        syncID,
		Timestamp:     time.Now(),
		ObjectReports: make(map[string]*ObjectValidationReport),
		DriftReports:  make(map[string]*DriftReport),
		Alerts:        make([]ValidationAlert, 0),
	}
}

// AddObjectValidation adds validation results for an object to the report.
func (m *MetricsCollector) AddObjectValidation(report *ValidationReport, objectName string, batchResults []BatchValidationResult) {
	objectReport := &ObjectValidationReport{
		ObjectName:   objectName,
		BatchResults: batchResults,
		Status:       "success",
	}
	
	// Aggregate metrics across all batches
	var totalRecords, processedRecords, errorRecords, warningRecords int64
	var totalProcessingTime time.Duration
	var allErrors []ValidationError
	var allWarnings []ValidationWarning
	
	// Collect field metrics for aggregation
	fieldMetricsMap := make(map[string][]FieldStats)
	
	for _, batch := range batchResults {
		totalRecords += int64(batch.TotalRecords)
		processedRecords += int64(batch.ValidRecords + batch.ErrorRecords)
		errorRecords += int64(batch.ErrorRecords)
		warningRecords += int64(batch.WarningRecords)
		totalProcessingTime += batch.ProcessingTime
		
		allErrors = append(allErrors, batch.Errors...)
		allWarnings = append(allWarnings, batch.Warnings...)
		
		// Collect field metrics
		for fieldName, fieldStats := range batch.FieldMetrics {
			if _, exists := fieldMetricsMap[fieldName]; !exists {
				fieldMetricsMap[fieldName] = make([]FieldStats, 0)
			}
			fieldMetricsMap[fieldName] = append(fieldMetricsMap[fieldName], fieldStats)
		}
	}
	
	objectReport.TotalRecords = totalRecords
	objectReport.ProcessedRecords = processedRecords
	objectReport.ErrorRecords = errorRecords
	objectReport.WarningRecords = warningRecords
	objectReport.ProcessingTime = totalProcessingTime
	
	// Calculate aggregated field metrics
	aggregatedFieldMetrics := m.aggregateFieldMetrics(fieldMetricsMap)
	
	// Calculate aggregated validation metrics
	if processedRecords > 0 {
		objectReport.AggregatedMetrics = ValidationMetrics{
			ErrorRate:            float64(errorRecords) / float64(processedRecords),
			WarningRate:          float64(warningRecords) / float64(processedRecords),
			ValidatedFields:      len(aggregatedFieldMetrics),
			TotalFields:          len(aggregatedFieldMetrics),
			FieldPopulationRates: make(map[string]float64),
		}
		
		// Calculate population rates
		var totalNullRate float64
		for fieldName, fieldStats := range aggregatedFieldMetrics {
			objectReport.AggregatedMetrics.FieldPopulationRates[fieldName] = fieldStats.PopulationRate
			totalNullRate += (1.0 - fieldStats.PopulationRate)
		}
		
		if len(aggregatedFieldMetrics) > 0 {
			objectReport.AggregatedMetrics.NullRate = totalNullRate / float64(len(aggregatedFieldMetrics))
		}
	}
	
	// Determine object status
	if errorRecords > 0 {
		objectReport.Status = "error"
	} else if warningRecords > 0 {
		objectReport.Status = "warning"
	}
	
	report.ObjectReports[objectName] = objectReport
}

// AddDriftReport adds a drift detection report to the validation report.
func (m *MetricsCollector) AddDriftReport(report *ValidationReport, objectName string, driftReport *DriftReport) {
	report.DriftReports[objectName] = driftReport
}

// GenerateAlerts analyzes validation results and generates alerts.
func (m *MetricsCollector) GenerateAlerts(report *ValidationReport, config ValidatorConfig) {
	timestamp := time.Now()
	
	// Check error thresholds for each object
	for objectName, objectReport := range report.ObjectReports {
		if objectReport.ProcessedRecords > 0 {
			errorRate := float64(objectReport.ErrorRecords) / float64(objectReport.ProcessedRecords)
			
			if errorRate > config.ErrorThreshold {
				alert := ValidationAlert{
					Type:        "error_threshold",
					Severity:    m.determineSeverity(errorRate, config.ErrorThreshold),
					ObjectName:  objectName,
					Message:     fmt.Sprintf("Error rate %.2f%% exceeds threshold %.2f%% for object %s", errorRate*100, config.ErrorThreshold*100, objectName),
					Threshold:   config.ErrorThreshold,
					ActualValue: errorRate,
					Timestamp:   timestamp,
				}
				report.Alerts = append(report.Alerts, alert)
			}
		}
	}
	
	// Check for drift alerts
	for objectName, driftReport := range report.DriftReports {
		if driftReport.DriftDetected {
			severity := "low"
			if driftReport.OverallScore > 0.7 {
				severity = "high"
			} else if driftReport.OverallScore > 0.4 {
				severity = "medium"
			}
			
			alert := ValidationAlert{
				Type:        "drift_detected",
				Severity:    severity,
				ObjectName:  objectName,
				Message:     fmt.Sprintf("Schema drift detected for object %s (score: %.2f)", objectName, driftReport.OverallScore),
				ActualValue: driftReport.OverallScore,
				Timestamp:   timestamp,
			}
			report.Alerts = append(report.Alerts, alert)
		}
	}
	
	// Check for data quality alerts
	for objectName, objectReport := range report.ObjectReports {
		for _, batch := range objectReport.BatchResults {
			for _, warning := range batch.Warnings {
				if warning.Type == "data_quality" {
					alert := ValidationAlert{
						Type:       "data_quality",
						Severity:   "low",
						ObjectName: objectName,
						FieldName:  warning.Field,
						Message:    fmt.Sprintf("Data quality issue in %s.%s: %s", objectName, warning.Field, warning.Message),
						Timestamp:  timestamp,
					}
					report.Alerts = append(report.Alerts, alert)
				}
			}
		}
	}
}

// FinalizeReport calculates summary metrics and finalizes the report.
func (m *MetricsCollector) FinalizeReport(report *ValidationReport) {
	start := report.Timestamp
	
	var totalObjects, successfulObjects, warningObjects, errorObjects int
	var totalRecords, totalErrorRecords, totalWarningRecords int64
	var totalProcessingTime time.Duration
	
	// Aggregate across all objects
	for _, objectReport := range report.ObjectReports {
		totalObjects++
		totalRecords += objectReport.TotalRecords
		totalErrorRecords += objectReport.ErrorRecords
		totalWarningRecords += objectReport.WarningRecords
		totalProcessingTime += objectReport.ProcessingTime
		
		switch objectReport.Status {
		case "success":
			successfulObjects++
		case "warning":
			warningObjects++
		case "error":
			errorObjects++
		}
	}
	
	// Calculate overall rates
	var errorRate, warningRate float64
	if totalRecords > 0 {
		errorRate = float64(totalErrorRecords) / float64(totalRecords)
		warningRate = float64(totalWarningRecords) / float64(totalRecords)
	}
	
	// Determine overall status
	overallStatus := "success"
	if errorObjects > 0 {
		overallStatus = "error"
	} else if warningObjects > 0 {
		overallStatus = "warning"
	}
	
	report.Summary = ValidationSummary{
		TotalObjects:      totalObjects,
		SuccessfulObjects: successfulObjects,
		WarningObjects:    warningObjects,
		ErrorObjects:      errorObjects,
		TotalRecords:      totalRecords,
		ErrorRate:         errorRate,
		WarningRate:       warningRate,
		OverallStatus:     overallStatus,
		ProcessingTime:    time.Since(start),
	}
}

// SaveReport saves the validation report to S3 storage.
func (m *MetricsCollector) SaveReport(ctx context.Context, report *ValidationReport) error {
	if m.s3Client == nil {
		return fmt.Errorf("S3 client not configured")
	}
	
	key := m.validationReportPath(report.Source, report.SyncID)
	
	if err := m.s3Client.PutJSON(ctx, key, report); err != nil {
		return fmt.Errorf("save validation report: %w", err)
	}
	
	if m.logger != nil {
		m.logger.Info("validation report saved",
			"source", report.Source,
			"sync_id", report.SyncID,
			"overall_status", report.Summary.OverallStatus,
			"total_objects", report.Summary.TotalObjects,
			"total_records", report.Summary.TotalRecords,
			"error_rate", report.Summary.ErrorRate,
			"alerts", len(report.Alerts),
		)
	}
	
	return nil
}

// LoadReport loads a validation report from S3 storage.
func (m *MetricsCollector) LoadReport(ctx context.Context, source, syncID string) (*ValidationReport, error) {
	if m.s3Client == nil {
		return nil, fmt.Errorf("S3 client not configured")
	}
	
	key := m.validationReportPath(source, syncID)
	
	var report ValidationReport
	if err := m.s3Client.GetJSON(ctx, key, &report); err != nil {
		return nil, fmt.Errorf("load validation report: %w", err)
	}
	
	return &report, nil
}

// GetRecentReports retrieves the most recent validation reports for a source.
func (m *MetricsCollector) GetRecentReports(ctx context.Context, source string, limit int) ([]*ValidationReport, error) {
	if m.s3Client == nil {
		return nil, fmt.Errorf("S3 client not configured")
	}
	
	prefix := fmt.Sprintf("validation/%s/reports/", source)
	
	// List objects with the prefix
	objects, err := m.s3Client.ListObjectsWithMetadata(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("list validation reports: %w", err)
	}
	
	// Sort by modification time (most recent first)
	for i := range objects {
		for j := i + 1; j < len(objects); j++ {
			if objects[i].LastModified.Before(objects[j].LastModified) {
				objects[i], objects[j] = objects[j], objects[i]
			}
		}
	}
	
	reports := make([]*ValidationReport, 0, limit)
	count := 0
	
	for _, obj := range objects {
		if count >= limit {
			break
		}
		
		var report ValidationReport
		if err := m.s3Client.GetJSON(ctx, obj.Key, &report); err != nil {
			if m.logger != nil {
				m.logger.Warn("failed to load validation report", "key", obj.Key, "error", err)
			}
			continue
		}
		
		reports = append(reports, &report)
		count++
	}
	
	return reports, nil
}

// aggregateFieldMetrics combines field metrics from multiple batches.
func (m *MetricsCollector) aggregateFieldMetrics(fieldMetricsMap map[string][]FieldStats) map[string]FieldStats {
	result := make(map[string]FieldStats)
	
	for fieldName, statsList := range fieldMetricsMap {
		if len(statsList) == 0 {
			continue
		}
		
		aggregated := FieldStats{
			FieldName:        fieldName,
			TypeDistribution: make(map[string]int),
			SampleValues:     make([]any, 0),
		}
		
		var totalNull, totalNonNull int
		var totalUnique int
		
		// Aggregate counts
		for _, stats := range statsList {
			totalNull += stats.NullCount
			totalNonNull += stats.NonNullCount
			totalUnique += stats.UniqueValues
			
			// Merge type distributions
			for typeName, count := range stats.TypeDistribution {
				aggregated.TypeDistribution[typeName] += count
			}
			
			// Collect sample values (up to a limit)
			for _, value := range stats.SampleValues {
				if len(aggregated.SampleValues) < 10 { // Limit sample values
					aggregated.SampleValues = append(aggregated.SampleValues, value)
				}
			}
		}
		
		aggregated.NullCount = totalNull
		aggregated.NonNullCount = totalNonNull
		aggregated.UniqueValues = totalUnique
		
		// Calculate population rate
		total := totalNull + totalNonNull
		if total > 0 {
			aggregated.PopulationRate = float64(totalNonNull) / float64(total)
		}
		
		result[fieldName] = aggregated
	}
	
	return result
}

// determineSeverity determines alert severity based on how much a value exceeds a threshold.
func (m *MetricsCollector) determineSeverity(actualValue, threshold float64) string {
	ratio := actualValue / threshold
	
	if ratio >= 3.0 {
		return "critical"
	} else if ratio >= 2.0 {
		return "high"
	} else if ratio >= 1.5 {
		return "medium"
	}
	return "low"
}

// Helper methods for S3 paths

func (m *MetricsCollector) validationReportPath(source, syncID string) string {
	return fmt.Sprintf("validation/%s/reports/%s.json", source, syncID)
}

