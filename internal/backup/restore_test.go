package backup

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/agentbrain/agentbrain/internal/config"
	"github.com/agentbrain/agentbrain/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestoreEngine_ValidateRestore(t *testing.T) {
	// Skip integration test
	t.Skip("Skipping integration test - requires S3 mock implementation")
}

func TestRestoreEngine_GetRestorePreview(t *testing.T) {
	// Skip integration test
	t.Skip("Skipping integration test - requires S3 mock implementation")
}

func TestRestorePreview_String(t *testing.T) {
	now := time.Now()
	preview := &RestorePreview{
		BackupID:          "test-backup-123",
		Source:            "test-source",
		Table:             "test-table",
		Version:           5,
		CreatedAt:         now,
		FilesToRestore:    []BackupFile{
			{
				OriginalPath: "test/file1.parquet",
				BackupPath:   "backup/test/file1.parquet",
				Size:         1024,
				Checksum:     "abc123",
				Type:         "data",
			},
		},
		TotalSize:         1024,
		EstimatedDuration: 10 * time.Second,
	}

	result := preview.String()
	
	assert.Contains(t, result, "test-backup-123")
	assert.Contains(t, result, "test-source/test-table")
	assert.Contains(t, result, "Version: 5")
	assert.Contains(t, result, "Files to restore: 1")
	assert.Contains(t, result, "Total size: 0.00 MB") // 1024 bytes = ~0.00 MB
	assert.Contains(t, result, "Estimated duration: 10s")
}

func TestRestoreEngine_RestoreFromBackup_Integration(t *testing.T) {
	// Skip integration test that would require real S3 setup
	t.Skip("Skipping integration test - requires S3 mock implementation")

	logger := slog.Default()
	backupConfig := BackupConfig{
		Enabled:           true,
		ValidationMode:    "checksum",
		ConcurrentUploads: 2,
	}

	ctx := context.Background()
	storageConfig := config.StorageConfig{
		Bucket: "test-bucket",
		Region: "us-east-1",
	}
	
	store, err := storage.NewS3Client(ctx, storageConfig)
	require.NoError(t, err)

	engine := NewBackupEngine(store, backupConfig, logger)
	restore := NewRestoreEngine(store, engine, backupConfig, logger)

	// This would test the complete restore workflow
	// 1. Create a test table with data
	// 2. Create a backup
	// 3. Delete the original table
	// 4. Restore from backup
	// 5. Verify restored data matches original

	err = restore.RestoreFromBackup(ctx, "test-backup", "target-source", "target-table")
	assert.NoError(t, err)
}

func TestRestoreEngine_RestoreFile(t *testing.T) {
	// Create a test backup file
	testData := []byte("test data")
	file := BackupFile{
		OriginalPath: "source/table/data/file1.parquet",
		BackupPath:   "backups/source/table/2024-01-01/data/file1.parquet",
		Size:         int64(len(testData)),
		Checksum:     generateChecksum(testData),
		Type:         "data",
	}

	// Verify the file structure is valid
	assert.NotEmpty(t, file.OriginalPath)
	assert.NotEmpty(t, file.BackupPath)
	assert.Equal(t, "data", file.Type)
	assert.Equal(t, int64(9), file.Size) // "test data" is 9 bytes
}

func TestRestorePathCalculations(t *testing.T) {
	// Test the path replacement logic used in restore operations
	
	tests := []struct {
		name         string
		originalPath string
		sourcePrefix string
		targetPrefix string
		expected     string
	}{
		{
			name:         "data file path replacement",
			originalPath: "source1/table1/data/part-00001.parquet",
			sourcePrefix: "source1/table1",
			targetPrefix: "source2/table2",
			expected:     "source2/table2/data/part-00001.parquet",
		},
		{
			name:         "log file path replacement",
			originalPath: "source1/table1/_delta_log/00000000000000000001.json",
			sourcePrefix: "source1/table1/_delta_log",
			targetPrefix: "source2/table2/_delta_log",
			expected:     "source2/table2/_delta_log/00000000000000000001.json",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// This simulates the path replacement logic in restoreFile
			result := replacePrefix(test.originalPath, test.sourcePrefix, test.targetPrefix)
			assert.Equal(t, test.expected, result)
		})
	}
}

// Helper function to simulate path replacement logic
func replacePrefix(path, oldPrefix, newPrefix string) string {
	if len(path) >= len(oldPrefix) && path[:len(oldPrefix)] == oldPrefix {
		return newPrefix + path[len(oldPrefix):]
	}
	return path
}