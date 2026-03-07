package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// BackupMetadata represents the metadata of a backup
type BackupMetadata struct {
	// Backup identification
	BackupID       string    `json:"backup_id"`
	Source         string    `json:"source"`
	Table          string    `json:"table"`
	CreatedAt      time.Time `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`

	// Source table information
	SourceVersion  int64     `json:"source_version"`
	SourcePath     string    `json:"source_path"`
	
	// Backup format and validation
	FormatVersion  int       `json:"format_version"`
	Checksum       string    `json:"checksum"`
	TotalFiles     int       `json:"total_files"`
	TotalSize      int64     `json:"total_size"`
	
	// Status and metrics
	Status         BackupStatus `json:"status"`
	ErrorMessage   string       `json:"error_message,omitempty"`
	Duration       time.Duration `json:"duration"`
	
	// Files inventory
	Files          []BackupFile `json:"files"`
}

// BackupFile represents a file within a backup
type BackupFile struct {
	OriginalPath string `json:"original_path"`
	BackupPath   string `json:"backup_path"`
	Size         int64  `json:"size"`
	Checksum     string `json:"checksum"`
	Type         string `json:"type"` // "log", "data", "checkpoint"
}

// BackupStatus represents the status of a backup operation
type BackupStatus string

const (
	BackupStatusPending    BackupStatus = "pending"
	BackupStatusInProgress BackupStatus = "in_progress"
	BackupStatusCompleted  BackupStatus = "completed"
	BackupStatusFailed     BackupStatus = "failed"
)

// BackupConfig defines configuration for backup operations
type BackupConfig struct {
	Enabled          bool   `yaml:"enabled" json:"enabled"`
	DestinationBucket string `yaml:"destination_bucket" json:"destination_bucket"`
	DestinationRegion string `yaml:"destination_region" json:"destination_region"`
	Schedule         string `yaml:"schedule" json:"schedule"`
	RetentionDays    int    `yaml:"retention_days" json:"retention_days"`
	CrossRegion      bool   `yaml:"cross_region" json:"cross_region"`
	EncryptionKey    string `yaml:"encryption_key" json:"encryption_key"`
	ValidationMode   string `yaml:"validation_mode" json:"validation_mode"` // "checksum", "full", "none"
	ConcurrentUploads int   `yaml:"concurrent_uploads" json:"concurrent_uploads"`
	ChunkSizeMB      int    `yaml:"chunk_size_mb" json:"chunk_size_mb"`
}

// BackupEngine provides backup operations for Delta tables
type BackupEngine interface {
	// CreateBackup creates a backup of the specified table at a given version
	CreateBackup(ctx context.Context, source, table string, version int64) (*BackupMetadata, error)
	
	// ListBackups returns all backups for a source/table combination
	ListBackups(ctx context.Context, source, table string) ([]*BackupMetadata, error)
	
	// GetBackupMetadata retrieves metadata for a specific backup
	GetBackupMetadata(ctx context.Context, backupID string) (*BackupMetadata, error)
	
	// DeleteBackup removes a backup and all its files
	DeleteBackup(ctx context.Context, backupID string) error
	
	// ValidateBackup validates the integrity of a backup
	ValidateBackup(ctx context.Context, backupID string) (*ValidationResult, error)
}

// RestoreEngine provides restore operations for Delta tables
type RestoreEngine interface {
	// RestoreFromBackup restores a table from a backup
	RestoreFromBackup(ctx context.Context, backupID, targetSource, targetTable string) error
	
	// ValidateRestore validates that a restore can be performed
	ValidateRestore(ctx context.Context, backupID, targetSource, targetTable string) error
	
	// GetRestorePreview shows what would be restored without performing the operation
	GetRestorePreview(ctx context.Context, backupID string) (*RestorePreview, error)
}

// BackupScheduler manages automated backup scheduling
type BackupScheduler interface {
	// ScheduleBackup adds a backup job to the schedule
	ScheduleBackup(source, table, schedule string) error
	
	// UnscheduleBackup removes a backup job from the schedule
	UnscheduleBackup(source, table string) error
	
	// Start begins the backup scheduler
	Start(ctx context.Context) error
	
	// Stop gracefully stops the backup scheduler
	Stop(ctx context.Context) error
	
	// GetScheduledBackups returns all currently scheduled backups
	GetScheduledBackups() []ScheduledBackup
}

// BackupValidator provides backup validation functionality
type BackupValidator interface {
	// ValidateIntegrity validates backup file integrity
	ValidateIntegrity(ctx context.Context, backupID string) (*ValidationResult, error)
	
	// ValidateCompleteness ensures all required files are present
	ValidateCompleteness(ctx context.Context, backupID string) (*ValidationResult, error)
	
	// ValidateRestorability tests if backup can be successfully restored
	ValidateRestorability(ctx context.Context, backupID string) (*ValidationResult, error)
}

// ValidationResult represents the result of a backup validation
type ValidationResult struct {
	Valid        bool                `json:"valid"`
	Issues       []ValidationIssue   `json:"issues"`
	CheckedFiles int                 `json:"checked_files"`
	Duration     time.Duration       `json:"duration"`
	Timestamp    time.Time           `json:"timestamp"`
}

// ValidationIssue represents a validation problem found
type ValidationIssue struct {
	Severity    string `json:"severity"` // "error", "warning", "info"
	File        string `json:"file"`
	Issue       string `json:"issue"`
	Description string `json:"description"`
}

// RestorePreview shows what would be restored
type RestorePreview struct {
	BackupID      string    `json:"backup_id"`
	Source        string    `json:"source"`
	Table         string    `json:"table"`
	Version       int64     `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	FilesToRestore []BackupFile `json:"files_to_restore"`
	TotalSize     int64     `json:"total_size"`
	EstimatedDuration time.Duration `json:"estimated_duration"`
}

// ScheduledBackup represents a scheduled backup job
type ScheduledBackup struct {
	Source      string `json:"source"`
	Table       string `json:"table"`
	Schedule    string `json:"schedule"`
	LastBackup  *time.Time `json:"last_backup,omitempty"`
	NextBackup  time.Time  `json:"next_backup"`
	Enabled     bool   `json:"enabled"`
}

// generateBackupID creates a unique backup identifier
func generateBackupID(source, table string, timestamp time.Time) string {
	input := source + "_" + table + "_" + timestamp.Format("2006-01-02T15:04:05.000Z07:00")
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:8]) // First 8 bytes for shorter ID
}

// generateChecksum creates a SHA256 checksum for data
func generateChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// validateBackupPath validates the backup path structure
func validateBackupPath(backupPath string) bool {
	// Expected format: backups/{source}/{table}/{timestamp}/
	// This is a simple validation - could be enhanced
	return len(backupPath) > 0 && backupPath[len(backupPath)-1] == '/'
}