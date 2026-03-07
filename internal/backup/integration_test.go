// +build integration

package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/agentbrain/agentbrain/internal/config"
	"github.com/agentbrain/agentbrain/internal/storage"
	"github.com/agentbrain/agentbrain/internal/storage/delta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These integration tests require a running S3-compatible service
// Run with: go test -tags=integration

func TestBackupRestoreWorkflow_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Setup test environment
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Use environment variables for S3 configuration
	storageConfig := config.StorageConfig{
		Bucket:   getEnvOrDefault("TEST_S3_BUCKET", "test-backup-bucket"),
		Region:   getEnvOrDefault("TEST_S3_REGION", "us-east-1"),
		Endpoint: getEnvOrDefault("TEST_S3_ENDPOINT", "http://localhost:4566"), // LocalStack
	}

	backupConfig := BackupConfig{
		Enabled:           true,
		DestinationBucket: storageConfig.Bucket,
		DestinationRegion: storageConfig.Region,
		ValidationMode:    "checksum",
		ConcurrentUploads: 2,
		RetentionDays:     7,
	}

	// Create S3 client
	store, err := storage.NewS3ClientWithCredentials(ctx, storageConfig, "test", "test", "")
	require.NoError(t, err)

	// Create backup manager
	manager := NewManager(store, config.BackupConfig{
		Enabled:           backupConfig.Enabled,
		DestinationBucket: backupConfig.DestinationBucket,
		DestinationRegion: backupConfig.DestinationRegion,
		ValidationMode:    backupConfig.ValidationMode,
		ConcurrentUploads: backupConfig.ConcurrentUploads,
		RetentionDays:     backupConfig.RetentionDays,
	}, logger)

	// Test data
	sourceTable := "test-source"
	tableName := "integration-test-table"

	// Step 1: Create a test Delta table with some data
	t.Run("CreateTestTable", func(t *testing.T) {
		logPrefix := fmt.Sprintf("%s/%s/_delta_log", sourceTable, tableName)
		table := delta.NewDeltaTable(store, sourceTable, tableName, logPrefix, logger)

		// Initialize table
		err := table.Initialize(ctx, `{"type":"struct","fields":[{"name":"id","type":"integer","nullable":false},{"name":"name","type":"string","nullable":true}]}`)
		require.NoError(t, err)

		// Add some data files (simulate)
		addAction := &delta.Add{
			Path:             fmt.Sprintf("%s/%s/data/part-00001.parquet", sourceTable, tableName),
			Size:             1024,
			ModificationTime: time.Now().UnixMilli(),
		}

		// Upload a dummy parquet file
		dummyData := make([]byte, 1024)
		for i := range dummyData {
			dummyData[i] = byte(i % 256)
		}
		err = store.Upload(ctx, addAction.Path, dummyData, "application/octet-stream")
		require.NoError(t, err)

		// Commit the add action
		_, err = table.Commit(ctx, []delta.Action{{Add: addAction}}, "INSERT")
		require.NoError(t, err)

		logger.Info("test table created", "source", sourceTable, "table", tableName)
	})

	var backupID string

	// Step 2: Create a backup of the table
	t.Run("CreateBackup", func(t *testing.T) {
		metadata, err := manager.Engine().CreateBackup(ctx, sourceTable, tableName, -1)
		require.NoError(t, err)
		require.NotNil(t, metadata)

		backupID = metadata.BackupID
		assert.Equal(t, sourceTable, metadata.Source)
		assert.Equal(t, tableName, metadata.Table)
		assert.Equal(t, BackupStatusCompleted, metadata.Status)
		assert.Greater(t, metadata.TotalFiles, 0)
		assert.Greater(t, metadata.TotalSize, int64(0))

		logger.Info("backup created", "backup_id", backupID, "files", metadata.TotalFiles)
	})

	// Step 3: Validate the backup
	t.Run("ValidateBackup", func(t *testing.T) {
		result, err := manager.Validator().ValidateIntegrity(ctx, backupID)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.True(t, result.Valid, "backup should be valid")
		assert.Greater(t, result.CheckedFiles, 0)
		
		if !result.Valid {
			for _, issue := range result.Issues {
				logger.Error("validation issue", "severity", issue.Severity, "issue", issue.Issue, "description", issue.Description)
			}
		}

		logger.Info("backup validation completed", "valid", result.Valid, "issues", len(result.Issues))
	})

	// Step 4: Test backup listing
	t.Run("ListBackups", func(t *testing.T) {
		backups, err := manager.Engine().ListBackups(ctx, sourceTable, tableName)
		require.NoError(t, err)
		require.Len(t, backups, 1)

		backup := backups[0]
		assert.Equal(t, backupID, backup.BackupID)
		assert.Equal(t, sourceTable, backup.Source)
		assert.Equal(t, tableName, backup.Table)
	})

	// Step 5: Get restore preview
	t.Run("RestorePreview", func(t *testing.T) {
		preview, err := manager.Restore().GetRestorePreview(ctx, backupID)
		require.NoError(t, err)
		require.NotNil(t, preview)

		assert.Equal(t, backupID, preview.BackupID)
		assert.Equal(t, sourceTable, preview.Source)
		assert.Equal(t, tableName, preview.Table)
		assert.Greater(t, len(preview.FilesToRestore), 0)
		assert.Greater(t, preview.TotalSize, int64(0))

		logger.Info("restore preview", "files", len(preview.FilesToRestore), "size", preview.TotalSize)
	})

	// Step 6: Restore to a different location
	t.Run("RestoreBackup", func(t *testing.T) {
		targetSource := "restored-source"
		targetTable := "restored-table"

		err := manager.Restore().RestoreFromBackup(ctx, backupID, targetSource, targetTable)
		require.NoError(t, err)

		// Verify restored table exists and has correct structure
		logPrefix := fmt.Sprintf("%s/%s/_delta_log", targetSource, targetTable)
		restoredTable := delta.NewDeltaTable(store, targetSource, targetTable, logPrefix, logger)

		// Check that the table has data
		snapshot, err := restoredTable.Snapshot(ctx, -1)
		require.NoError(t, err)
		require.NotNil(t, snapshot)

		assert.Greater(t, len(snapshot.Files), 0, "restored table should have data files")

		logger.Info("restore completed", "target", fmt.Sprintf("%s/%s", targetSource, targetTable))
	})

	// Step 7: Test backup scheduler (brief test)
	t.Run("BackupScheduler", func(t *testing.T) {
		scheduler := manager.Scheduler()

		// Schedule a backup
		err := scheduler.ScheduleBackup(sourceTable, tableName, "@every 1h")
		require.NoError(t, err)

		// Check scheduled backups
		scheduled := scheduler.GetScheduledBackups()
		assert.Len(t, scheduled, 1)

		backup := scheduled[0]
		assert.Equal(t, sourceTable, backup.Source)
		assert.Equal(t, tableName, backup.Table)
		assert.Equal(t, "@every 1h", backup.Schedule)
		assert.True(t, backup.Enabled)

		// Unschedule
		err = scheduler.UnscheduleBackup(sourceTable, tableName)
		require.NoError(t, err)

		scheduled = scheduler.GetScheduledBackups()
		assert.Len(t, scheduled, 0)
	})

	// Step 8: Clean up - delete the backup
	t.Run("DeleteBackup", func(t *testing.T) {
		err := manager.Engine().DeleteBackup(ctx, backupID)
		require.NoError(t, err)

		// Verify backup is deleted
		backups, err := manager.Engine().ListBackups(ctx, sourceTable, tableName)
		require.NoError(t, err)
		assert.Len(t, backups, 0, "backup should be deleted")

		logger.Info("backup deleted", "backup_id", backupID)
	})
}

func TestBackupManager_StartStop_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	storageConfig := config.StorageConfig{
		Bucket:   getEnvOrDefault("TEST_S3_BUCKET", "test-backup-bucket"),
		Region:   getEnvOrDefault("TEST_S3_REGION", "us-east-1"),
		Endpoint: getEnvOrDefault("TEST_S3_ENDPOINT", "http://localhost:4566"),
	}

	backupConfig := config.BackupConfig{
		Enabled:       true,
		Schedule:      "@every 10s",
		RetentionDays: 1,
	}

	store, err := storage.NewS3ClientWithCredentials(ctx, storageConfig, "test", "test", "")
	require.NoError(t, err)

	manager := NewManager(store, backupConfig, logger)

	// Test starting the manager
	err = manager.Start(ctx)
	require.NoError(t, err)

	assert.True(t, manager.IsEnabled())

	// Let it run briefly
	time.Sleep(2 * time.Second)

	// Test stopping the manager
	err = manager.Stop(ctx)
	require.NoError(t, err)
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Helper function to create test data
func createTestParquetData(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	return data
}

func TestBackupRetention_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// This test would verify that old backups are cleaned up according to retention policy
	// Implementation would involve:
	// 1. Create several backups with different timestamps
	// 2. Set a short retention period
	// 3. Run cleanup
	// 4. Verify old backups are deleted

	t.Skip("Retention test requires time manipulation - implement with mock time")
}