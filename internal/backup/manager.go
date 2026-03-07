package backup

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/agentbrain/agentbrain/internal/config"
	"github.com/agentbrain/agentbrain/internal/storage"
)

// Manager provides a unified interface for all backup operations
type Manager struct {
	engine    BackupEngine
	restore   RestoreEngine
	scheduler BackupScheduler
	validator BackupValidator
	store     *storage.S3Client
	config    config.BackupConfig
	logger    *slog.Logger
}

// NewManager creates a new backup manager with all components
func NewManager(store *storage.S3Client, cfg config.BackupConfig, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}

	backupConfig := BackupConfig{
		Enabled:           cfg.Enabled,
		DestinationBucket: cfg.DestinationBucket,
		DestinationRegion: cfg.DestinationRegion,
		Schedule:          cfg.Schedule,
		RetentionDays:     cfg.RetentionDays,
		CrossRegion:       cfg.CrossRegion,
		EncryptionKey:     cfg.EncryptionKey,
		ValidationMode:    cfg.ValidationMode,
		ConcurrentUploads: cfg.ConcurrentUploads,
		ChunkSizeMB:       cfg.ChunkSizeMB,
	}

	// Create all components
	engine := NewBackupEngine(store, backupConfig, logger)
	restore := NewRestoreEngine(store, engine, backupConfig, logger)
	scheduler := NewBackupScheduler(engine, backupConfig, logger)
	validator := NewBackupValidator(store, engine, backupConfig, logger)

	return &Manager{
		engine:    engine,
		restore:   restore,
		scheduler: scheduler,
		validator: validator,
		store:     store,
		config:    cfg,
		logger:    logger,
	}
}

// Engine returns the backup engine
func (m *Manager) Engine() BackupEngine {
	return m.engine
}

// Restore returns the restore engine
func (m *Manager) Restore() RestoreEngine {
	return m.restore
}

// Scheduler returns the backup scheduler
func (m *Manager) Scheduler() BackupScheduler {
	return m.scheduler
}

// Validator returns the backup validator
func (m *Manager) Validator() BackupValidator {
	return m.validator
}

// Start starts all managed services
func (m *Manager) Start(ctx context.Context) error {
	if !m.config.Enabled {
		m.logger.Info("backup system is disabled")
		return nil
	}

	if err := m.scheduler.Start(ctx); err != nil {
		return fmt.Errorf("start backup scheduler: %w", err)
	}

	m.logger.Info("backup manager started")
	return nil
}

// Stop stops all managed services
func (m *Manager) Stop(ctx context.Context) error {
	if !m.config.Enabled {
		return nil
	}

	if err := m.scheduler.Stop(ctx); err != nil {
		return fmt.Errorf("stop backup scheduler: %w", err)
	}

	m.logger.Info("backup manager stopped")
	return nil
}

// IsEnabled returns true if backup is enabled
func (m *Manager) IsEnabled() bool {
	return m.config.Enabled
}

// GetConfig returns the backup configuration
func (m *Manager) GetConfig() config.BackupConfig {
	return m.config
}