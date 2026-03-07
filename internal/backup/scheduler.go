package backup

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// backupScheduler implements the BackupScheduler interface
type backupScheduler struct {
	backupEngine BackupEngine
	cron         *cron.Cron
	jobs         map[string]cron.EntryID // key: source_table, value: cron entry ID
	scheduled    map[string]ScheduledBackup
	config       BackupConfig
	logger       *slog.Logger
	mu           sync.RWMutex
	running      bool
}

// NewBackupScheduler creates a new backup scheduler
func NewBackupScheduler(backupEngine BackupEngine, config BackupConfig, logger *slog.Logger) BackupScheduler {
	if logger == nil {
		logger = slog.Default()
	}

	scheduler := &backupScheduler{
		backupEngine: backupEngine,
		cron:         cron.New(cron.WithSeconds()),
		jobs:         make(map[string]cron.EntryID),
		scheduled:    make(map[string]ScheduledBackup),
		config:       config,
		logger:       logger,
	}

	return scheduler
}

// ScheduleBackup adds a backup job to the schedule
func (s *backupScheduler) ScheduleBackup(source, table, schedule string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.config.Enabled {
		return fmt.Errorf("backup is not enabled")
	}

	key := fmt.Sprintf("%s_%s", source, table)

	// Remove existing schedule if present
	if entryID, exists := s.jobs[key]; exists {
		s.cron.Remove(entryID)
		delete(s.jobs, key)
		s.logger.Info("removed existing backup schedule", "source", source, "table", table)
	}

	// Create backup job function
	job := func() {
		s.executeBackup(source, table)
	}

	// Add job to cron scheduler
	entryID, err := s.cron.AddFunc(schedule, job)
	if err != nil {
		return fmt.Errorf("add cron job: %w", err)
	}

	// Store job information
	s.jobs[key] = entryID

	// Calculate next run time
	entry := s.cron.Entry(entryID)
	nextRun := entry.Next

	scheduledBackup := ScheduledBackup{
		Source:     source,
		Table:      table,
		Schedule:   schedule,
		NextBackup: nextRun,
		Enabled:    true,
	}
	s.scheduled[key] = scheduledBackup

	s.logger.Info("scheduled backup job",
		"source", source,
		"table", table,
		"schedule", schedule,
		"next_run", nextRun)

	return nil
}

// UnscheduleBackup removes a backup job from the schedule
func (s *backupScheduler) UnscheduleBackup(source, table string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := fmt.Sprintf("%s_%s", source, table)

	entryID, exists := s.jobs[key]
	if !exists {
		return fmt.Errorf("no scheduled backup found for %s/%s", source, table)
	}

	// Remove from cron scheduler
	s.cron.Remove(entryID)
	delete(s.jobs, key)
	delete(s.scheduled, key)

	s.logger.Info("unscheduled backup job", "source", source, "table", table)
	return nil
}

// Start begins the backup scheduler
func (s *backupScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("scheduler is already running")
	}

	s.cron.Start()
	s.running = true

	s.logger.Info("backup scheduler started")

	// Start cleanup routine
	go s.cleanupRoutine(ctx)

	return nil
}

// Stop gracefully stops the backup scheduler
func (s *backupScheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	// Create a context with timeout for graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Stop the cron scheduler
	cronCtx := s.cron.Stop()

	// Wait for running jobs to complete or timeout
	select {
	case <-cronCtx.Done():
		s.logger.Info("backup scheduler stopped gracefully")
	case <-shutdownCtx.Done():
		s.logger.Warn("backup scheduler shutdown timeout, some jobs may still be running")
	}

	s.running = false
	return nil
}

// GetScheduledBackups returns all currently scheduled backups
func (s *backupScheduler) GetScheduledBackups() []ScheduledBackup {
	s.mu.RLock()
	defer s.mu.RUnlock()

	backups := make([]ScheduledBackup, 0, len(s.scheduled))
	for _, backup := range s.scheduled {
		// Update next run time from cron
		if entryID, exists := s.jobs[fmt.Sprintf("%s_%s", backup.Source, backup.Table)]; exists {
			entry := s.cron.Entry(entryID)
			backup.NextBackup = entry.Next
		}
		backups = append(backups, backup)
	}

	return backups
}

// executeBackup performs a backup operation for a scheduled job
func (s *backupScheduler) executeBackup(source, table string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	s.logger.Info("executing scheduled backup", "source", source, "table", table)

	startTime := time.Now()

	// Create backup at latest version (-1)
	metadata, err := s.backupEngine.CreateBackup(ctx, source, table, -1)
	if err != nil {
		s.logger.Error("scheduled backup failed",
			"source", source,
			"table", table,
			"error", err,
			"duration", time.Since(startTime))
		return
	}

	// Update last backup time in scheduled backup info
	s.mu.Lock()
	key := fmt.Sprintf("%s_%s", source, table)
	if scheduled, exists := s.scheduled[key]; exists {
		scheduled.LastBackup = &startTime
		s.scheduled[key] = scheduled
	}
	s.mu.Unlock()

	s.logger.Info("scheduled backup completed",
		"source", source,
		"table", table,
		"backup_id", metadata.BackupID,
		"duration", time.Since(startTime),
		"files", metadata.TotalFiles,
		"size", metadata.TotalSize)
}

// cleanupRoutine periodically cleans up old backups based on retention policy
func (s *backupScheduler) cleanupRoutine(ctx context.Context) {
	if s.config.RetentionDays <= 0 {
		s.logger.Info("backup retention disabled, skipping cleanup routine")
		return
	}

	ticker := time.NewTicker(24 * time.Hour) // Run cleanup daily
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("backup cleanup routine stopped")
			return
		case <-ticker.C:
			s.performCleanup(ctx)
		}
	}
}

// performCleanup removes backups older than the retention period
func (s *backupScheduler) performCleanup(ctx context.Context) {
	s.logger.Info("starting backup cleanup", "retention_days", s.config.RetentionDays)

	cutoffTime := time.Now().AddDate(0, 0, -s.config.RetentionDays)
	cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	// Get list of all scheduled backups to clean up
	scheduled := s.GetScheduledBackups()
	
	deletedCount := 0
	for _, backup := range scheduled {
		backups, err := s.backupEngine.ListBackups(cleanupCtx, backup.Source, backup.Table)
		if err != nil {
			s.logger.Error("failed to list backups for cleanup",
				"source", backup.Source,
				"table", backup.Table,
				"error", err)
			continue
		}

		for _, metadata := range backups {
			if metadata.CreatedAt.Before(cutoffTime) {
				if err := s.backupEngine.DeleteBackup(cleanupCtx, metadata.BackupID); err != nil {
					s.logger.Error("failed to delete old backup",
						"backup_id", metadata.BackupID,
						"created", metadata.CreatedAt,
						"error", err)
				} else {
					s.logger.Info("deleted old backup",
						"backup_id", metadata.BackupID,
						"source", metadata.Source,
						"table", metadata.Table,
						"created", metadata.CreatedAt)
					deletedCount++
				}
			}
		}
	}

	s.logger.Info("backup cleanup completed",
		"deleted_backups", deletedCount,
		"retention_days", s.config.RetentionDays)
}

// GetJobStatus returns the status of scheduled jobs
func (s *backupScheduler) GetJobStatus() map[string]JobStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := make(map[string]JobStatus)
	
	for key, entryID := range s.jobs {
		entry := s.cron.Entry(entryID)
		scheduled := s.scheduled[key]
		
		jobStatus := JobStatus{
			Key:        key,
			Source:     scheduled.Source,
			Table:      scheduled.Table,
			Schedule:   scheduled.Schedule,
			NextRun:    entry.Next,
			LastRun:    scheduled.LastBackup,
			Enabled:    scheduled.Enabled,
		}
		
		status[key] = jobStatus
	}
	
	return status
}

// JobStatus represents the status of a scheduled backup job
type JobStatus struct {
	Key      string     `json:"key"`
	Source   string     `json:"source"`
	Table    string     `json:"table"`
	Schedule string     `json:"schedule"`
	NextRun  time.Time  `json:"next_run"`
	LastRun  *time.Time `json:"last_run,omitempty"`
	Enabled  bool       `json:"enabled"`
}

// IsRunning returns true if the scheduler is currently running
func (s *backupScheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}