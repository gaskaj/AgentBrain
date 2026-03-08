package sync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/agentbrain/agentbrain/internal/storage"
	"github.com/google/uuid"
)

// ViolationType represents the type of consistency violation.
type ViolationType string

const (
	ViolationTypeStaleness          ViolationType = "staleness"
	ViolationTypeMissingObject     ViolationType = "missing_object"
	ViolationTypeSchemaConflict    ViolationType = "schema_conflict"
	ViolationTypeWatermarkDrift    ViolationType = "watermark_drift"
	ViolationTypeTransactionBoundary ViolationType = "transaction_boundary"
)

// Severity represents the severity level of a violation.
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// ConsistencyStatus represents the overall consistency status.
type ConsistencyStatus string

const (
	ConsistencyStatusHealthy    ConsistencyStatus = "healthy"
	ConsistencyStatusDegraded   ConsistencyStatus = "degraded"
	ConsistencyStatusUnhealthy  ConsistencyStatus = "unhealthy"
)

// ObjectResult represents the sync result for a single object.
type ObjectResult struct {
	ObjectName    string    `json:"object_name"`
	Success       bool      `json:"success"`
	Error         string    `json:"error,omitempty"`
	RecordCount   int64     `json:"record_count"`
	LastSyncTime  time.Time `json:"last_sync_time"`
	WatermarkValue time.Time `json:"watermark_value"`
	SyncDuration  time.Duration `json:"sync_duration"`
}

// ConsistencyViolation represents a detected consistency issue.
type ConsistencyViolation struct {
	Type            ViolationType `json:"type"`
	Objects         []string      `json:"objects"`
	Description     string        `json:"description"`
	Severity        Severity      `json:"severity"`
	SuggestedAction string        `json:"suggested_action"`
	DetectedAt      time.Time     `json:"detected_at"`
}

// SyncConsistencyReport represents a comprehensive consistency assessment.
type SyncConsistencyReport struct {
	SyncID        string                    `json:"sync_id"`
	Timestamp     time.Time                 `json:"timestamp"`
	Source        string                    `json:"source"`
	ObjectResults map[string]ObjectResult   `json:"object_results"`
	Violations    []ConsistencyViolation    `json:"violations"`
	OverallStatus ConsistencyStatus         `json:"overall_status"`
}

// HasViolations returns true if the report contains any violations.
func (r *SyncConsistencyReport) HasViolations() bool {
	return len(r.Violations) > 0
}

// HasCriticalViolations returns true if the report contains critical violations.
func (r *SyncConsistencyReport) HasCriticalViolations() bool {
	for _, v := range r.Violations {
		if v.Severity == SeverityCritical {
			return true
		}
	}
	return false
}

// ConsistencyConfig holds configuration for consistency validation.
type ConsistencyConfig struct {
	Enabled         bool                       `yaml:"enabled"`
	Relationships   map[string][]string        `yaml:"relationships"`
	Windows         map[string]string          `yaml:"staleness_windows"`
	MaxStaleness    string                     `yaml:"max_staleness"`
	RequiredObjects []string                   `yaml:"required_objects"`
	FailOnViolation bool                       `yaml:"fail_on_violation"`
}

// ConsistencyTracker tracks and validates cross-object consistency.
type ConsistencyTracker struct {
	s3Store       *storage.S3Client
	source        string
	relationships map[string][]string // object -> dependent objects
	windows       map[string]time.Duration // object -> max staleness
	maxStaleness  time.Duration
	logger        *slog.Logger
}

// NewConsistencyTracker creates a new consistency tracker.
func NewConsistencyTracker(s3Store *storage.S3Client, source string, config ConsistencyConfig, logger *slog.Logger) *ConsistencyTracker {
	relationships := make(map[string][]string)
	if config.Relationships != nil {
		relationships = config.Relationships
	}

	windows := make(map[string]time.Duration)
	for obj, windowStr := range config.Windows {
		if duration, err := time.ParseDuration(windowStr); err == nil {
			windows[obj] = duration
		}
	}

	maxStaleness := 24 * time.Hour // default
	if config.MaxStaleness != "" {
		if duration, err := time.ParseDuration(config.MaxStaleness); err == nil {
			maxStaleness = duration
		}
	}

	return &ConsistencyTracker{
		s3Store:       s3Store,
		source:        source,
		relationships: relationships,
		windows:       windows,
		maxStaleness:  maxStaleness,
		logger:        logger,
	}
}



// ValidateSync validates consistency across all synchronized objects.
func (ct *ConsistencyTracker) ValidateSync(ctx context.Context, plans []*ObjectPlan, state *SyncState) *SyncConsistencyReport {
	syncID := uuid.New().String()
	report := &SyncConsistencyReport{
		SyncID:        syncID,
		Timestamp:     time.Now(),
		Source:        ct.source,
		ObjectResults: make(map[string]ObjectResult),
		Violations:    make([]ConsistencyViolation, 0),
		OverallStatus: ConsistencyStatusHealthy,
	}

	ct.logger.Info("validating sync consistency", "sync_id", syncID, "objects", len(plans))

	// Build object results from sync plans and state
	for _, plan := range plans {
		objState, exists := state.Objects[plan.ObjectName]
		result := ObjectResult{
			ObjectName:     plan.ObjectName,
			Success:        true, // Assume success if we got this far
			LastSyncTime:   time.Now(),
			WatermarkValue: time.Now(),
		}

		if exists {
			result.LastSyncTime = objState.LastSyncTime
			result.WatermarkValue = objState.WatermarkValue
			result.RecordCount = objState.TotalRecords
		}

		report.ObjectResults[plan.ObjectName] = result
	}

	// Check for various types of violations
	ct.checkStalenessViolations(report)
	ct.checkMissingObjectViolations(report)
	ct.checkWatermarkDriftViolations(report)
	ct.checkTransactionBoundaryViolations(report)

	// Determine overall status
	report.OverallStatus = ct.calculateOverallStatus(report.Violations)

	ct.logger.Info("consistency validation completed",
		"sync_id", syncID,
		"violations", len(report.Violations),
		"status", report.OverallStatus,
	)

	return report
}

// checkStalenessViolations checks for objects that haven't been synced within their staleness window.
func (ct *ConsistencyTracker) checkStalenessViolations(report *SyncConsistencyReport) {
	now := time.Now()

	for objectName, result := range report.ObjectResults {
		maxAge := ct.maxStaleness
		if objectMaxAge, exists := ct.windows[objectName]; exists {
			maxAge = objectMaxAge
		}

		age := now.Sub(result.LastSyncTime)
		if age > maxAge {
			violation := ConsistencyViolation{
				Type:        ViolationTypeStaleness,
				Objects:     []string{objectName},
				Description: fmt.Sprintf("Object %s is stale (last sync: %v ago, max allowed: %v)", objectName, age.Truncate(time.Second), maxAge),
				Severity:    ct.determineStalenessServerity(age, maxAge),
				SuggestedAction: fmt.Sprintf("Trigger immediate sync for %s", objectName),
				DetectedAt:  time.Now(),
			}
			report.Violations = append(report.Violations, violation)
		}
	}
}

// checkMissingObjectViolations checks for missing required objects.
func (ct *ConsistencyTracker) checkMissingObjectViolations(report *SyncConsistencyReport) {
	// Check if any dependent objects are missing when their parent was synced
	for parentObject, dependents := range ct.relationships {
		if _, parentExists := report.ObjectResults[parentObject]; parentExists {
			var missingDependents []string
			for _, dependent := range dependents {
				if _, dependentExists := report.ObjectResults[dependent]; !dependentExists {
					missingDependents = append(missingDependents, dependent)
				}
			}

			if len(missingDependents) > 0 {
				violation := ConsistencyViolation{
					Type:        ViolationTypeMissingObject,
					Objects:     append([]string{parentObject}, missingDependents...),
					Description: fmt.Sprintf("Parent object %s was synced but dependent objects are missing: %s", parentObject, strings.Join(missingDependents, ", ")),
					Severity:    SeverityHigh,
					SuggestedAction: fmt.Sprintf("Sync missing dependent objects: %s", strings.Join(missingDependents, ", ")),
					DetectedAt:  time.Now(),
				}
				report.Violations = append(report.Violations, violation)
			}
		}
	}
}

// checkWatermarkDriftViolations checks for watermark drift between related objects.
func (ct *ConsistencyTracker) checkWatermarkDriftViolations(report *SyncConsistencyReport) {
	for parentObject, dependents := range ct.relationships {
		parentResult, parentExists := report.ObjectResults[parentObject]
		if !parentExists {
			continue
		}

		for _, dependent := range dependents {
			dependentResult, dependentExists := report.ObjectResults[dependent]
			if !dependentExists {
				continue
			}

			// Check if dependent is significantly behind parent
			drift := parentResult.WatermarkValue.Sub(dependentResult.WatermarkValue)
			if drift > time.Hour { // Allow 1 hour drift
				violation := ConsistencyViolation{
					Type:        ViolationTypeWatermarkDrift,
					Objects:     []string{parentObject, dependent},
					Description: fmt.Sprintf("Watermark drift between %s and %s: %v", parentObject, dependent, drift.Truncate(time.Second)),
					Severity:    ct.determineWatermarkDriftSeverity(drift),
					SuggestedAction: fmt.Sprintf("Sync %s to catch up with %s", dependent, parentObject),
					DetectedAt:  time.Now(),
				}
				report.Violations = append(report.Violations, violation)
			}
		}
	}
}

// checkTransactionBoundaryViolations checks for transaction boundary issues.
func (ct *ConsistencyTracker) checkTransactionBoundaryViolations(report *SyncConsistencyReport) {
	// Check if related objects were synced within a reasonable time window
	for parentObject, dependents := range ct.relationships {
		parentResult, parentExists := report.ObjectResults[parentObject]
		if !parentExists {
			continue
		}

		for _, dependent := range dependents {
			dependentResult, dependentExists := report.ObjectResults[dependent]
			if !dependentExists {
				continue
			}

			// Check if sync times are too far apart
			timeDiff := parentResult.LastSyncTime.Sub(dependentResult.LastSyncTime)
			if timeDiff < 0 {
				timeDiff = -timeDiff
			}

			if timeDiff > 6*time.Hour { // Allow 6 hours between related syncs
				violation := ConsistencyViolation{
					Type:        ViolationTypeTransactionBoundary,
					Objects:     []string{parentObject, dependent},
					Description: fmt.Sprintf("Sync time gap between related objects %s and %s: %v", parentObject, dependent, timeDiff.Truncate(time.Second)),
					Severity:    SeverityMedium,
					SuggestedAction: fmt.Sprintf("Consider syncing related objects %s and %s closer together", parentObject, dependent),
					DetectedAt:  time.Now(),
				}
				report.Violations = append(report.Violations, violation)
			}
		}
	}
}

// determineStalenessServerity determines the severity based on how stale the object is.
func (ct *ConsistencyTracker) determineStalenessServerity(age, maxAge time.Duration) Severity {
	ratio := float64(age) / float64(maxAge)
	switch {
	case ratio > 4:
		return SeverityCritical
	case ratio > 2:
		return SeverityHigh
	case ratio > 1.5:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

// determineWatermarkDriftSeverity determines severity based on watermark drift.
func (ct *ConsistencyTracker) determineWatermarkDriftSeverity(drift time.Duration) Severity {
	switch {
	case drift > 24*time.Hour:
		return SeverityCritical
	case drift > 12*time.Hour:
		return SeverityHigh
	case drift > 6*time.Hour:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

// calculateOverallStatus determines the overall consistency status based on violations.
func (ct *ConsistencyTracker) calculateOverallStatus(violations []ConsistencyViolation) ConsistencyStatus {
	if len(violations) == 0 {
		return ConsistencyStatusHealthy
	}

	hasCritical := false
	hasHigh := false

	for _, v := range violations {
		switch v.Severity {
		case SeverityCritical:
			hasCritical = true
		case SeverityHigh:
			hasHigh = true
		}
	}

	switch {
	case hasCritical:
		return ConsistencyStatusUnhealthy
	case hasHigh:
		return ConsistencyStatusDegraded
	default:
		return ConsistencyStatusDegraded
	}
}

// StoreReport stores the consistency report in S3.
func (ct *ConsistencyTracker) StoreReport(ctx context.Context, report *SyncConsistencyReport) error {
	key := fmt.Sprintf("consistency/%s/reports/%s.json", ct.source, report.SyncID)

	if err := ct.s3Store.PutJSON(ctx, key, report); err != nil {
		return fmt.Errorf("store consistency report: %w", err)
	}

	ct.logger.Debug("stored consistency report", "sync_id", report.SyncID, "key", key)
	return nil
}

// GetRecentReports retrieves recent consistency reports for analysis.
func (ct *ConsistencyTracker) GetRecentReports(ctx context.Context, limit int) ([]*SyncConsistencyReport, error) {
	// This would typically list objects with a prefix and parse the most recent ones
	// For now, returning empty slice as this would require implementing S3 listing
	return []*SyncConsistencyReport{}, nil
}