package delta

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// Deprecated: Use CheckpointConfig from checkpoint_manager.go instead
const checkpointInterval = 10

// LastCheckpoint records which version was last checkpointed.
type LastCheckpoint struct {
	Version int64 `json:"version"`
	Size    int64 `json:"size"`
}

// LegacyCheckpointManager handles basic checkpointing (deprecated - use CheckpointManager).
type LegacyCheckpointManager struct {
	store             S3Store
	table             *DeltaTable
	lastCheckpointKey string
	logPrefix         string
	logger            *slog.Logger
}

// NewLegacyCheckpointManager creates a basic checkpoint manager (deprecated).
func NewLegacyCheckpointManager(store S3Store, table *DeltaTable, lastCheckpointKey, logPrefix string, logger *slog.Logger) *LegacyCheckpointManager {
	return &LegacyCheckpointManager{
		store:             store,
		table:             table,
		lastCheckpointKey: lastCheckpointKey,
		logPrefix:         logPrefix,
		logger:            logger,
	}
}

// MaybeCheckpoint creates a checkpoint if the version is at a checkpoint interval.
func (m *LegacyCheckpointManager) MaybeCheckpoint(ctx context.Context, version int64) error {
	if version == 0 || version%checkpointInterval != 0 {
		return nil
	}
	return m.CreateCheckpoint(ctx, version)
}

// CreateCheckpoint writes a JSON checkpoint (simplified - not Parquet for simplicity)
// containing the full snapshot state at the given version.
func (m *LegacyCheckpointManager) CreateCheckpoint(ctx context.Context, version int64) error {
	snap, err := m.table.Snapshot(ctx, version)
	if err != nil {
		return fmt.Errorf("snapshot for checkpoint at version %d: %w", version, err)
	}

	// Build the checkpoint as a list of actions representing current state
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

	data, err := json.Marshal(actions)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	checkpointKey := fmt.Sprintf("%s%020d.checkpoint.json", m.logPrefix, version)
	if err := m.store.Upload(ctx, checkpointKey, data, "application/json"); err != nil {
		return fmt.Errorf("upload checkpoint: %w", err)
	}

	lc := LastCheckpoint{
		Version: version,
		Size:    int64(len(actions)),
	}
	if err := m.store.PutJSON(ctx, m.lastCheckpointKey, lc); err != nil {
		return fmt.Errorf("write last checkpoint marker: %w", err)
	}

	m.logger.Info("created checkpoint", "version", version, "actions", len(actions))
	return nil
}

// GetLastCheckpoint reads the last checkpoint marker.
func (m *LegacyCheckpointManager) GetLastCheckpoint(ctx context.Context) (*LastCheckpoint, error) {
	var lc LastCheckpoint
	err := m.store.GetJSON(ctx, m.lastCheckpointKey, &lc)
	if err != nil {
		return nil, err
	}
	return &lc, nil
}
