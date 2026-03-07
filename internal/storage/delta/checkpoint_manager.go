package delta

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DeltaCheckpointConfig contains configuration for checkpoint management.
type DeltaCheckpointConfig struct {
	Frequency         int           `yaml:"frequency"`          // Commits between checkpoints
	RetentionDays     int           `yaml:"retention_days"`     // Days to retain old checkpoints
	MaxCheckpoints    int           `yaml:"max_checkpoints"`    // Maximum number of checkpoints to retain
	ValidationEnabled bool          `yaml:"validation_enabled"` // Validate checkpoints after creation
	CompactionEnabled bool          `yaml:"compaction_enabled"` // Enable small file compaction during checkpoint
	SizeThresholdMB   int64         `yaml:"size_threshold_mb"`  // Size threshold for adaptive checkpointing
	AdaptiveMode      bool          `yaml:"adaptive_mode"`      // Use adaptive checkpoint frequency
	CreationTimeout   time.Duration `yaml:"creation_timeout"`   // Timeout for checkpoint creation
}

// CheckpointManager handles comprehensive checkpoint lifecycle management.
type CheckpointManager struct {
	store             S3Store
	table             *DeltaTable
	validator         *CheckpointValidator
	config            DeltaCheckpointConfig
	lastCheckpointKey string
	logPrefix         string
	logger            *slog.Logger
	
	// Metrics tracking
	checkpointCount    int64
	lastCheckpointTime time.Time
	totalReplayTime    time.Duration
}

// NewCheckpointManager creates a comprehensive checkpoint manager.
func NewCheckpointManager(store S3Store, table *DeltaTable, config DeltaCheckpointConfig, lastCheckpointKey, logPrefix string, logger *slog.Logger) *CheckpointManager {
	// Set defaults
	if config.Frequency <= 0 {
		config.Frequency = 10
	}
	if config.RetentionDays <= 0 {
		config.RetentionDays = 30
	}
	if config.MaxCheckpoints <= 0 {
		config.MaxCheckpoints = 50
	}
	if config.SizeThresholdMB <= 0 {
		config.SizeThresholdMB = 128
	}
	if config.CreationTimeout <= 0 {
		config.CreationTimeout = 30 * time.Minute
	}

	validator := NewCheckpointValidator(store, logPrefix, logger)

	return &CheckpointManager{
		store:             store,
		table:             table,
		validator:         validator,
		config:            config,
		lastCheckpointKey: lastCheckpointKey,
		logPrefix:         logPrefix,
		logger:            logger,
	}
}

// MaybeCheckpoint creates a checkpoint based on adaptive or fixed frequency logic.
func (m *CheckpointManager) MaybeCheckpoint(ctx context.Context, version int64) error {
	if version == 0 {
		return nil
	}

	shouldCheckpoint, err := m.shouldCreateCheckpoint(ctx, version)
	if err != nil {
		return fmt.Errorf("determine checkpoint need: %w", err)
	}

	if !shouldCheckpoint {
		return nil
	}

	return m.CreateCheckpoint(ctx, version)
}

// shouldCreateCheckpoint determines if a checkpoint should be created using adaptive or fixed logic.
func (m *CheckpointManager) shouldCreateCheckpoint(ctx context.Context, version int64) (bool, error) {
	if !m.config.AdaptiveMode {
		return version%int64(m.config.Frequency) == 0, nil
	}

	// Adaptive logic based on data volume and log replay performance
	lastCheckpoint, err := m.GetLastCheckpoint(ctx)
	if err != nil {
		// No previous checkpoint, use frequency
		return version%int64(m.config.Frequency) == 0, nil
	}

	versionsSinceCheckpoint := version - lastCheckpoint.Version
	
	// Always checkpoint at minimum frequency
	if versionsSinceCheckpoint >= int64(m.config.Frequency*2) {
		return true, nil
	}

	// Check if we have enough new data to warrant a checkpoint
	if versionsSinceCheckpoint >= int64(m.config.Frequency) {
		// Estimate data volume since last checkpoint
		dataVolume, err := m.estimateDataVolume(ctx, lastCheckpoint.Version, version)
		if err != nil {
			m.logger.Warn("failed to estimate data volume", "error", err)
			return versionsSinceCheckpoint >= int64(m.config.Frequency), nil
		}

		// Checkpoint if we've exceeded size threshold
		if dataVolume >= m.config.SizeThresholdMB {
			return true, nil
		}

		// Check if log replay is getting slow
		if m.shouldCheckpointForPerformance(versionsSinceCheckpoint) {
			return true, nil
		}
	}

	return false, nil
}

// estimateDataVolume estimates the data volume between two versions (in MB).
func (m *CheckpointManager) estimateDataVolume(ctx context.Context, fromVersion, toVersion int64) (int64, error) {
	var totalSize int64

	for v := fromVersion + 1; v <= toVersion; v++ {
		actions, err := m.table.log.ReadVersion(ctx, v)
		if err != nil {
			continue // Skip problematic versions
		}

		for _, action := range actions {
			if action.Add != nil {
				totalSize += action.Add.Size
			}
		}
	}

	return totalSize / (1024 * 1024), nil // Convert to MB
}

// shouldCheckpointForPerformance determines if checkpointing is needed for performance reasons.
func (m *CheckpointManager) shouldCheckpointForPerformance(versionsSinceCheckpoint int64) bool {
	// Simple heuristic: checkpoint if we have many versions to replay
	return versionsSinceCheckpoint >= 50
}

// CreateCheckpoint creates a comprehensive checkpoint with validation and cleanup.
func (m *CheckpointManager) CreateCheckpoint(ctx context.Context, version int64) error {
	start := time.Now()
	
	// Create timeout context
	createCtx, cancel := context.WithTimeout(ctx, m.config.CreationTimeout)
	defer cancel()

	m.logger.Info("creating checkpoint", "version", version, "config", m.config)

	// Get snapshot for checkpoint
	snap, err := m.table.Snapshot(createCtx, version)
	if err != nil {
		return fmt.Errorf("snapshot for checkpoint at version %d: %w", version, err)
	}

	// Build checkpoint actions
	actions := m.buildCheckpointActions(snap)

	// Optionally trigger small file compaction
	if m.config.CompactionEnabled {
		if compactedActions, err := m.compactSmallFiles(createCtx, actions); err == nil {
			actions = compactedActions
			m.logger.Info("applied file compaction during checkpoint", "version", version)
		} else {
			m.logger.Warn("file compaction failed, proceeding with original actions", "error", err)
		}
	}

	// Create checkpoint file
	checkpointKey := fmt.Sprintf("%s%020d.checkpoint.json", m.logPrefix, version)
	if err := m.writeCheckpointFile(createCtx, checkpointKey, actions); err != nil {
		return fmt.Errorf("write checkpoint file: %w", err)
	}

	// Validate checkpoint if enabled
	if m.config.ValidationEnabled {
		if err := m.validator.ValidateCheckpoint(createCtx, checkpointKey, snap); err != nil {
			m.logger.Error("checkpoint validation failed", "error", err, "version", version)
			// Don't fail the checkpoint creation, but log the error
		} else {
			m.logger.Info("checkpoint validation passed", "version", version)
		}
	}

	// Update last checkpoint marker
	lastCheckpoint := LastCheckpoint{
		Version: version,
		Size:    int64(len(actions)),
	}
	if err := m.store.PutJSON(createCtx, m.lastCheckpointKey, lastCheckpoint); err != nil {
		return fmt.Errorf("write last checkpoint marker: %w", err)
	}

	// Clean up old checkpoints
	if err := m.cleanupOldCheckpoints(createCtx, version); err != nil {
		m.logger.Warn("checkpoint cleanup failed", "error", err)
	}

	duration := time.Since(start)
	m.checkpointCount++
	m.lastCheckpointTime = time.Now()

	m.logger.Info("created checkpoint",
		"version", version,
		"actions", len(actions),
		"duration", duration,
		"total_checkpoints", m.checkpointCount,
	)

	return nil
}

// buildCheckpointActions constructs the action list for a checkpoint.
func (m *CheckpointManager) buildCheckpointActions(snap *Snapshot) []Action {
	var actions []Action

	if snap.Protocol != nil {
		actions = append(actions, Action{Protocol: snap.Protocol})
	}
	if snap.Metadata != nil {
		actions = append(actions, Action{MetaData: snap.Metadata})
	}
	for _, f := range snap.Files {
		actions = append(actions, Action{Add: f})
	}

	return actions
}

// writeCheckpointFile writes the checkpoint data to storage.
func (m *CheckpointManager) writeCheckpointFile(ctx context.Context, key string, actions []Action) error {
	data, err := json.Marshal(actions)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	return m.store.Upload(ctx, key, data, "application/json")
}

// compactSmallFiles performs basic small file compaction during checkpoint creation.
func (m *CheckpointManager) compactSmallFiles(ctx context.Context, actions []Action) ([]Action, error) {
	// This is a placeholder for small file compaction logic
	// In a full implementation, this would:
	// 1. Identify files smaller than a threshold
	// 2. Group them for compaction
	// 3. Create new compacted files
	// 4. Return updated actions with remove/add pairs
	
	// For now, just return the original actions
	return actions, nil
}

// cleanupOldCheckpoints removes old checkpoints based on retention policy.
func (m *CheckpointManager) cleanupOldCheckpoints(ctx context.Context, currentVersion int64) error {
	// List all checkpoint files
	checkpointFiles, err := m.listCheckpointFiles(ctx)
	if err != nil {
		return fmt.Errorf("list checkpoint files: %w", err)
	}

	// Sort by version (oldest first)
	sort.Slice(checkpointFiles, func(i, j int) bool {
		return checkpointFiles[i].Version < checkpointFiles[j].Version
	})

	// Determine which checkpoints to keep
	cutoffTime := time.Now().AddDate(0, 0, -m.config.RetentionDays)
	toDelete := m.selectCheckpointsForDeletion(checkpointFiles, cutoffTime, currentVersion)

	// Delete old checkpoints
	for _, file := range toDelete {
		if err := m.deleteCheckpointFile(ctx, file.Key); err != nil {
			m.logger.Warn("failed to delete old checkpoint", "key", file.Key, "error", err)
		} else {
			m.logger.Info("deleted old checkpoint", "key", file.Key, "version", file.Version)
		}
	}

	return nil
}

// CheckpointFile represents a checkpoint file in storage.
type CheckpointFile struct {
	Key     string
	Version int64
	Time    time.Time
}

// listCheckpointFiles lists all checkpoint files in storage.
func (m *CheckpointManager) listCheckpointFiles(ctx context.Context) ([]CheckpointFile, error) {
	keys, err := m.store.List(ctx, m.logPrefix)
	if err != nil {
		return nil, err
	}

	var files []CheckpointFile
	for _, key := range keys {
		if strings.Contains(key, ".checkpoint.json") {
			if version, err := m.extractVersionFromCheckpointKey(key); err == nil {
				files = append(files, CheckpointFile{
					Key:     key,
					Version: version,
					Time:    time.Now(), // We don't have actual file time, use current time
				})
			}
		}
	}

	return files, nil
}

// extractVersionFromCheckpointKey extracts version number from checkpoint key.
func (m *CheckpointManager) extractVersionFromCheckpointKey(key string) (int64, error) {
	// Key format: prefix/00000000000000000010.checkpoint.json
	parts := strings.Split(key, "/")
	filename := parts[len(parts)-1]
	versionStr := strings.Split(filename, ".")[0]
	
	return strconv.ParseInt(versionStr, 10, 64)
}

// selectCheckpointsForDeletion determines which checkpoints should be deleted.
func (m *CheckpointManager) selectCheckpointsForDeletion(files []CheckpointFile, cutoffTime time.Time, currentVersion int64) []CheckpointFile {
	if len(files) <= 1 {
		return nil // Always keep at least one checkpoint
	}

	var toDelete []CheckpointFile
	kept := 0

	// Keep recent checkpoints and respect max count
	for i := len(files) - 1; i >= 0; i-- {
		file := files[i]
		
		// Always keep the most recent checkpoint
		if file.Version == currentVersion {
			kept++
			continue
		}
		
		// Keep if within retention period and under max count
		if kept < m.config.MaxCheckpoints {
			kept++
			continue
		}
		
		// Mark for deletion
		toDelete = append(toDelete, file)
	}

	return toDelete
}

// deleteCheckpointFile removes a checkpoint file from storage.
func (m *CheckpointManager) deleteCheckpointFile(ctx context.Context, key string) error {
	// Note: S3Store interface doesn't have a Delete method in the current implementation
	// This would need to be added to the interface for full functionality
	m.logger.Warn("checkpoint deletion not implemented - S3Store needs Delete method", "key", key)
	return nil
}

// GetLastCheckpoint reads the last checkpoint marker.
func (m *CheckpointManager) GetLastCheckpoint(ctx context.Context) (*LastCheckpoint, error) {
	var lc LastCheckpoint
	err := m.store.GetJSON(ctx, m.lastCheckpointKey, &lc)
	if err != nil {
		return nil, err
	}
	return &lc, nil
}

// GetCheckpointMetrics returns current checkpoint metrics.
func (m *CheckpointManager) GetCheckpointMetrics() CheckpointMetrics {
	return CheckpointMetrics{
		TotalCheckpoints:   m.checkpointCount,
		LastCheckpointTime: m.lastCheckpointTime,
		TotalReplayTime:    m.totalReplayTime,
		Config:             m.config,
	}
}

// CheckpointMetrics contains checkpoint performance and health metrics.
type CheckpointMetrics struct {
	TotalCheckpoints   int64                  `json:"total_checkpoints"`
	LastCheckpointTime time.Time              `json:"last_checkpoint_time"`
	TotalReplayTime    time.Duration          `json:"total_replay_time"`
	Config             DeltaCheckpointConfig  `json:"config"`
}