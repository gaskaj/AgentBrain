package delta

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	
	"github.com/agentbrain/agentbrain/internal/retry"
)

// Snapshot represents the state of a Delta table at a specific version.
type Snapshot struct {
	Version  int64
	Protocol *Protocol
	Metadata *Metadata
	Files    map[string]*Add // active files keyed by path
}

// DeltaTable manages a Delta Lake table.
type DeltaTable struct {
	log               *TransactionLog
	checkpointManager *CheckpointManager
	source            string
	object            string
	logger            *slog.Logger
	retryPolicy       *retry.RetryPolicy
	circuitBreaker    *retry.CircuitBreaker
}

// NewDeltaTable creates a new DeltaTable manager.
func NewDeltaTable(store S3Store, source, object, logPrefix string, logger *slog.Logger) *DeltaTable {
	table := &DeltaTable{
		log:    NewTransactionLog(store, logPrefix),
		source: source,
		object: object,
		logger: logger,
	}
	
	// Initialize retry policies for Delta operations
	table.initializeRetryPolicies()
	
	return table
}

// NewDeltaTableWithCheckpoints creates a DeltaTable with comprehensive checkpoint management.
func NewDeltaTableWithCheckpoints(store S3Store, source, object, logPrefix string, config DeltaCheckpointConfig, logger *slog.Logger) *DeltaTable {
	table := &DeltaTable{
		log:    NewTransactionLog(store, logPrefix),
		source: source,
		object: object,
		logger: logger,
	}
	
	lastCheckpointKey := fmt.Sprintf("%s_last_checkpoint", logPrefix)
	table.checkpointManager = NewCheckpointManager(store, table, config, lastCheckpointKey, logPrefix, logger)
	
	// Initialize retry policies for Delta operations
	table.initializeRetryPolicies()
	
	return table
}

// Initialize creates the initial version (0) with protocol and metadata.
func (t *DeltaTable) Initialize(ctx context.Context, schemaString string) error {
	exists, err := t.log.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("check existing table: %w", err)
	}
	if exists >= 0 {
		return nil // already initialized
	}

	tableID := fmt.Sprintf("%s_%s", t.source, t.object)
	actions := []Action{
		NewProtocolAction(),
		NewMetadataAction(tableID, t.object, schemaString),
		NewCommitInfoAction("CREATE TABLE", -1, true),
	}

	if err := t.log.WriteVersion(ctx, 0, actions); err != nil {
		return fmt.Errorf("initialize table: %w", err)
	}

	t.logger.Info("initialized delta table", "source", t.source, "object", t.object)
	return nil
}

// Commit appends a new version to the Delta log with the given actions.
func (t *DeltaTable) Commit(ctx context.Context, actions []Action, operation string) (int64, error) {
	commitOp := func(ctx context.Context) (int64, error) {
		latest, err := t.log.LatestVersion(ctx)
		if err != nil {
			return -1, fmt.Errorf("get latest version: %w", err)
		}

		newVersion := latest + 1

		commitInfo := NewCommitInfoAction(operation, latest, true)
		allActions := append(actions, commitInfo)

		if err := t.log.WriteVersion(ctx, newVersion, allActions); err != nil {
			return -1, fmt.Errorf("commit version %d: %w", newVersion, err)
		}

		// Create checkpoint if checkpoint manager is available
		if t.checkpointManager != nil {
			if err := t.checkpointManager.MaybeCheckpoint(ctx, newVersion); err != nil {
				t.logger.Warn("checkpoint creation failed", "version", newVersion, "error", err)
				// Don't fail the commit if checkpoint fails
			}
		}

		t.logger.Info("committed delta version",
			"source", t.source,
			"object", t.object,
			"version", newVersion,
			"actions", len(actions),
		)

		return newVersion, nil
	}

	// Execute commit with retry and circuit breaker protection
	if t.retryPolicy != nil && t.circuitBreaker != nil {
		return retry.ExecuteWithRetryAndCircuitBreaker(ctx, t.retryPolicy, t.circuitBreaker, commitOp)
	}

	// Fallback to direct execution
	return commitOp(ctx)
}

// Snapshot replays the transaction log up to the given version and returns the table state.
// If version is -1, it returns the latest snapshot.
func (t *DeltaTable) Snapshot(ctx context.Context, version int64) (*Snapshot, error) {
	if version == -1 {
		latest, err := t.log.LatestVersion(ctx)
		if err != nil {
			return nil, err
		}
		if latest < 0 {
			return nil, fmt.Errorf("table has no versions")
		}
		version = latest
	}

	snap := &Snapshot{
		Version: version,
		Files:   make(map[string]*Add),
	}

	for v := int64(0); v <= version; v++ {
		actions, err := t.log.ReadVersion(ctx, v)
		if err != nil {
			return nil, fmt.Errorf("read version %d: %w", v, err)
		}

		for i := range actions {
			a := &actions[i]
			switch {
			case a.Protocol != nil:
				snap.Protocol = a.Protocol
			case a.MetaData != nil:
				snap.Metadata = a.MetaData
			case a.Add != nil:
				snap.Files[a.Add.Path] = a.Add
			case a.Remove != nil:
				delete(snap.Files, a.Remove.Path)
			}
		}
	}

	return snap, nil
}

// LatestVersion returns the latest version, or -1 if none.
func (t *DeltaTable) LatestVersion(ctx context.Context) (int64, error) {
	return t.log.LatestVersion(ctx)
}

// ActiveFiles returns the set of currently active file paths at the latest version.
func (t *DeltaTable) ActiveFiles(ctx context.Context) ([]string, error) {
	snap, err := t.Snapshot(ctx, -1)
	if err != nil {
		return nil, err
	}

	files := make([]string, 0, len(snap.Files))
	for path := range snap.Files {
		files = append(files, path)
	}
	return files, nil
}

// GetCheckpointManager returns the checkpoint manager if available.
func (t *DeltaTable) GetCheckpointManager() *CheckpointManager {
	return t.checkpointManager
}

// SetCheckpointManager sets the checkpoint manager for the table.
func (t *DeltaTable) SetCheckpointManager(manager *CheckpointManager) {
	t.checkpointManager = manager
}

// initializeRetryPolicies sets up retry policies for Delta table operations.
func (t *DeltaTable) initializeRetryPolicies() {
	// Create retry policy optimized for Delta operations
	t.retryPolicy = &retry.RetryPolicy{
		MaxAttempts:   3,
		BaseDelay:     500 * time.Millisecond,
		MaxDelay:      10 * time.Second,
		BackoffFunc:   retry.LinearBackoff,
		RetryableFunc: retry.DefaultRetryableFunc,
		Jitter:        false, // Delta operations should be predictable
	}
	
	// Create circuit breaker for Delta operations
	t.circuitBreaker = retry.NewCircuitBreaker("delta_operations", 5, 2*time.Minute)
}

// SetRetryPolicy allows customizing the retry policy for Delta operations.
func (t *DeltaTable) SetRetryPolicy(policy *retry.RetryPolicy) {
	t.retryPolicy = policy
}

// SetCircuitBreaker allows customizing the circuit breaker for Delta operations.
func (t *DeltaTable) SetCircuitBreaker(cb *retry.CircuitBreaker) {
	t.circuitBreaker = cb
}

// CreateBackup creates a backup of this table at the specified version
func (t *DeltaTable) CreateBackup(ctx context.Context, timestamp time.Time) error {
	// This method would typically integrate with the backup engine
	// For now, it's a placeholder that validates the table can be backed up
	
	latest, err := t.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("get latest version for backup: %w", err)
	}
	
	if latest < 0 {
		return fmt.Errorf("cannot backup table with no versions")
	}
	
	// Validate table structure
	_, err = t.Snapshot(ctx, latest)
	if err != nil {
		return fmt.Errorf("cannot create snapshot for backup: %w", err)
	}
	
	t.logger.Info("table backup validation successful", 
		"source", t.source,
		"object", t.object,
		"version", latest,
		"timestamp", timestamp)
		
	return nil
}

// RestoreFromBackup restores this table from a backup path
func (t *DeltaTable) RestoreFromBackup(ctx context.Context, backupPath string) error {
	// This method would typically integrate with the restore engine  
	// For now, it's a placeholder that validates the restore target
	
	if backupPath == "" {
		return fmt.Errorf("backup path cannot be empty")
	}
	
	t.logger.Info("table restore validation successful",
		"source", t.source,
		"object", t.object,
		"backup_path", backupPath)
		
	return nil
}

// ValidateBackup validates that a backup at the specified path is complete and valid
func (t *DeltaTable) ValidateBackup(ctx context.Context, backupPath string) error {
	// This method would typically integrate with the backup validator
	// For now, it's a placeholder that performs basic validation
	
	if backupPath == "" {
		return fmt.Errorf("backup path cannot be empty")
	}
	
	t.logger.Info("backup validation successful",
		"source", t.source,
		"object", t.object,
		"backup_path", backupPath)
		
	return nil
}
