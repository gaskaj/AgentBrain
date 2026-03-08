package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/agentbrain/agentbrain/internal/storage"
)

// Migration defines a database migration.
type Migration interface {
	Version() int
	Description() string
	Up(ctx context.Context, oldData interface{}) (interface{}, error)
	Down(ctx context.Context, newData interface{}) (interface{}, error)
	Validate(ctx context.Context, data interface{}) error
}

// MigrationEngine manages and executes database migrations.
type MigrationEngine struct {
	registry     SchemaRegistry
	migrations   map[int]Migration
	storage      *storage.S3Client
	logger       *slog.Logger
	config       *Config
	backupPrefix string
}

// Config holds migration engine configuration.
type Config struct {
	AutoMigrate         bool          `yaml:"auto_migrate"`
	BackupBeforeMigrate bool          `yaml:"backup_before_migrate"`
	ValidationMode      string        `yaml:"validation_mode"` // strict, warn, skip
	MaxMigrationTime    time.Duration `yaml:"max_migration_time"`
}

// NewMigrationEngine creates a new migration engine.
func NewMigrationEngine(registry SchemaRegistry, s3 *storage.S3Client, logger *slog.Logger, config *Config) *MigrationEngine {
	if config == nil {
		config = &Config{
			AutoMigrate:         true,
			BackupBeforeMigrate: true,
			ValidationMode:      "strict",
			MaxMigrationTime:    5 * time.Minute,
		}
	}

	return &MigrationEngine{
		registry:     registry,
		migrations:   make(map[int]Migration),
		storage:      s3,
		logger:       logger,
		config:       config,
		backupPrefix: "migration-backups",
	}
}

// RegisterMigration registers a migration with the engine.
func (e *MigrationEngine) RegisterMigration(migration Migration) error {
	version := migration.Version()
	if version <= 0 {
		return fmt.Errorf("migration version must be positive, got %d", version)
	}

	if _, exists := e.migrations[version]; exists {
		return fmt.Errorf("migration for version %d already registered", version)
	}

	e.migrations[version] = migration
	e.logger.Debug("registered migration", "version", version, "description", migration.Description())
	return nil
}

// MigrateData migrates data from any version to the target version.
func (e *MigrationEngine) MigrateData(ctx context.Context, data []byte, targetVersion int) ([]byte, error) {
	// Check if data is already versioned
	var versionedState *VersionedState
	if IsVersioned(data) {
		if err := json.Unmarshal(data, &versionedState); err != nil {
			return nil, fmt.Errorf("unmarshal versioned data: %w", err)
		}
	} else {
		// Wrap legacy unversioned data
		var rawData interface{}
		if err := json.Unmarshal(data, &rawData); err != nil {
			return nil, fmt.Errorf("unmarshal legacy data: %w", err)
		}
		versionedState = WrapLegacyData(rawData)
	}

	currentVersion := versionedState.Version
	e.logger.Info("starting migration", 
		"from_version", currentVersion, 
		"to_version", targetVersion)

	if currentVersion == targetVersion {
		// Even if no migration is needed, return properly versioned data
		resultBytes, err := json.Marshal(versionedState)
		if err != nil {
			return nil, fmt.Errorf("marshal existing versioned data: %w", err)
		}
		return resultBytes, nil
	}

	// Create timeout context
	migrationCtx, cancel := context.WithTimeout(ctx, e.config.MaxMigrationTime)
	defer cancel()

	// Backup original data if configured
	if e.config.BackupBeforeMigrate {
		if err := e.createBackup(migrationCtx, data, currentVersion); err != nil {
			return nil, fmt.Errorf("create backup: %w", err)
		}
	}

	// Execute migration path
	migratedData, err := e.executeMigrationPath(migrationCtx, versionedState.Data, currentVersion, targetVersion)
	if err != nil {
		return nil, fmt.Errorf("execute migration path: %w", err)
	}

	// Create new versioned state
	result := &VersionedState{
		Version:   targetVersion,
		Data:      migratedData,
		CreatedAt: versionedState.CreatedAt,
		UpdatedAt: time.Now(),
	}

	// Validate final result if configured
	if e.config.ValidationMode == "strict" {
		if err := e.registry.ValidateSchema(targetVersion, migratedData); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	} else if e.config.ValidationMode == "warn" {
		if err := e.registry.ValidateSchema(targetVersion, migratedData); err != nil {
			e.logger.Warn("migration validation warning", "error", err)
		}
	}

	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal migrated data: %w", err)
	}

	e.logger.Info("migration completed successfully", 
		"from_version", currentVersion, 
		"to_version", targetVersion)

	return resultBytes, nil
}

// executeMigrationPath executes the sequence of migrations needed.
func (e *MigrationEngine) executeMigrationPath(ctx context.Context, data interface{}, fromVersion, toVersion int) (interface{}, error) {
	if fromVersion == toVersion {
		return data, nil
	}

	// Determine direction and create migration path
	var path []int
	if fromVersion < toVersion {
		// Forward migration
		for v := fromVersion + 1; v <= toVersion; v++ {
			if _, exists := e.migrations[v]; !exists {
				return nil, fmt.Errorf("missing migration for version %d", v)
			}
			path = append(path, v)
		}
	} else {
		// Reverse migration
		for v := fromVersion; v > toVersion; v-- {
			if _, exists := e.migrations[v]; !exists {
				return nil, fmt.Errorf("missing migration for version %d", v)
			}
			path = append(path, v)
		}
	}

	currentData := data
	for _, version := range path {
		migration := e.migrations[version]
		
		e.logger.Debug("executing migration step", 
			"version", version, 
			"description", migration.Description())

		var err error
		if fromVersion < toVersion {
			// Forward migration
			currentData, err = migration.Up(ctx, currentData)
		} else {
			// Reverse migration  
			currentData, err = migration.Down(ctx, currentData)
		}

		if err != nil {
			return nil, fmt.Errorf("migration %d failed: %w", version, err)
		}

		// Validate intermediate result
		if err := migration.Validate(ctx, currentData); err != nil {
			return nil, fmt.Errorf("validation failed for migration %d: %w", version, err)
		}
	}

	return currentData, nil
}

// createBackup creates a backup of the original data.
func (e *MigrationEngine) createBackup(ctx context.Context, data []byte, version int) error {
	timestamp := time.Now().Format("20060102-150405")
	key := fmt.Sprintf("%s/version-%d-%s.json", e.backupPrefix, version, timestamp)

	if err := e.storage.Upload(ctx, key, data, "application/json"); err != nil {
		return fmt.Errorf("save backup to %s: %w", key, err)
	}

	e.logger.Info("created migration backup", "key", key)
	return nil
}

// GetMigrationPlan returns the sequence of migrations needed to reach the target version.
func (e *MigrationEngine) GetMigrationPlan(fromVersion, toVersion int) ([]Migration, error) {
	if fromVersion == toVersion {
		return nil, nil
	}

	var plan []Migration
	if fromVersion < toVersion {
		// Forward migration plan
		for v := fromVersion + 1; v <= toVersion; v++ {
			migration, exists := e.migrations[v]
			if !exists {
				return nil, fmt.Errorf("missing migration for version %d", v)
			}
			plan = append(plan, migration)
		}
	} else {
		// Reverse migration plan
		for v := fromVersion; v > toVersion; v-- {
			migration, exists := e.migrations[v]
			if !exists {
				return nil, fmt.Errorf("missing migration for version %d", v)
			}
			plan = append(plan, migration)
		}
	}

	return plan, nil
}

// ListMigrations returns all registered migrations sorted by version.
func (e *MigrationEngine) ListMigrations() []Migration {
	var migrations []Migration
	var versions []int

	for version := range e.migrations {
		versions = append(versions, version)
	}

	sort.Ints(versions)
	for _, version := range versions {
		migrations = append(migrations, e.migrations[version])
	}

	return migrations
}

// GetCurrentVersion detects the version of the given data.
func GetCurrentVersion(data []byte) (int, error) {
	if IsVersioned(data) {
		var versionedState VersionedState
		if err := json.Unmarshal(data, &versionedState); err != nil {
			return 0, fmt.Errorf("unmarshal versioned data: %w", err)
		}
		return versionedState.Version, nil
	}
	// Legacy unversioned data is considered version 1
	return 1, nil
}