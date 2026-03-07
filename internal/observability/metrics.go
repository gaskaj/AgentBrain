package observability

import (
	"sync"
	"time"
)

// CheckpointMetrics contains checkpoint-related performance metrics.
type CheckpointMetrics struct {
	TotalCheckpoints      int64         `json:"total_checkpoints"`
	LastCheckpointTime    time.Time     `json:"last_checkpoint_time"`
	CheckpointDuration    time.Duration `json:"checkpoint_duration"`
	ValidationSuccesses   int64         `json:"validation_successes"`
	ValidationFailures    int64         `json:"validation_failures"`
	StorageSpaceSaved     int64         `json:"storage_space_saved_bytes"`
	LogReplayTime         time.Duration `json:"log_replay_time"`
	CompactionRuns        int64         `json:"compaction_runs"`
	CheckpointsDeleted    int64         `json:"checkpoints_deleted"`
}

// DeltaMetrics contains Delta Lake related metrics.
type DeltaMetrics struct {
	TablesManaged         int64                      `json:"tables_managed"`
	TotalCommits          int64                      `json:"total_commits"`
	ActiveFiles           int64                      `json:"active_files"`
	TotalDataSize         int64                      `json:"total_data_size_bytes"`
	CheckpointMetrics     CheckpointMetrics          `json:"checkpoint_metrics"`
	TableMetrics          map[string]TableMetrics    `json:"table_metrics"`
}

// TableMetrics contains metrics for individual Delta tables.
type TableMetrics struct {
	Source               string        `json:"source"`
	Object               string        `json:"object"`
	CurrentVersion       int64         `json:"current_version"`
	FileCount            int64         `json:"file_count"`
	DataSize             int64         `json:"data_size_bytes"`
	LastCommitTime       time.Time     `json:"last_commit_time"`
	LastCheckpointTime   time.Time     `json:"last_checkpoint_time"`
	CommitsPerHour       float64       `json:"commits_per_hour"`
}

// MetricsCollector collects and aggregates Delta Lake metrics.
type MetricsCollector struct {
	mu            sync.RWMutex
	deltaMetrics  DeltaMetrics
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		deltaMetrics: DeltaMetrics{
			TableMetrics: make(map[string]TableMetrics),
		},
	}
}

// RecordCheckpointCreated records a checkpoint creation event.
func (mc *MetricsCollector) RecordCheckpointCreated(tableName string, version int64, duration time.Duration) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.deltaMetrics.CheckpointMetrics.TotalCheckpoints++
	mc.deltaMetrics.CheckpointMetrics.LastCheckpointTime = time.Now()
	mc.deltaMetrics.CheckpointMetrics.CheckpointDuration = duration
	
	// Update table-specific metrics
	if table, exists := mc.deltaMetrics.TableMetrics[tableName]; exists {
		table.LastCheckpointTime = time.Now()
		mc.deltaMetrics.TableMetrics[tableName] = table
	}
}

// RecordCheckpointValidation records a checkpoint validation result.
func (mc *MetricsCollector) RecordCheckpointValidation(tableName string, success bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	if success {
		mc.deltaMetrics.CheckpointMetrics.ValidationSuccesses++
	} else {
		mc.deltaMetrics.CheckpointMetrics.ValidationFailures++
	}
}

// RecordLogReplay records the time taken for log replay.
func (mc *MetricsCollector) RecordLogReplay(tableName string, duration time.Duration) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.deltaMetrics.CheckpointMetrics.LogReplayTime = duration
}

// RecordStorageSaved records storage space saved by cleanup operations.
func (mc *MetricsCollector) RecordStorageSaved(bytes int64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.deltaMetrics.CheckpointMetrics.StorageSpaceSaved += bytes
}

// RecordCompactionRun records a file compaction operation.
func (mc *MetricsCollector) RecordCompactionRun(tableName string, filesBefore, filesAfter int64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.deltaMetrics.CheckpointMetrics.CompactionRuns++
}

// RecordCheckpointDeleted records a checkpoint deletion.
func (mc *MetricsCollector) RecordCheckpointDeleted(tableName string, version int64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.deltaMetrics.CheckpointMetrics.CheckpointsDeleted++
}

// RecordTableCommit records a commit to a Delta table.
func (mc *MetricsCollector) RecordTableCommit(source, object string, version int64, fileCount, dataSize int64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	tableName := source + "_" + object
	now := time.Now()
	
	mc.deltaMetrics.TotalCommits++
	
	table, exists := mc.deltaMetrics.TableMetrics[tableName]
	if !exists {
		table = TableMetrics{
			Source: source,
			Object: object,
		}
		mc.deltaMetrics.TablesManaged++
	}
	
	// Calculate commits per hour
	if !table.LastCommitTime.IsZero() {
		hoursSinceLastCommit := now.Sub(table.LastCommitTime).Hours()
		if hoursSinceLastCommit > 0 {
			table.CommitsPerHour = 1.0 / hoursSinceLastCommit
		}
	}
	
	table.CurrentVersion = version
	table.FileCount = fileCount
	table.DataSize = dataSize
	table.LastCommitTime = now
	
	mc.deltaMetrics.TableMetrics[tableName] = table
	
	// Update aggregate metrics
	mc.deltaMetrics.ActiveFiles += fileCount
	mc.deltaMetrics.TotalDataSize += dataSize
}

// GetDeltaMetrics returns a snapshot of current Delta Lake metrics.
func (mc *MetricsCollector) GetDeltaMetrics() DeltaMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	// Return a deep copy to avoid race conditions
	metrics := mc.deltaMetrics
	
	// Copy table metrics map
	metrics.TableMetrics = make(map[string]TableMetrics)
	for k, v := range mc.deltaMetrics.TableMetrics {
		metrics.TableMetrics[k] = v
	}
	
	return metrics
}

// GetTableMetrics returns metrics for a specific table.
func (mc *MetricsCollector) GetTableMetrics(source, object string) (TableMetrics, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	tableName := source + "_" + object
	metrics, exists := mc.deltaMetrics.TableMetrics[tableName]
	return metrics, exists
}

// GetCheckpointHealthScore calculates a health score for checkpointing (0-100).
func (mc *MetricsCollector) GetCheckpointHealthScore() float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	metrics := mc.deltaMetrics.CheckpointMetrics
	
	if metrics.TotalCheckpoints == 0 {
		return 50.0 // Neutral score if no checkpoints yet
	}
	
	score := 100.0
	
	// Reduce score for validation failures
	if totalValidations := metrics.ValidationSuccesses + metrics.ValidationFailures; totalValidations > 0 {
		failureRate := float64(metrics.ValidationFailures) / float64(totalValidations)
		score -= failureRate * 30.0 // Max 30 point penalty for failures
	}
	
	// Reduce score if checkpoints are stale
	if !metrics.LastCheckpointTime.IsZero() {
		hoursSinceLastCheckpoint := time.Since(metrics.LastCheckpointTime).Hours()
		if hoursSinceLastCheckpoint > 24 {
			stalePenalty := (hoursSinceLastCheckpoint - 24) * 2 // 2 points per hour after 24h
			if stalePenalty > 20 {
				stalePenalty = 20 // Max 20 point penalty
			}
			score -= stalePenalty
		}
	}
	
	// Ensure score is within bounds
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	
	return score
}

// Reset clears all metrics (useful for testing).
func (mc *MetricsCollector) Reset() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.deltaMetrics = DeltaMetrics{
		TableMetrics: make(map[string]TableMetrics),
	}
}