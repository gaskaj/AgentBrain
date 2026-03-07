package delta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// ValidationMode defines the level of checkpoint validation.
type ValidationMode string

const (
	ValidationModeStrict     ValidationMode = "strict"     // Full validation with checksums
	ValidationModePermissive ValidationMode = "permissive" // Basic validation, log errors but don't fail
	ValidationModeDisabled   ValidationMode = "disabled"   // No validation
)

// CheckpointValidator handles validation and recovery of Delta Lake checkpoints.
type CheckpointValidator struct {
	store     S3Store
	logPrefix string
	logger    *slog.Logger
}

// NewCheckpointValidator creates a new checkpoint validator.
func NewCheckpointValidator(store S3Store, logPrefix string, logger *slog.Logger) *CheckpointValidator {
	return &CheckpointValidator{
		store:     store,
		logPrefix: logPrefix,
		logger:    logger,
	}
}

// ValidationResult contains the results of checkpoint validation.
type ValidationResult struct {
	Valid             bool                   `json:"valid"`
	CheckpointVersion int64                  `json:"checkpoint_version"`
	ActionsCount      int                    `json:"actions_count"`
	FilesCount        int                    `json:"files_count"`
	Checksum          string                 `json:"checksum"`
	Errors            []ValidationError      `json:"errors,omitempty"`
	ValidationTime    time.Duration          `json:"validation_time"`
	ValidatedAt       time.Time              `json:"validated_at"`
}

// ValidationError represents a validation error.
type ValidationError struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Severity    string `json:"severity"` // "error", "warning", "info"
}

// ValidateCheckpoint performs comprehensive validation of a checkpoint file.
func (v *CheckpointValidator) ValidateCheckpoint(ctx context.Context, checkpointKey string, expectedSnapshot *Snapshot) error {
	start := time.Now()
	
	result := &ValidationResult{
		CheckpointVersion: expectedSnapshot.Version,
		ValidatedAt:       start,
	}

	v.logger.Info("validating checkpoint", "key", checkpointKey, "version", expectedSnapshot.Version)

	// Read checkpoint file
	data, err := v.store.Download(ctx, checkpointKey)
	if err != nil {
		return fmt.Errorf("read checkpoint file: %w", err)
	}

	// Calculate checksum
	result.Checksum = v.calculateChecksum(data)

	// Parse checkpoint actions
	var actions []Action
	if err := json.Unmarshal(data, &actions); err != nil {
		return fmt.Errorf("parse checkpoint JSON: %w", err)
	}

	result.ActionsCount = len(actions)

	// Validate checkpoint structure
	if err := v.validateCheckpointStructure(actions, result); err != nil {
		return fmt.Errorf("checkpoint structure validation failed: %w", err)
	}

	// Validate against expected snapshot
	if err := v.validateAgainstSnapshot(actions, expectedSnapshot, result); err != nil {
		return fmt.Errorf("snapshot validation failed: %w", err)
	}

	// Validate file integrity
	if err := v.validateFileIntegrity(ctx, actions, result); err != nil {
		v.logger.Warn("file integrity validation failed", "error", err)
		result.Errors = append(result.Errors, ValidationError{
			Type:        "file_integrity",
			Description: fmt.Sprintf("File integrity validation failed: %v", err),
			Severity:    "warning",
		})
	}

	result.ValidationTime = time.Since(start)
	result.Valid = len(result.Errors) == 0

	// Log validation results
	if result.Valid {
		v.logger.Info("checkpoint validation passed",
			"key", checkpointKey,
			"version", result.CheckpointVersion,
			"actions", result.ActionsCount,
			"files", result.FilesCount,
			"duration", result.ValidationTime,
			"checksum", result.Checksum,
		)
	} else {
		v.logger.Error("checkpoint validation failed",
			"key", checkpointKey,
			"version", result.CheckpointVersion,
			"errors", len(result.Errors),
			"duration", result.ValidationTime,
		)
		for _, err := range result.Errors {
			v.logger.Error("validation error", "type", err.Type, "description", err.Description, "severity", err.Severity)
		}
	}

	if !result.Valid {
		return fmt.Errorf("checkpoint validation failed with %d errors", len(result.Errors))
	}

	return nil
}

// validateCheckpointStructure validates the basic structure of the checkpoint.
func (v *CheckpointValidator) validateCheckpointStructure(actions []Action, result *ValidationResult) error {
	var hasProtocol, hasMetadata bool
	var fileCount int

	for _, action := range actions {
		switch {
		case action.Protocol != nil:
			if hasProtocol {
				result.Errors = append(result.Errors, ValidationError{
					Type:        "structure",
					Description: "Multiple protocol actions found",
					Severity:    "error",
				})
			}
			hasProtocol = true
		case action.MetaData != nil:
			if hasMetadata {
				result.Errors = append(result.Errors, ValidationError{
					Type:        "structure", 
					Description: "Multiple metadata actions found",
					Severity:    "error",
				})
			}
			hasMetadata = true
		case action.Add != nil:
			fileCount++
			if action.Add.Path == "" {
				result.Errors = append(result.Errors, ValidationError{
					Type:        "structure",
					Description: "Add action missing path",
					Severity:    "error",
				})
			}
			if action.Add.Size < 0 {
				result.Errors = append(result.Errors, ValidationError{
					Type:        "structure",
					Description: fmt.Sprintf("Add action has negative size: %d", action.Add.Size),
					Severity:    "error",
				})
			}
		case action.Remove != nil:
			result.Errors = append(result.Errors, ValidationError{
				Type:        "structure",
				Description: "Remove action found in checkpoint (checkpoints should only contain active files)",
				Severity:    "warning",
			})
		}
	}

	result.FilesCount = fileCount

	if !hasProtocol {
		result.Errors = append(result.Errors, ValidationError{
			Type:        "structure",
			Description: "No protocol action found",
			Severity:    "error",
		})
	}

	if !hasMetadata {
		result.Errors = append(result.Errors, ValidationError{
			Type:        "structure",
			Description: "No metadata action found",
			Severity:    "error",
		})
	}

	return nil
}

// validateAgainstSnapshot compares checkpoint actions against the expected snapshot.
func (v *CheckpointValidator) validateAgainstSnapshot(actions []Action, expectedSnapshot *Snapshot, result *ValidationResult) error {
	checkpointFiles := make(map[string]*Add)
	
	for _, action := range actions {
		if action.Add != nil {
			checkpointFiles[action.Add.Path] = action.Add
		}
	}

	// Check that all expected files are present
	for path, expectedFile := range expectedSnapshot.Files {
		checkpointFile, exists := checkpointFiles[path]
		if !exists {
			result.Errors = append(result.Errors, ValidationError{
				Type:        "completeness",
				Description: fmt.Sprintf("Expected file missing from checkpoint: %s", path),
				Severity:    "error",
			})
			continue
		}

		// Validate file metadata matches
		if checkpointFile.Size != expectedFile.Size {
			result.Errors = append(result.Errors, ValidationError{
				Type:        "consistency",
				Description: fmt.Sprintf("File size mismatch for %s: expected %d, got %d", path, expectedFile.Size, checkpointFile.Size),
				Severity:    "error",
			})
		}

		if checkpointFile.ModificationTime != expectedFile.ModificationTime {
			result.Errors = append(result.Errors, ValidationError{
				Type:        "consistency",
				Description: fmt.Sprintf("File modification time mismatch for %s", path),
				Severity:    "warning",
			})
		}
	}

	// Check for unexpected files in checkpoint
	for path := range checkpointFiles {
		if _, exists := expectedSnapshot.Files[path]; !exists {
			result.Errors = append(result.Errors, ValidationError{
				Type:        "consistency",
				Description: fmt.Sprintf("Unexpected file in checkpoint: %s", path),
				Severity:    "error",
			})
		}
	}

	return nil
}

// validateFileIntegrity checks if referenced files actually exist in storage.
func (v *CheckpointValidator) validateFileIntegrity(ctx context.Context, actions []Action, result *ValidationResult) error {
	for _, action := range actions {
		if action.Add == nil {
			continue
		}

		// Check if file exists in storage
		exists, err := v.store.Exists(ctx, action.Add.Path)
		if err != nil {
			return fmt.Errorf("check file existence for %s: %w", action.Add.Path, err)
		}

		if !exists {
			result.Errors = append(result.Errors, ValidationError{
				Type:        "integrity",
				Description: fmt.Sprintf("Referenced file does not exist: %s", action.Add.Path),
				Severity:    "error",
			})
		}
	}

	return nil
}

// RecoverFromCheckpoint attempts to recover table state from a valid checkpoint.
func (v *CheckpointValidator) RecoverFromCheckpoint(ctx context.Context, table *DeltaTable, checkpointVersion int64) error {
	v.logger.Info("attempting checkpoint recovery", "version", checkpointVersion)

	checkpointKey := fmt.Sprintf("%s%020d.checkpoint.json", v.logPrefix, checkpointVersion)
	
	// Verify checkpoint exists
	exists, err := v.store.Exists(ctx, checkpointKey)
	if err != nil {
		return fmt.Errorf("check checkpoint existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("checkpoint file does not exist: %s", checkpointKey)
	}

	// Read and validate checkpoint
	data, err := v.store.Download(ctx, checkpointKey)
	if err != nil {
		return fmt.Errorf("read checkpoint file: %w", err)
	}

	var actions []Action
	if err := json.Unmarshal(data, &actions); err != nil {
		return fmt.Errorf("parse checkpoint: %w", err)
	}

	// Basic structure validation
	result := &ValidationResult{CheckpointVersion: checkpointVersion}
	if err := v.validateCheckpointStructure(actions, result); err != nil {
		return fmt.Errorf("checkpoint structure invalid: %w", err)
	}

	// Set valid based on whether we have errors
	result.Valid = len(result.Errors) == 0
	
	if !result.Valid {
		return fmt.Errorf("checkpoint failed validation with %d errors", len(result.Errors))
	}

	v.logger.Info("checkpoint recovery successful",
		"version", checkpointVersion,
		"actions", len(actions),
		"files", result.FilesCount,
	)

	return nil
}

// FindValidCheckpoint searches for the most recent valid checkpoint.
func (v *CheckpointValidator) FindValidCheckpoint(ctx context.Context, table *DeltaTable, maxVersion int64) (int64, error) {
	v.logger.Info("searching for valid checkpoint", "max_version", maxVersion)

	// Start from the most recent possible version and work backwards
	for version := maxVersion; version >= 0; version-- {
		checkpointKey := fmt.Sprintf("%s%020d.checkpoint.json", v.logPrefix, version)
		
		exists, err := v.store.Exists(ctx, checkpointKey)
		if err != nil {
			continue
		}
		if !exists {
			continue
		}

		// Try to validate this checkpoint
		if err := v.validateCheckpointFile(ctx, checkpointKey); err != nil {
			v.logger.Warn("checkpoint validation failed", "version", version, "error", err)
			continue
		}

		v.logger.Info("found valid checkpoint", "version", version)
		return version, nil
	}

	return -1, fmt.Errorf("no valid checkpoint found")
}

// validateCheckpointFile performs basic validation of a checkpoint file.
func (v *CheckpointValidator) validateCheckpointFile(ctx context.Context, checkpointKey string) error {
	data, err := v.store.Download(ctx, checkpointKey)
	if err != nil {
		return fmt.Errorf("read checkpoint: %w", err)
	}

	var actions []Action
	if err := json.Unmarshal(data, &actions); err != nil {
		return fmt.Errorf("parse checkpoint: %w", err)
	}

	result := &ValidationResult{}
	return v.validateCheckpointStructure(actions, result)
}

// calculateChecksum computes SHA256 checksum of checkpoint data.
func (v *CheckpointValidator) calculateChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}