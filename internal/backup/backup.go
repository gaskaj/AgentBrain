package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/agentbrain/agentbrain/internal/storage"
	"github.com/agentbrain/agentbrain/internal/storage/delta"
)

// backupEngine implements the BackupEngine interface
type backupEngine struct {
	store  *storage.S3Client
	config BackupConfig
	logger *slog.Logger
	mu     sync.RWMutex
}

// NewBackupEngine creates a new backup engine
func NewBackupEngine(store *storage.S3Client, config BackupConfig, logger *slog.Logger) BackupEngine {
	if logger == nil {
		logger = slog.Default()
	}
	
	return &backupEngine{
		store:  store,
		config: config,
		logger: logger,
	}
}

// CreateBackup creates a backup of the specified table at a given version
func (e *backupEngine) CreateBackup(ctx context.Context, source, table string, version int64) (*BackupMetadata, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.config.Enabled {
		return nil, fmt.Errorf("backup is not enabled")
	}

	startTime := time.Now()
	backupID := generateBackupID(source, table, startTime)
	
	metadata := &BackupMetadata{
		BackupID:      backupID,
		Source:        source,
		Table:         table,
		CreatedAt:     startTime,
		SourceVersion: version,
		SourcePath:    e.getSourcePath(source, table),
		FormatVersion: 1,
		Status:        BackupStatusInProgress,
		Files:         []BackupFile{},
	}

	e.logger.Info("starting backup", 
		"backup_id", backupID, 
		"source", source, 
		"table", table, 
		"version", version)

	// Create backup directory structure
	backupBasePath := e.getBackupPath(source, table, startTime)

	// Save initial metadata
	if err := e.saveBackupMetadata(ctx, metadata); err != nil {
		return nil, fmt.Errorf("save initial metadata: %w", err)
	}

	// Perform the actual backup
	if err := e.performBackup(ctx, source, table, version, backupBasePath, metadata); err != nil {
		metadata.Status = BackupStatusFailed
		metadata.ErrorMessage = err.Error()
		metadata.CompletedAt = &startTime
		
		// Save failed metadata
		if saveErr := e.saveBackupMetadata(ctx, metadata); saveErr != nil {
			e.logger.Error("failed to save error metadata", "error", saveErr)
		}
		
		return nil, fmt.Errorf("perform backup: %w", err)
	}

	// Finalize metadata
	completedAt := time.Now()
	metadata.Status = BackupStatusCompleted
	metadata.CompletedAt = &completedAt
	metadata.Duration = completedAt.Sub(startTime)
	
	// Calculate total size and final checksum
	metadata.TotalSize = e.calculateTotalSize(metadata.Files)
	metadata.Checksum = e.calculateBackupChecksum(metadata)

	// Save final metadata
	if err := e.saveBackupMetadata(ctx, metadata); err != nil {
		return nil, fmt.Errorf("save final metadata: %w", err)
	}

	e.logger.Info("backup completed", 
		"backup_id", backupID, 
		"duration", metadata.Duration,
		"files", metadata.TotalFiles,
		"size", metadata.TotalSize)

	return metadata, nil
}

// performBackup performs the actual backup operation
func (e *backupEngine) performBackup(ctx context.Context, source, table string, version int64, backupBasePath string, metadata *BackupMetadata) error {
	// Create a timeout context for the entire backup operation
	timeout := 30 * time.Minute // Default timeout
	backupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Get Delta table for this source/table
	logPrefix := fmt.Sprintf("%s/%s/_delta_log", source, table)
	deltaTable := delta.NewDeltaTable(e.store, source, table, logPrefix, e.logger)

	// Get the table snapshot at the specified version
	snapshot, err := deltaTable.Snapshot(backupCtx, version)
	if err != nil {
		return fmt.Errorf("get table snapshot: %w", err)
	}

	// Backup transaction logs
	if err := e.backupTransactionLogs(backupCtx, source, table, version, backupBasePath, metadata); err != nil {
		return fmt.Errorf("backup transaction logs: %w", err)
	}

	// Backup data files
	if err := e.backupDataFiles(backupCtx, snapshot, backupBasePath, metadata); err != nil {
		return fmt.Errorf("backup data files: %w", err)
	}

	// Create and save manifest
	if err := e.createManifest(backupCtx, backupBasePath, metadata); err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}

	return nil
}

// backupTransactionLogs backs up all transaction log files up to the specified version
func (e *backupEngine) backupTransactionLogs(ctx context.Context, source, table string, version int64, backupBasePath string, metadata *BackupMetadata) error {
	logPrefix := fmt.Sprintf("%s/%s/_delta_log", source, table)
	
	// List all log files up to the version
	for v := int64(0); v <= version; v++ {
		logFileName := fmt.Sprintf("%020d.json", v)
		sourcePath := fmt.Sprintf("%s/%s", logPrefix, logFileName)
		
		// Check if log file exists
		exists, err := e.store.Exists(ctx, sourcePath)
		if err != nil {
			return fmt.Errorf("check log file existence %s: %w", sourcePath, err)
		}
		
		if !exists {
			// Check for checkpoint file instead
			checkpointPath := fmt.Sprintf("%s/%020d.checkpoint.parquet", logPrefix, v)
			if exists, err := e.store.Exists(ctx, checkpointPath); err != nil {
				return fmt.Errorf("check checkpoint file existence %s: %w", checkpointPath, err)
			} else if exists {
				sourcePath = checkpointPath
				logFileName = fmt.Sprintf("%020d.checkpoint.parquet", v)
			} else {
				continue // Skip missing files
			}
		}
		
		// Download and copy log file
		data, err := e.store.Download(ctx, sourcePath)
		if err != nil {
			return fmt.Errorf("download log file %s: %w", sourcePath, err)
		}
		
		backupPath := fmt.Sprintf("%s_delta_log/%s", backupBasePath, logFileName)
		if err := e.store.Upload(ctx, backupPath, data, "application/json"); err != nil {
			return fmt.Errorf("upload log file to backup %s: %w", backupPath, err)
		}
		
		// Add to metadata
		backupFile := BackupFile{
			OriginalPath: sourcePath,
			BackupPath:   backupPath,
			Size:         int64(len(data)),
			Checksum:     generateChecksum(data),
			Type:         "log",
		}
		metadata.Files = append(metadata.Files, backupFile)
		metadata.TotalFiles++
	}
	
	// Also backup _last_checkpoint file if it exists
	lastCheckpointPath := fmt.Sprintf("%s/_last_checkpoint", logPrefix)
	if exists, err := e.store.Exists(ctx, lastCheckpointPath); err != nil {
		return fmt.Errorf("check last checkpoint file: %w", err)
	} else if exists {
		data, err := e.store.Download(ctx, lastCheckpointPath)
		if err != nil {
			return fmt.Errorf("download last checkpoint file: %w", err)
		}
		
		backupPath := fmt.Sprintf("%s_delta_log/_last_checkpoint", backupBasePath)
		if err := e.store.Upload(ctx, backupPath, data, "application/json"); err != nil {
			return fmt.Errorf("upload last checkpoint to backup: %w", err)
		}
		
		backupFile := BackupFile{
			OriginalPath: lastCheckpointPath,
			BackupPath:   backupPath,
			Size:         int64(len(data)),
			Checksum:     generateChecksum(data),
			Type:         "checkpoint",
		}
		metadata.Files = append(metadata.Files, backupFile)
		metadata.TotalFiles++
	}

	return nil
}

// backupDataFiles backs up all active data files referenced in the snapshot
func (e *backupEngine) backupDataFiles(ctx context.Context, snapshot *delta.Snapshot, backupBasePath string, metadata *BackupMetadata) error {
	// Use semaphore to limit concurrent uploads
	concurrency := e.config.ConcurrentUploads
	if concurrency <= 0 {
		concurrency = 4 // Default concurrency
	}
	
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, add := range snapshot.Files {
		wg.Add(1)
		go func(add *delta.Add) {
			defer wg.Done()
			
			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Download data file
			data, err := e.store.Download(ctx, add.Path)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("download data file %s: %w", add.Path, err)
				}
				mu.Unlock()
				return
			}
			
			// Upload to backup location
			backupPath := fmt.Sprintf("%sdata/%s", backupBasePath, add.Path)
			if err := e.store.Upload(ctx, backupPath, data, "application/octet-stream"); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("upload data file to backup %s: %w", backupPath, err)
				}
				mu.Unlock()
				return
			}
			
			// Add to metadata
			backupFile := BackupFile{
				OriginalPath: add.Path,
				BackupPath:   backupPath,
				Size:         int64(len(data)),
				Checksum:     generateChecksum(data),
				Type:         "data",
			}
			
			mu.Lock()
			metadata.Files = append(metadata.Files, backupFile)
			metadata.TotalFiles++
			mu.Unlock()
		}(add)
	}

	wg.Wait()
	return firstErr
}

// createManifest creates and saves the backup manifest file
func (e *backupEngine) createManifest(ctx context.Context, backupBasePath string, metadata *BackupMetadata) error {
	manifestData, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	manifestPath := fmt.Sprintf("%smanifest.json", backupBasePath)
	if err := e.store.Upload(ctx, manifestPath, manifestData, "application/json"); err != nil {
		return fmt.Errorf("upload manifest: %w", err)
	}

	// Also create checksum file
	checksumData := []byte(generateChecksum(manifestData))
	checksumPath := fmt.Sprintf("%schecksum.sha256", backupBasePath)
	if err := e.store.Upload(ctx, checksumPath, checksumData, "text/plain"); err != nil {
		return fmt.Errorf("upload checksum: %w", err)
	}

	return nil
}

// ListBackups returns all backups for a source/table combination
func (e *backupEngine) ListBackups(ctx context.Context, source, table string) ([]*BackupMetadata, error) {
	prefix := fmt.Sprintf("backups/%s/%s/", source, table)
	
	// List all backup directories
	keys, err := e.store.List(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("list backup directories: %w", err)
	}

	var backups []*BackupMetadata
	for _, key := range keys {
		if strings.HasSuffix(key, "/manifest.json") {
			// Extract backup metadata
			var metadata BackupMetadata
			if err := e.store.GetJSON(ctx, key, &metadata); err != nil {
				e.logger.Warn("failed to load backup metadata", "key", key, "error", err)
				continue
			}
			backups = append(backups, &metadata)
		}
	}

	return backups, nil
}

// GetBackupMetadata retrieves metadata for a specific backup
func (e *backupEngine) GetBackupMetadata(ctx context.Context, backupID string) (*BackupMetadata, error) {
	// Find backup by ID - this requires searching through backup directories
	// In a production system, you might maintain an index for faster lookups
	
	prefix := "backups/"
	keys, err := e.store.List(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("list backup keys: %w", err)
	}

	for _, key := range keys {
		if strings.HasSuffix(key, "/manifest.json") {
			var metadata BackupMetadata
			if err := e.store.GetJSON(ctx, key, &metadata); err != nil {
				continue // Skip invalid manifests
			}
			
			if metadata.BackupID == backupID {
				return &metadata, nil
			}
		}
	}

	return nil, fmt.Errorf("backup not found: %s", backupID)
}

// DeleteBackup removes a backup and all its files
func (e *backupEngine) DeleteBackup(ctx context.Context, backupID string) error {
	metadata, err := e.GetBackupMetadata(ctx, backupID)
	if err != nil {
		return fmt.Errorf("get backup metadata: %w", err)
	}

	// Delete all backup files
	backupBasePath := e.getBackupPath(metadata.Source, metadata.Table, metadata.CreatedAt)
	
	// List all files in the backup directory
	keys, err := e.store.List(ctx, backupBasePath)
	if err != nil {
		return fmt.Errorf("list backup files: %w", err)
	}

	// Delete all files
	for _, key := range keys {
		if err := e.store.Delete(ctx, key); err != nil {
			e.logger.Warn("failed to delete backup file", "key", key, "error", err)
		}
	}

	e.logger.Info("backup deleted", "backup_id", backupID)
	return nil
}

// ValidateBackup validates the integrity of a backup
func (e *backupEngine) ValidateBackup(ctx context.Context, backupID string) (*ValidationResult, error) {
	startTime := time.Now()
	
	metadata, err := e.GetBackupMetadata(ctx, backupID)
	if err != nil {
		return nil, fmt.Errorf("get backup metadata: %w", err)
	}

	result := &ValidationResult{
		Valid:     true,
		Issues:    []ValidationIssue{},
		Timestamp: startTime,
	}

	// Validate each file in the backup
	for _, file := range metadata.Files {
		result.CheckedFiles++
		
		// Check if file exists
		exists, err := e.store.Exists(ctx, file.BackupPath)
		if err != nil {
			result.addError(file.BackupPath, "existence_check_failed", err.Error())
			continue
		}
		
		if !exists {
			result.addError(file.BackupPath, "file_missing", "backup file does not exist")
			continue
		}

		// Validate checksum if validation mode requires it
		if e.config.ValidationMode == "checksum" || e.config.ValidationMode == "full" {
			data, err := e.store.Download(ctx, file.BackupPath)
			if err != nil {
				result.addError(file.BackupPath, "download_failed", err.Error())
				continue
			}
			
			actualChecksum := generateChecksum(data)
			if actualChecksum != file.Checksum {
				result.addError(file.BackupPath, "checksum_mismatch", 
					fmt.Sprintf("expected %s, got %s", file.Checksum, actualChecksum))
			}
		}
	}

	result.Duration = time.Since(startTime)
	result.Valid = len(result.Issues) == 0

	return result, nil
}

// Helper methods

func (e *backupEngine) getSourcePath(source, table string) string {
	return fmt.Sprintf("%s/%s", source, table)
}

func (e *backupEngine) getBackupPath(source, table string, timestamp time.Time) string {
	return fmt.Sprintf("backups/%s/%s/%s/", source, table, timestamp.Format("2006-01-02T15-04-05.000Z07-00"))
}

func (e *backupEngine) saveBackupMetadata(ctx context.Context, metadata *BackupMetadata) error {
	backupBasePath := e.getBackupPath(metadata.Source, metadata.Table, metadata.CreatedAt)
	manifestPath := fmt.Sprintf("%smanifest.json", backupBasePath)
	return e.store.PutJSON(ctx, manifestPath, metadata)
}

func (e *backupEngine) calculateTotalSize(files []BackupFile) int64 {
	var total int64
	for _, file := range files {
		total += file.Size
	}
	return total
}

func (e *backupEngine) calculateBackupChecksum(metadata *BackupMetadata) string {
	data, _ := json.Marshal(metadata.Files)
	return generateChecksum(data)
}

// addError adds a validation error to the result
func (r *ValidationResult) addError(file, issue, description string) {
	r.Valid = false
	r.Issues = append(r.Issues, ValidationIssue{
		Severity:    "error",
		File:        file,
		Issue:       issue,
		Description: description,
	})
}