package sync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/agentbrain/agentbrain/internal/schema"
	"github.com/agentbrain/agentbrain/internal/storage"
)

// ObjectState holds sync state for a single object.
type ObjectState struct {
	LastSyncTime    time.Time      `json:"lastSyncTime"`
	WatermarkField  string         `json:"watermarkField"`
	WatermarkValue  time.Time      `json:"watermarkValue"`
	SchemaHash      string         `json:"schemaHash"`
	SchemaVersion   int            `json:"schemaVersion"`
	PreviousSchema  *schema.Schema `json:"previousSchema,omitempty"`
	DeltaVersion    int64          `json:"deltaVersion"`
	TotalRecords    int64          `json:"totalRecords"`
	LastSyncRecords int64          `json:"lastSyncRecords"`
	SyncType        string         `json:"syncType"` // "full" or "incremental"
}

// SyncState holds the overall state for a source.
type SyncState struct {
	Source                  string                    `json:"source"`
	Objects                 map[string]ObjectState    `json:"objects"`
	LastRunAt               time.Time                 `json:"lastRunAt"`
	RunCount                int64                     `json:"runCount"`
	LastConsistencyCheck    time.Time                 `json:"lastConsistencyCheck"`
	ConsistencyViolations   []ConsistencyViolation    `json:"consistencyViolations"`
}

// StateStore persists and loads sync state from S3.
type StateStore struct {
	s3     *storage.S3Client
	layout storage.Layout
	logger *slog.Logger
}

// NewStateStore creates a state store backed by S3.
func NewStateStore(s3 *storage.S3Client, logger *slog.Logger) *StateStore {
	return &StateStore{
		s3:     s3,
		logger: logger,
	}
}

// Load reads the sync state for a source. Returns a new empty state if none exists.
func (s *StateStore) Load(ctx context.Context, source string) (*SyncState, error) {
	key := s.layout.SyncState(source)

	var state SyncState
	err := s.s3.GetJSON(ctx, key, &state)
	if err != nil {
		// If the state file doesn't exist, return a new empty state
		s.logger.Info("no existing sync state found, starting fresh", "source", source)
		return &SyncState{
			Source:  source,
			Objects: make(map[string]ObjectState),
		}, nil
	}

	s.logger.Info("loaded sync state",
		"source", source,
		"objects", len(state.Objects),
		"lastRun", state.LastRunAt,
	)
	return &state, nil
}

// Save persists the sync state for a source.
func (s *StateStore) Save(ctx context.Context, state *SyncState) error {
	key := s.layout.SyncState(state.Source)
	if err := s.s3.PutJSON(ctx, key, state); err != nil {
		return fmt.Errorf("save sync state for %s: %w", state.Source, err)
	}
	s.logger.Debug("saved sync state", "source", state.Source)
	return nil
}

// SaveSchemaVersion stores a schema version in the schema history.
func (s *StateStore) SaveSchemaVersion(ctx context.Context, source, objectName string, version int, schemaData any) error {
	key := s.layout.SchemaVersion(source, objectName, version)
	if err := s.s3.PutJSON(ctx, key, schemaData); err != nil {
		return fmt.Errorf("save schema v%d for %s/%s: %w", version, source, objectName, err)
	}
	return nil
}
