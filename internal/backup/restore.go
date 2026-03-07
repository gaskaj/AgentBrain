package backup

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/agentbrain/agentbrain/internal/storage"
)

// restoreEngine implements the RestoreEngine interface
type restoreEngine struct {
	store         *storage.S3Client
	backupEngine  BackupEngine
	config        BackupConfig
	logger        *slog.Logger
	mu            sync.RWMutex
}

// NewRestoreEngine creates a new restore engine
func NewRestoreEngine(store *storage.S3Client, backupEngine BackupEngine, config BackupConfig, logger *slog.Logger) RestoreEngine {
	if logger == nil {
		logger = slog.Default()
	}
	
	return &restoreEngine{
		store:        store,
		backupEngine: backupEngine,
		config:       config,
		logger:       logger,
	}
}

// RestoreFromBackup restores a table from a backup
func (r *restoreEngine) RestoreFromBackup(ctx context.Context, backupID, targetSource, targetTable string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger.Info("starting restore operation",
		"backup_id", backupID,
		"target_source", targetSource,
		"target_table", targetTable)

	// Get backup metadata
	metadata, err := r.backupEngine.GetBackupMetadata(ctx, backupID)
	if err != nil {
		return fmt.Errorf("get backup metadata: %w", err)
	}

	// Validate that restore can be performed
	if err := r.ValidateRestore(ctx, backupID, targetSource, targetTable); err != nil {
		return fmt.Errorf("validate restore: %w", err)
	}

	// Create timeout context for restore operation
	timeout := 60 * time.Minute // Default timeout for restore
	restoreCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Perform the restore operation
	if err := r.performRestore(restoreCtx, metadata, targetSource, targetTable); err != nil {
		return fmt.Errorf("perform restore: %w", err)
	}

	r.logger.Info("restore operation completed",
		"backup_id", backupID,
		"target_source", targetSource,
		"target_table", targetTable,
		"restored_files", len(metadata.Files))

	return nil
}

// performRestore performs the actual restore operation
func (r *restoreEngine) performRestore(ctx context.Context, metadata *BackupMetadata, targetSource, targetTable string) error {
	// Calculate target paths
	targetLogPrefix := fmt.Sprintf("%s/%s/_delta_log", targetSource, targetTable)
	targetDataPrefix := fmt.Sprintf("%s/%s", targetSource, targetTable)

	// Check if target already exists and handle accordingly
	exists, err := r.checkTargetExists(ctx, targetLogPrefix)
	if err != nil {
		return fmt.Errorf("check target existence: %w", err)
	}

	if exists {
		r.logger.Warn("target table already exists, will overwrite", 
			"target", targetDataPrefix)
		// In a production system, you might want to create a backup of the existing table first
	}

	// Restore files in parallel with controlled concurrency
	concurrency := r.config.ConcurrentUploads
	if concurrency <= 0 {
		concurrency = 4
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	// Separate log files and data files for correct restore order
	logFiles := make([]BackupFile, 0)
	dataFiles := make([]BackupFile, 0)

	for _, file := range metadata.Files {
		if file.Type == "log" || file.Type == "checkpoint" {
			logFiles = append(logFiles, file)
		} else {
			dataFiles = append(dataFiles, file)
		}
	}

	// Restore log files first (sequentially to maintain order)
	for _, file := range logFiles {
		if err := r.restoreFile(ctx, file, targetSource, targetTable, metadata); err != nil {
			return fmt.Errorf("restore log file %s: %w", file.OriginalPath, err)
		}
	}

	// Then restore data files in parallel
	for _, file := range dataFiles {
		wg.Add(1)
		go func(file BackupFile) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := r.restoreFile(ctx, file, targetSource, targetTable, metadata); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("restore data file %s: %w", file.OriginalPath, err)
				}
				mu.Unlock()
			}
		}(file)
	}

	wg.Wait()
	return firstErr
}

// restoreFile restores a single file from backup
func (r *restoreEngine) restoreFile(ctx context.Context, file BackupFile, targetSource, targetTable string, sourceMetadata *BackupMetadata) error {
	// Download file from backup location
	data, err := r.store.Download(ctx, file.BackupPath)
	if err != nil {
		return fmt.Errorf("download from backup: %w", err)
	}

	// Verify checksum
	actualChecksum := generateChecksum(data)
	if actualChecksum != file.Checksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", file.Checksum, actualChecksum)
	}

	// Calculate target path
	var targetPath string
	if file.Type == "log" || file.Type == "checkpoint" {
		// For log files, replace the source/table part with target values
		originalLogPrefix := fmt.Sprintf("%s/%s/_delta_log", sourceMetadata.Source, sourceMetadata.Table)
		targetLogPrefix := fmt.Sprintf("%s/%s/_delta_log", targetSource, targetTable)
		targetPath = strings.Replace(file.OriginalPath, originalLogPrefix, targetLogPrefix, 1)
	} else {
		// For data files, replace the source/table part with target values
		originalDataPrefix := fmt.Sprintf("%s/%s", sourceMetadata.Source, sourceMetadata.Table)
		targetDataPrefix := fmt.Sprintf("%s/%s", targetSource, targetTable)
		targetPath = strings.Replace(file.OriginalPath, originalDataPrefix, targetDataPrefix, 1)
	}

	// Upload file to target location
	contentType := "application/octet-stream"
	if file.Type == "log" || file.Type == "checkpoint" {
		if strings.HasSuffix(file.OriginalPath, ".json") {
			contentType = "application/json"
		}
	}

	if err := r.store.Upload(ctx, targetPath, data, contentType); err != nil {
		return fmt.Errorf("upload to target: %w", err)
	}

	r.logger.Debug("restored file",
		"backup_path", file.BackupPath,
		"target_path", targetPath,
		"size", file.Size)

	return nil
}

// ValidateRestore validates that a restore can be performed
func (r *restoreEngine) ValidateRestore(ctx context.Context, backupID, targetSource, targetTable string) error {
	// Get backup metadata
	metadata, err := r.backupEngine.GetBackupMetadata(ctx, backupID)
	if err != nil {
		return fmt.Errorf("get backup metadata: %w", err)
	}

	// Validate backup integrity first
	validation, err := r.backupEngine.ValidateBackup(ctx, backupID)
	if err != nil {
		return fmt.Errorf("validate backup: %w", err)
	}

	if !validation.Valid {
		return fmt.Errorf("backup is not valid: %d issues found", len(validation.Issues))
	}

	// Check that target paths are valid
	if targetSource == "" || targetTable == "" {
		return fmt.Errorf("target source and table must not be empty")
	}

	// Check if backup contains required components
	hasLogFiles := false
	hasMetadata := false
	
	for _, file := range metadata.Files {
		if file.Type == "log" || file.Type == "checkpoint" {
			hasLogFiles = true
			if strings.Contains(file.OriginalPath, "00000000000000000000.json") {
				hasMetadata = true
			}
		}
	}

	if !hasLogFiles {
		return fmt.Errorf("backup does not contain transaction log files")
	}

	if !hasMetadata {
		return fmt.Errorf("backup does not contain table metadata")
	}

	return nil
}

// GetRestorePreview shows what would be restored without performing the operation
func (r *restoreEngine) GetRestorePreview(ctx context.Context, backupID string) (*RestorePreview, error) {
	metadata, err := r.backupEngine.GetBackupMetadata(ctx, backupID)
	if err != nil {
		return nil, fmt.Errorf("get backup metadata: %w", err)
	}

	// Calculate estimated duration based on file sizes
	// This is a rough estimate - could be improved with historical data
	totalSize := metadata.TotalSize
	estimatedMBPS := int64(10) // 10 MB/s estimate
	estimatedSeconds := totalSize / (estimatedMBPS * 1024 * 1024)
	if estimatedSeconds < 1 {
		estimatedSeconds = 1
	}

	preview := &RestorePreview{
		BackupID:          backupID,
		Source:            metadata.Source,
		Table:             metadata.Table,
		Version:           metadata.SourceVersion,
		CreatedAt:         metadata.CreatedAt,
		FilesToRestore:    metadata.Files,
		TotalSize:         totalSize,
		EstimatedDuration: time.Duration(estimatedSeconds) * time.Second,
	}

	return preview, nil
}

// checkTargetExists checks if the target table already exists
func (r *restoreEngine) checkTargetExists(ctx context.Context, targetLogPrefix string) (bool, error) {
	// Check if any log files exist at the target location
	keys, err := r.store.List(ctx, targetLogPrefix)
	if err != nil {
		return false, fmt.Errorf("list target location: %w", err)
	}

	return len(keys) > 0, nil
}

// RestorePreviewToString returns a human-readable summary of the restore preview
func (preview *RestorePreview) String() string {
	var sb strings.Builder
	
	sb.WriteString(fmt.Sprintf("Backup ID: %s\n", preview.BackupID))
	sb.WriteString(fmt.Sprintf("Source: %s/%s\n", preview.Source, preview.Table))
	sb.WriteString(fmt.Sprintf("Version: %d\n", preview.Version))
	sb.WriteString(fmt.Sprintf("Created: %s\n", preview.CreatedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Files to restore: %d\n", len(preview.FilesToRestore)))
	sb.WriteString(fmt.Sprintf("Total size: %.2f MB\n", float64(preview.TotalSize)/(1024*1024)))
	sb.WriteString(fmt.Sprintf("Estimated duration: %s\n", preview.EstimatedDuration))
	
	return sb.String()
}