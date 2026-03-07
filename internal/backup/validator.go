package backup

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/agentbrain/agentbrain/internal/storage"
)

// backupValidator implements the BackupValidator interface
type backupValidator struct {
	store        *storage.S3Client
	backupEngine BackupEngine
	config       BackupConfig
	logger       *slog.Logger
}

// NewBackupValidator creates a new backup validator
func NewBackupValidator(store *storage.S3Client, backupEngine BackupEngine, config BackupConfig, logger *slog.Logger) BackupValidator {
	if logger == nil {
		logger = slog.Default()
	}
	
	return &backupValidator{
		store:        store,
		backupEngine: backupEngine,
		config:       config,
		logger:       logger,
	}
}

// ValidateIntegrity validates backup file integrity
func (v *backupValidator) ValidateIntegrity(ctx context.Context, backupID string) (*ValidationResult, error) {
	startTime := time.Now()
	v.logger.Info("starting integrity validation", "backup_id", backupID)
	
	metadata, err := v.backupEngine.GetBackupMetadata(ctx, backupID)
	if err != nil {
		return nil, fmt.Errorf("get backup metadata: %w", err)
	}

	result := &ValidationResult{
		Valid:     true,
		Issues:    []ValidationIssue{},
		Timestamp: startTime,
	}

	// Create timeout context for validation
	validationCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	// Validate manifest file exists and is readable
	manifestPath := v.getManifestPath(metadata)
	if err := v.validateManifestFile(validationCtx, manifestPath, result); err != nil {
		return result, err
	}

	// Validate checksum file if it exists
	checksumPath := strings.Replace(manifestPath, "manifest.json", "checksum.sha256", 1)
	if err := v.validateChecksumFile(validationCtx, checksumPath, result); err != nil {
		v.logger.Warn("checksum file validation failed", "error", err)
		result.addWarning(checksumPath, "checksum_file_invalid", err.Error())
	}

	// Validate each backup file
	for _, file := range metadata.Files {
		result.CheckedFiles++
		
		if err := v.validateBackupFile(validationCtx, file, result); err != nil {
			v.logger.Error("file validation failed", 
				"file", file.BackupPath, 
				"error", err)
		}
	}

	result.Duration = time.Since(startTime)
	result.Valid = len(v.getErrors(result.Issues)) == 0

	v.logger.Info("integrity validation completed",
		"backup_id", backupID,
		"valid", result.Valid,
		"issues", len(result.Issues),
		"duration", result.Duration)

	return result, nil
}

// ValidateCompleteness ensures all required files are present
func (v *backupValidator) ValidateCompleteness(ctx context.Context, backupID string) (*ValidationResult, error) {
	startTime := time.Now()
	v.logger.Info("starting completeness validation", "backup_id", backupID)
	
	metadata, err := v.backupEngine.GetBackupMetadata(ctx, backupID)
	if err != nil {
		return nil, fmt.Errorf("get backup metadata: %w", err)
	}

	result := &ValidationResult{
		Valid:     true,
		Issues:    []ValidationIssue{},
		Timestamp: startTime,
	}

	// Check required components
	v.validateRequiredComponents(metadata, result)
	
	// Check file sequence integrity for log files
	v.validateLogFileSequence(metadata, result)
	
	// Check data file references
	v.validateDataFileReferences(metadata, result)

	result.Duration = time.Since(startTime)
	result.Valid = len(v.getErrors(result.Issues)) == 0

	v.logger.Info("completeness validation completed",
		"backup_id", backupID,
		"valid", result.Valid,
		"issues", len(result.Issues),
		"duration", result.Duration)

	return result, nil
}

// ValidateRestorability tests if backup can be successfully restored
func (v *backupValidator) ValidateRestorability(ctx context.Context, backupID string) (*ValidationResult, error) {
	startTime := time.Now()
	v.logger.Info("starting restorability validation", "backup_id", backupID)
	
	// First run integrity and completeness checks
	integrityResult, err := v.ValidateIntegrity(ctx, backupID)
	if err != nil {
		return nil, fmt.Errorf("validate integrity: %w", err)
	}

	completenessResult, err := v.ValidateCompleteness(ctx, backupID)
	if err != nil {
		return nil, fmt.Errorf("validate completeness: %w", err)
	}

	// Combine results
	result := &ValidationResult{
		Valid:        integrityResult.Valid && completenessResult.Valid,
		Issues:       append(integrityResult.Issues, completenessResult.Issues...),
		CheckedFiles: integrityResult.CheckedFiles,
		Timestamp:    startTime,
	}

	// Additional restorability checks
	metadata, err := v.backupEngine.GetBackupMetadata(ctx, backupID)
	if err != nil {
		return result, fmt.Errorf("get backup metadata: %w", err)
	}

	// Validate that backup format is supported
	if err := v.validateBackupFormat(metadata, result); err != nil {
		v.logger.Error("backup format validation failed", "error", err)
	}

	// Check if all required Delta Lake components are present
	v.validateDeltaLakeComponents(metadata, result)

	result.Duration = time.Since(startTime)
	result.Valid = len(v.getErrors(result.Issues)) == 0

	v.logger.Info("restorability validation completed",
		"backup_id", backupID,
		"valid", result.Valid,
		"issues", len(result.Issues),
		"duration", result.Duration)

	return result, nil
}

// Helper methods for validation

func (v *backupValidator) validateManifestFile(ctx context.Context, manifestPath string, result *ValidationResult) error {
	exists, err := v.store.Exists(ctx, manifestPath)
	if err != nil {
		result.addError(manifestPath, "existence_check_failed", err.Error())
		return err
	}

	if !exists {
		result.addError(manifestPath, "manifest_missing", "backup manifest file does not exist")
		return fmt.Errorf("manifest file missing")
	}

	// Try to download and parse manifest
	var metadata BackupMetadata
	if err := v.store.GetJSON(ctx, manifestPath, &metadata); err != nil {
		result.addError(manifestPath, "manifest_invalid", fmt.Sprintf("cannot parse manifest: %v", err))
		return err
	}

	return nil
}

func (v *backupValidator) validateChecksumFile(ctx context.Context, checksumPath string, result *ValidationResult) error {
	exists, err := v.store.Exists(ctx, checksumPath)
	if err != nil {
		return err
	}

	if !exists {
		result.addWarning(checksumPath, "checksum_file_missing", "checksum file not found")
		return nil
	}

	// Download and validate checksum format
	data, err := v.store.Download(ctx, checksumPath)
	if err != nil {
		return fmt.Errorf("download checksum file: %w", err)
	}

	checksum := string(data)
	if len(checksum) != 64 { // SHA256 hex length
		result.addWarning(checksumPath, "invalid_checksum_format", "checksum is not valid SHA256 format")
	}

	return nil
}

func (v *backupValidator) validateBackupFile(ctx context.Context, file BackupFile, result *ValidationResult) error {
	// Check file existence
	exists, err := v.store.Exists(ctx, file.BackupPath)
	if err != nil {
		result.addError(file.BackupPath, "existence_check_failed", err.Error())
		return err
	}

	if !exists {
		result.addError(file.BackupPath, "file_missing", "backup file does not exist")
		return nil
	}

	// Validate checksum based on configuration
	if v.config.ValidationMode == "checksum" || v.config.ValidationMode == "full" {
		if err := v.validateFileChecksum(ctx, file, result); err != nil {
			return err
		}
	}

	return nil
}

func (v *backupValidator) validateFileChecksum(ctx context.Context, file BackupFile, result *ValidationResult) error {
	data, err := v.store.Download(ctx, file.BackupPath)
	if err != nil {
		result.addError(file.BackupPath, "download_failed", err.Error())
		return err
	}

	actualChecksum := generateChecksum(data)
	if actualChecksum != file.Checksum {
		result.addError(file.BackupPath, "checksum_mismatch",
			fmt.Sprintf("expected %s, got %s", file.Checksum, actualChecksum))
	}

	// Validate file size
	if int64(len(data)) != file.Size {
		result.addError(file.BackupPath, "size_mismatch",
			fmt.Sprintf("expected %d bytes, got %d bytes", file.Size, len(data)))
	}

	return nil
}

func (v *backupValidator) validateRequiredComponents(metadata *BackupMetadata, result *ValidationResult) {
	hasProtocol := false
	hasMetadata := false
	hasLogFiles := false

	for _, file := range metadata.Files {
		if file.Type == "log" {
			hasLogFiles = true
			
			// Check if this is the initial version with protocol and metadata
			if strings.Contains(file.OriginalPath, "00000000000000000000.json") {
				hasProtocol = true
				hasMetadata = true
			}
		}
	}

	if !hasLogFiles {
		result.addError("", "missing_log_files", "backup does not contain any transaction log files")
	}

	if !hasProtocol {
		result.addError("", "missing_protocol", "backup does not contain Delta Lake protocol information")
	}

	if !hasMetadata {
		result.addError("", "missing_metadata", "backup does not contain table metadata")
	}
}

func (v *backupValidator) validateLogFileSequence(metadata *BackupMetadata, result *ValidationResult) {
	logVersions := make(map[int64]bool)
	maxVersion := int64(-1)

	for _, file := range metadata.Files {
		if file.Type == "log" && strings.Contains(file.OriginalPath, ".json") {
			// Extract version from filename like 00000000000000000123.json
			var version int64
			if n, err := fmt.Sscanf(file.OriginalPath, "%*[^/]/%020d.json", &version); n == 1 && err == nil {
				logVersions[version] = true
				if version > maxVersion {
					maxVersion = version
				}
			}
		}
	}

	// Check for gaps in version sequence
	for v := int64(0); v <= maxVersion; v++ {
		if !logVersions[v] {
			result.addWarning("", "version_gap", 
				fmt.Sprintf("transaction log version %d is missing", v))
		}
	}
}

func (v *backupValidator) validateDataFileReferences(metadata *BackupMetadata, result *ValidationResult) {
	// This is a basic check - in a full implementation, you'd parse the transaction
	// logs to validate that all referenced data files are present
	dataFiles := 0
	for _, file := range metadata.Files {
		if file.Type == "data" {
			dataFiles++
		}
	}

	if dataFiles == 0 {
		result.addWarning("", "no_data_files", "backup contains no data files")
	}
}

func (v *backupValidator) validateBackupFormat(metadata *BackupMetadata, result *ValidationResult) error {
	supportedVersions := []int{1} // Currently only support format version 1
	
	supported := false
	for _, version := range supportedVersions {
		if metadata.FormatVersion == version {
			supported = true
			break
		}
	}

	if !supported {
		result.addError("", "unsupported_format", 
			fmt.Sprintf("backup format version %d is not supported", metadata.FormatVersion))
	}

	return nil
}

func (v *backupValidator) validateDeltaLakeComponents(metadata *BackupMetadata, result *ValidationResult) {
	// Ensure the backup follows Delta Lake structure expectations
	hasDeltaLog := false
	
	for _, file := range metadata.Files {
		if strings.Contains(file.OriginalPath, "_delta_log") {
			hasDeltaLog = true
			break
		}
	}

	if !hasDeltaLog {
		result.addError("", "missing_delta_log", "backup does not contain Delta Lake transaction log")
	}
}

func (v *backupValidator) getManifestPath(metadata *BackupMetadata) string {
	return fmt.Sprintf("backups/%s/%s/%s/manifest.json",
		metadata.Source,
		metadata.Table,
		metadata.CreatedAt.Format("2006-01-02T15-04-05.000Z07-00"))
}

func (v *backupValidator) getErrors(issues []ValidationIssue) []ValidationIssue {
	var errors []ValidationIssue
	for _, issue := range issues {
		if issue.Severity == "error" {
			errors = append(errors, issue)
		}
	}
	return errors
}

// addWarning adds a warning to the validation result
func (r *ValidationResult) addWarning(file, issue, description string) {
	r.Issues = append(r.Issues, ValidationIssue{
		Severity:    "warning",
		File:        file,
		Issue:       issue,
		Description: description,
	})
}