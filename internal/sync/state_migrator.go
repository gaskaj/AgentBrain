package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/agentbrain/agentbrain/internal/migration"
	"github.com/agentbrain/agentbrain/internal/migration/migrations"
	"github.com/agentbrain/agentbrain/internal/storage"
)

// MigratableStateStore wraps StateStore with migration capabilities.
type MigratableStateStore struct {
	*StateStore
	migrator *migration.MigrationEngine
	registry *migration.DefaultSchemaRegistry
}

// NewMigratableStateStore creates a migration-aware state store.
func NewMigratableStateStore(s3 *storage.S3Client, logger *slog.Logger, migrationConfig *migration.Config) (*MigratableStateStore, error) {
	baseStore := NewStateStore(s3, logger)
	
	// Create schema registry and register current schema
	registry := migration.NewSchemaRegistry()
	if err := registry.RegisterSchema(CurrentSyncStateVersion, &SyncState{}); err != nil {
		return nil, fmt.Errorf("register sync state schema: %w", err)
	}

	// Create migration engine
	migrator := migration.NewMigrationEngine(registry, s3, logger, migrationConfig)

	// Register migrations
	if err := migrator.RegisterMigration(&migrations.V001InitialVersion{}); err != nil {
		return nil, fmt.Errorf("register initial migration: %w", err)
	}

	return &MigratableStateStore{
		StateStore: baseStore,
		migrator:   migrator,
		registry:   registry,
	}, nil
}

// Load reads and migrates sync state for a source to the current version.
func (s *MigratableStateStore) Load(ctx context.Context, source string) (*SyncState, error) {
	key := s.layout.SyncState(source)

	// Try to load raw bytes first
	rawData, err := s.s3.Download(ctx, key)
	if err != nil {
		// If the state file doesn't exist, return a new empty state
		s.logger.Info("no existing sync state found, starting fresh", "source", source)
		return &SyncState{
			Source:  source,
			Objects: make(map[string]ObjectState),
		}, nil
	}

	// Detect current version
	currentVersion, err := migration.GetCurrentVersion(rawData)
	if err != nil {
		return nil, fmt.Errorf("detect state version: %w", err)
	}

	s.logger.Debug("detected state version", 
		"source", source, 
		"version", currentVersion, 
		"target", CurrentSyncStateVersion)

	// Migrate if necessary
	targetVersion := CurrentSyncStateVersion
	if currentVersion != targetVersion {
		s.logger.Info("migrating state", 
			"source", source, 
			"from", currentVersion, 
			"to", targetVersion)

		rawData, err = s.migrator.MigrateData(ctx, rawData, targetVersion)
		if err != nil {
			return nil, fmt.Errorf("migrate state: %w", err)
		}
	}

	// Extract the migrated state
	var versionedState migration.VersionedState
	if migration.IsVersioned(rawData) {
		if err := json.Unmarshal(rawData, &versionedState); err != nil {
			return nil, fmt.Errorf("unmarshal versioned state: %w", err)
		}
	} else {
		// Handle legacy unversioned data
		var state SyncState
		if err := json.Unmarshal(rawData, &state); err != nil {
			return nil, fmt.Errorf("unmarshal legacy state: %w", err)
		}
		versionedState = *migration.WrapLegacyData(&state)
	}

	// Extract the actual sync state from versioned wrapper
	var state SyncState
	if err := s.registry.ExtractData(&versionedState, &state); err != nil {
		return nil, fmt.Errorf("extract sync state: %w", err)
	}

	s.logger.Info("loaded sync state",
		"source", source,
		"version", versionedState.Version,
		"objects", len(state.Objects),
		"lastRun", state.LastRunAt,
	)

	return &state, nil
}

// Save persists the sync state with version information.
func (s *MigratableStateStore) Save(ctx context.Context, state *SyncState) error {
	key := s.layout.SyncState(state.Source)

	// Wrap state in versioned container
	versionedState := s.registry.GetVersionedState(state)

	// Save versioned state
	if err := s.s3.PutJSON(ctx, key, versionedState); err != nil {
		return fmt.Errorf("save versioned sync state for %s: %w", state.Source, err)
	}

	s.logger.Debug("saved versioned sync state", 
		"source", state.Source, 
		"version", versionedState.Version)
	return nil
}

// GetMigrationPlan returns the migration plan for a source's state.
func (s *MigratableStateStore) GetMigrationPlan(ctx context.Context, source string, targetVersion int) ([]migration.Migration, error) {
	key := s.layout.SyncState(source)

	rawData, err := s.s3.Download(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("load state for migration plan: %w", err)
	}

	currentVersion, err := migration.GetCurrentVersion(rawData)
	if err != nil {
		return nil, fmt.Errorf("detect current version: %w", err)
	}

	return s.migrator.GetMigrationPlan(currentVersion, targetVersion)
}

// ValidateState validates that a state conforms to its schema version.
func (s *MigratableStateStore) ValidateState(ctx context.Context, state *SyncState) error {
	return s.registry.ValidateSchema(CurrentSyncStateVersion, state)
}

// GetMigrator returns the underlying migration engine for advanced operations.
func (s *MigratableStateStore) GetMigrator() *migration.MigrationEngine {
	return s.migrator
}