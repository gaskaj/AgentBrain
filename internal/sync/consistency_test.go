package sync

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)



func TestNewConsistencyTracker(t *testing.T) {
	logger := slog.Default()
	
	config := ConsistencyConfig{
		Enabled: true,
		Relationships: map[string][]string{
			"Account": {"Contact", "Opportunity"},
			"Contact": {"Case"},
		},
		Windows: map[string]string{
			"Account": "2h",
			"Contact": "1h",
		},
		MaxStaleness: "24h",
	}

	tracker := NewConsistencyTracker(nil, "salesforce", config, logger)

	assert.NotNil(t, tracker)
	assert.Equal(t, "salesforce", tracker.source)
	assert.Equal(t, 2, len(tracker.relationships))
	assert.Equal(t, []string{"Contact", "Opportunity"}, tracker.relationships["Account"])
	assert.Equal(t, 2*time.Hour, tracker.windows["Account"])
	assert.Equal(t, 1*time.Hour, tracker.windows["Contact"])
	assert.Equal(t, 24*time.Hour, tracker.maxStaleness)
}

func TestValidateSync_HealthyState(t *testing.T) {
	logger := slog.Default()
	
	config := ConsistencyConfig{
		Enabled: true,
		Relationships: map[string][]string{
			"Account": {"Contact"},
		},
		MaxStaleness: "24h",
	}

	tracker := NewConsistencyTracker(nil, "salesforce", config, logger)

	// Create test plans and state
	now := time.Now()
	plans := []*ObjectPlan{
		{ObjectName: "Account", Mode: SyncModeFull},
		{ObjectName: "Contact", Mode: SyncModeIncremental},
	}

	state := &SyncState{
		Source: "salesforce",
		Objects: map[string]ObjectState{
			"Account": {
				LastSyncTime:   now.Add(-1 * time.Hour),
				WatermarkValue: now.Add(-1 * time.Hour),
				TotalRecords:   1000,
			},
			"Contact": {
				LastSyncTime:   now.Add(-30 * time.Minute),
				WatermarkValue: now.Add(-30 * time.Minute),
				TotalRecords:   5000,
			},
		},
	}

	report := tracker.ValidateSync(context.Background(), plans, state)

	assert.NotNil(t, report)
	assert.Equal(t, "salesforce", report.Source)
	assert.Equal(t, 2, len(report.ObjectResults))
	assert.Equal(t, ConsistencyStatusHealthy, report.OverallStatus)
	assert.False(t, report.HasViolations())
	assert.False(t, report.HasCriticalViolations())
}

func TestValidateSync_StalenessViolation(t *testing.T) {
	logger := slog.Default()
	
	config := ConsistencyConfig{
		Enabled:      true,
		MaxStaleness: "1h",
	}

	tracker := NewConsistencyTracker(nil, "salesforce", config, logger)

	// Create test plans with stale object
	now := time.Now()
	plans := []*ObjectPlan{
		{ObjectName: "Account", Mode: SyncModeFull},
	}

	state := &SyncState{
		Source: "salesforce",
		Objects: map[string]ObjectState{
			"Account": {
				LastSyncTime:   now.Add(-3 * time.Hour), // 3 hours ago, exceeds 1h limit
				WatermarkValue: now.Add(-3 * time.Hour),
				TotalRecords:   1000,
			},
		},
	}

	report := tracker.ValidateSync(context.Background(), plans, state)

	assert.NotNil(t, report)
	assert.True(t, report.HasViolations())
	assert.Equal(t, 1, len(report.Violations))
	assert.Equal(t, ViolationTypeStaleness, report.Violations[0].Type)
	assert.Contains(t, report.Violations[0].Objects, "Account")
	assert.Equal(t, ConsistencyStatusDegraded, report.OverallStatus)
}

func TestValidateSync_MissingObjectViolation(t *testing.T) {
	logger := slog.Default()
	
	config := ConsistencyConfig{
		Enabled: true,
		Relationships: map[string][]string{
			"Account": {"Contact", "Opportunity"},
		},
		MaxStaleness: "24h",
	}

	tracker := NewConsistencyTracker(nil, "salesforce", config, logger)

	// Create test plans with missing dependent
	now := time.Now()
	plans := []*ObjectPlan{
		{ObjectName: "Account", Mode: SyncModeFull},
		// Missing Contact and Opportunity plans
	}

	state := &SyncState{
		Source: "salesforce",
		Objects: map[string]ObjectState{
			"Account": {
				LastSyncTime:   now.Add(-1 * time.Hour),
				WatermarkValue: now.Add(-1 * time.Hour),
				TotalRecords:   1000,
			},
		},
	}

	report := tracker.ValidateSync(context.Background(), plans, state)

	assert.NotNil(t, report)
	assert.True(t, report.HasViolations())
	assert.Equal(t, 1, len(report.Violations))
	assert.Equal(t, ViolationTypeMissingObject, report.Violations[0].Type)
	assert.Contains(t, report.Violations[0].Objects, "Account")
	assert.Contains(t, report.Violations[0].Description, "Contact")
	assert.Contains(t, report.Violations[0].Description, "Opportunity")
	assert.Equal(t, SeverityHigh, report.Violations[0].Severity)
}

func TestValidateSync_WatermarkDriftViolation(t *testing.T) {
	logger := slog.Default()
	
	config := ConsistencyConfig{
		Enabled: true,
		Relationships: map[string][]string{
			"Account": {"Contact"},
		},
		MaxStaleness: "24h",
	}

	tracker := NewConsistencyTracker(nil, "salesforce", config, logger)

	// Create test plans with watermark drift
	now := time.Now()
	plans := []*ObjectPlan{
		{ObjectName: "Account", Mode: SyncModeFull},
		{ObjectName: "Contact", Mode: SyncModeIncremental},
	}

	state := &SyncState{
		Source: "salesforce",
		Objects: map[string]ObjectState{
			"Account": {
				LastSyncTime:   now.Add(-1 * time.Hour),
				WatermarkValue: now.Add(-1 * time.Hour), // Recent watermark
				TotalRecords:   1000,
			},
			"Contact": {
				LastSyncTime:   now.Add(-30 * time.Minute),
				WatermarkValue: now.Add(-5 * time.Hour), // Old watermark, drift > 1h
				TotalRecords:   5000,
			},
		},
	}

	report := tracker.ValidateSync(context.Background(), plans, state)

	assert.NotNil(t, report)
	assert.True(t, report.HasViolations())
	assert.Equal(t, 1, len(report.Violations))
	assert.Equal(t, ViolationTypeWatermarkDrift, report.Violations[0].Type)
	assert.Contains(t, report.Violations[0].Objects, "Account")
	assert.Contains(t, report.Violations[0].Objects, "Contact")
}

func TestValidateSync_TransactionBoundaryViolation(t *testing.T) {
	logger := slog.Default()
	
	config := ConsistencyConfig{
		Enabled: true,
		Relationships: map[string][]string{
			"Account": {"Contact"},
		},
		MaxStaleness: "24h",
	}

	tracker := NewConsistencyTracker(nil, "salesforce", config, logger)

	// Create test plans with large sync time gap
	now := time.Now()
	plans := []*ObjectPlan{
		{ObjectName: "Account", Mode: SyncModeFull},
		{ObjectName: "Contact", Mode: SyncModeIncremental},
	}

	state := &SyncState{
		Source: "salesforce",
		Objects: map[string]ObjectState{
			"Account": {
				LastSyncTime:   now.Add(-1 * time.Hour),
				WatermarkValue: now.Add(-1 * time.Hour),
				TotalRecords:   1000,
			},
			"Contact": {
				LastSyncTime:   now.Add(-8 * time.Hour), // 7h gap, exceeds 6h threshold
				WatermarkValue: now.Add(-8 * time.Hour),
				TotalRecords:   5000,
			},
		},
	}

	report := tracker.ValidateSync(context.Background(), plans, state)

	assert.NotNil(t, report)
	assert.True(t, report.HasViolations())
	assert.True(t, len(report.Violations) >= 1)
	
	// Look for transaction boundary violation (may not be the first one)
	var foundTransactionBoundary bool
	for _, violation := range report.Violations {
		if violation.Type == ViolationTypeTransactionBoundary {
			foundTransactionBoundary = true
			assert.Contains(t, violation.Objects, "Account")
			assert.Contains(t, violation.Objects, "Contact")
			assert.Equal(t, SeverityMedium, violation.Severity)
			break
		}
	}
	assert.True(t, foundTransactionBoundary, "Expected to find a transaction boundary violation")
}

func TestCalculateOverallStatus(t *testing.T) {
	logger := slog.Default()
	config := ConsistencyConfig{}
	tracker := NewConsistencyTracker(nil, "test", config, logger)

	tests := []struct {
		name       string
		violations []ConsistencyViolation
		expected   ConsistencyStatus
	}{
		{
			name:       "no violations",
			violations: []ConsistencyViolation{},
			expected:   ConsistencyStatusHealthy,
		},
		{
			name: "low severity only",
			violations: []ConsistencyViolation{
				{Severity: SeverityLow},
			},
			expected: ConsistencyStatusDegraded,
		},
		{
			name: "high severity",
			violations: []ConsistencyViolation{
				{Severity: SeverityHigh},
			},
			expected: ConsistencyStatusDegraded,
		},
		{
			name: "critical severity",
			violations: []ConsistencyViolation{
				{Severity: SeverityCritical},
			},
			expected: ConsistencyStatusUnhealthy,
		},
		{
			name: "mixed severities with critical",
			violations: []ConsistencyViolation{
				{Severity: SeverityLow},
				{Severity: SeverityCritical},
				{Severity: SeverityMedium},
			},
			expected: ConsistencyStatusUnhealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := tracker.calculateOverallStatus(tt.violations)
			assert.Equal(t, tt.expected, status)
		})
	}
}

