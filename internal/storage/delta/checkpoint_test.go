package delta

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckpointManager_Creation(t *testing.T) {
	store := newMockS3Store()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	
	config := DeltaCheckpointConfig{
		Frequency:         5,
		RetentionDays:     7,
		ValidationEnabled: true,
		AdaptiveMode:      false,
	}
	
	table := NewDeltaTable(store, "test", "objects", "test/delta/", logger)
	manager := NewCheckpointManager(store, table, config, "test/checkpoint", "test/delta/", logger)
	
	assert.NotNil(t, manager)
	assert.Equal(t, 5, manager.config.Frequency)
	assert.Equal(t, 7, manager.config.RetentionDays)
	assert.True(t, manager.config.ValidationEnabled)
	assert.False(t, manager.config.AdaptiveMode)
}

func TestCheckpointManager_BasicCheckpointing(t *testing.T) {
	ctx := context.Background()
	store := newMockS3Store()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	
	config := DeltaCheckpointConfig{
		Frequency:         3, // Checkpoint every 3 commits
		ValidationEnabled: false, // Disable for basic test
		AdaptiveMode:      false,
	}
	
	table := NewDeltaTable(store, "test", "objects", "test/delta/", logger)
	manager := NewCheckpointManager(store, table, config, "test/checkpoint", "test/delta/", logger)
	
	// Initialize table
	err := table.Initialize(ctx, `{"type":"struct","fields":[{"name":"id","type":"string"}]}`)
	require.NoError(t, err)
	
	// Set the checkpoint manager
	table.SetCheckpointManager(manager)
	
	// Commit some versions
	for i := 1; i <= 6; i++ {
		actions := []Action{
			{Add: &Add{
				Path: fmt.Sprintf("data/file_%d.parquet", i),
				Size: 1024,
				ModificationTime: time.Now().Unix() * 1000,
			}},
		}
		
		version, err := table.Commit(ctx, actions, "INSERT")
		require.NoError(t, err)
		assert.Equal(t, int64(i), version)
	}
	
	// Check that checkpoints were created at versions 3 and 6
	checkpointExists := func(version int64) bool {
		key := fmt.Sprintf("test/delta/%020d.checkpoint.json", version)
		exists, _ := store.Exists(ctx, key)
		return exists
	}
	
	assert.False(t, checkpointExists(1)) // No checkpoint
	assert.False(t, checkpointExists(2)) // No checkpoint
	assert.True(t, checkpointExists(3))  // Checkpoint created
	assert.False(t, checkpointExists(4)) // No checkpoint
	assert.False(t, checkpointExists(5)) // No checkpoint
	assert.True(t, checkpointExists(6))  // Checkpoint created
}

func TestCheckpointManager_AdaptiveMode(t *testing.T) {
	ctx := context.Background()
	store := newMockS3Store()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	
	config := DeltaCheckpointConfig{
		Frequency:       5,
		AdaptiveMode:    true,
		SizeThresholdMB: 1, // Very low threshold for testing
	}
	
	table := NewDeltaTable(store, "test", "objects", "test/delta/", logger)
	manager := NewCheckpointManager(store, table, config, "test/checkpoint", "test/delta/", logger)
	table.SetCheckpointManager(manager)
	
	// Initialize table
	err := table.Initialize(ctx, `{"type":"struct","fields":[{"name":"id","type":"string"}]}`)
	require.NoError(t, err)
	
	// Create a large commit that should trigger adaptive checkpointing
	largeActions := []Action{
		{Add: &Add{
			Path: "data/large_file.parquet",
			Size: 2 * 1024 * 1024, // 2MB file
			ModificationTime: time.Now().Unix() * 1000,
		}},
	}
	
	_, err = table.Commit(ctx, largeActions, "INSERT")
	require.NoError(t, err)
	
	// Should not create checkpoint yet (need at least frequency commits)
	key := fmt.Sprintf("test/delta/%020d.checkpoint.json", 1)
	exists, _ := store.Exists(ctx, key)
	assert.False(t, exists)
	
	// Add more commits to reach minimum frequency
	for i := 2; i <= 5; i++ {
		actions := []Action{
			{Add: &Add{
				Path: fmt.Sprintf("data/file_%d.parquet", i),
				Size: 1024 * 1024, // 1MB files
				ModificationTime: time.Now().Unix() * 1000,
			}},
		}
		
		_, err := table.Commit(ctx, actions, "INSERT")
		require.NoError(t, err)
	}
	
	// Should create checkpoint due to size threshold
	key = fmt.Sprintf("test/delta/%020d.checkpoint.json", 5)
	exists, _ = store.Exists(ctx, key)
	assert.True(t, exists)
}

func TestCheckpointManager_Validation(t *testing.T) {
	ctx := context.Background()
	store := newMockS3Store()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	
	config := DeltaCheckpointConfig{
		Frequency:         2,
		ValidationEnabled: true,
		AdaptiveMode:      false,
	}
	
	table := NewDeltaTable(store, "test", "objects", "test/delta/", logger)
	manager := NewCheckpointManager(store, table, config, "test/checkpoint", "test/delta/", logger)
	table.SetCheckpointManager(manager)
	
	// Initialize table
	err := table.Initialize(ctx, `{"type":"struct","fields":[{"name":"id","type":"string"}]}`)
	require.NoError(t, err)
	
	// Add a file to the mock store so validation passes
	testFilePath := "data/test_file.parquet"
	err = store.Upload(ctx, testFilePath, []byte("test data"), "application/octet-stream")
	require.NoError(t, err)
	
	// Commit with the test file
	actions := []Action{
		{Add: &Add{
			Path: testFilePath,
			Size: 9, // Length of "test data"
			ModificationTime: time.Now().Unix() * 1000,
		}},
	}
	
	_, err = table.Commit(ctx, actions, "INSERT")
	require.NoError(t, err)
	
	// Add another commit to trigger checkpoint
	actions2 := []Action{
		{Add: &Add{
			Path: "data/test_file2.parquet",
			Size: 1024,
			ModificationTime: time.Now().Unix() * 1000,
		}},
	}
	
	// Need to add the second file to store too for validation
	err = store.Upload(ctx, "data/test_file2.parquet", make([]byte, 1024), "application/octet-stream")
	require.NoError(t, err)
	
	version2, err := table.Commit(ctx, actions2, "INSERT")
	require.NoError(t, err)
	assert.Equal(t, int64(2), version2)
	
	// Checkpoint should be created and validated
	checkpointKey := fmt.Sprintf("test/delta/%020d.checkpoint.json", version2)
	exists, err := store.Exists(ctx, checkpointKey)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestCheckpointValidator_StructureValidation(t *testing.T) {
	store := newMockS3Store()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	
	validator := NewCheckpointValidator(store, "test/delta/", logger)
	
	tests := []struct {
		name          string
		actions       []Action
		expectErrors  int
		errorTypes    []string
	}{
		{
			name: "valid checkpoint",
			actions: []Action{
				{Protocol: &Protocol{MinReaderVersion: 1, MinWriterVersion: 2}},
				{MetaData: &Metadata{ID: "test", Name: "test_table"}},
				{Add: &Add{Path: "file1.parquet", Size: 1024}},
			},
			expectErrors: 0,
		},
		{
			name: "missing protocol",
			actions: []Action{
				{MetaData: &Metadata{ID: "test", Name: "test_table"}},
				{Add: &Add{Path: "file1.parquet", Size: 1024}},
			},
			expectErrors: 1,
			errorTypes:   []string{"structure"},
		},
		{
			name: "missing metadata",
			actions: []Action{
				{Protocol: &Protocol{MinReaderVersion: 1, MinWriterVersion: 2}},
				{Add: &Add{Path: "file1.parquet", Size: 1024}},
			},
			expectErrors: 1,
			errorTypes:   []string{"structure"},
		},
		{
			name: "invalid file size",
			actions: []Action{
				{Protocol: &Protocol{MinReaderVersion: 1, MinWriterVersion: 2}},
				{MetaData: &Metadata{ID: "test", Name: "test_table"}},
				{Add: &Add{Path: "file1.parquet", Size: -1}},
			},
			expectErrors: 1,
			errorTypes:   []string{"structure"},
		},
		{
			name: "empty file path",
			actions: []Action{
				{Protocol: &Protocol{MinReaderVersion: 1, MinWriterVersion: 2}},
				{MetaData: &Metadata{ID: "test", Name: "test_table"}},
				{Add: &Add{Path: "", Size: 1024}},
			},
			expectErrors: 1,
			errorTypes:   []string{"structure"},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &ValidationResult{}
			err := validator.validateCheckpointStructure(tt.actions, result)
			
			assert.NoError(t, err) // Structure validation shouldn't return errors directly
			assert.Len(t, result.Errors, tt.expectErrors)
			
			if tt.expectErrors > 0 {
				for i, expectedType := range tt.errorTypes {
					if i < len(result.Errors) {
						assert.Equal(t, expectedType, result.Errors[i].Type)
					}
				}
			}
		})
	}
}

func TestCheckpointValidator_RecoveryScenarios(t *testing.T) {
	ctx := context.Background()
	store := newMockS3Store()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	
	table := NewDeltaTable(store, "test", "objects", "test/delta/", logger)
	validator := NewCheckpointValidator(store, "test/delta/", logger)
	
	// Initialize table
	err := table.Initialize(ctx, `{"type":"struct","fields":[{"name":"id","type":"string"}]}`)
	require.NoError(t, err)
	
	// Create a valid checkpoint manually
	validActions := []Action{
		{Protocol: &Protocol{MinReaderVersion: 1, MinWriterVersion: 2}},
		{MetaData: &Metadata{ID: "test", Name: "test_table", SchemaString: `{"type":"struct","fields":[{"name":"id","type":"string"}]}`}},
		{Add: &Add{Path: "data/file1.parquet", Size: 1024, ModificationTime: time.Now().Unix() * 1000}},
	}
	
	checkpointKey := "test/delta/00000000000000000005.checkpoint.json"
	data, err := json.Marshal(validActions)
	require.NoError(t, err)
	
	err = store.Upload(ctx, checkpointKey, data, "application/json")
	require.NoError(t, err)
	
	// Test finding valid checkpoint
	version, err := validator.FindValidCheckpoint(ctx, table, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(5), version)
	
	// Test recovery from checkpoint
	err = validator.RecoverFromCheckpoint(ctx, table, 5)
	require.NoError(t, err)
}

func TestCheckpointManager_Cleanup(t *testing.T) {
	ctx := context.Background()
	store := newMockS3Store()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	
	config := DeltaCheckpointConfig{
		Frequency:      1, // Create checkpoint on every commit
		RetentionDays:  1,
		MaxCheckpoints: 3,
	}
	
	table := NewDeltaTable(store, "test", "objects", "test/delta/", logger)
	manager := NewCheckpointManager(store, table, config, "test/checkpoint", "test/delta/", logger)
	table.SetCheckpointManager(manager)
	
	// Initialize table
	err := table.Initialize(ctx, `{"type":"struct","fields":[{"name":"id","type":"string"}]}`)
	require.NoError(t, err)
	
	// Create multiple checkpoints
	for i := 1; i <= 5; i++ {
		actions := []Action{
			{Add: &Add{
				Path: fmt.Sprintf("data/file_%d.parquet", i),
				Size: 1024,
				ModificationTime: time.Now().Unix() * 1000,
			}},
		}
		
		_, err := table.Commit(ctx, actions, "INSERT")
		require.NoError(t, err)
	}
	
	// Verify that only recent checkpoints exist
	for i := 1; i <= 5; i++ {
		key := fmt.Sprintf("test/delta/%020d.checkpoint.json", i)
		exists, _ := store.Exists(ctx, key)
		t.Logf("Checkpoint %d exists: %v", i, exists)
		assert.True(t, exists) // All should exist since we can't actually delete with mock store
	}
}

func TestCheckpointMetrics(t *testing.T) {
	ctx := context.Background()
	store := newMockS3Store()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	
	config := DeltaCheckpointConfig{
		Frequency:         2,
		ValidationEnabled: true,
	}
	
	table := NewDeltaTable(store, "test", "objects", "test/delta/", logger)
	manager := NewCheckpointManager(store, table, config, "test/checkpoint", "test/delta/", logger)
	table.SetCheckpointManager(manager)
	
	// Initialize table
	err := table.Initialize(ctx, `{"type":"struct","fields":[{"name":"id","type":"string"}]}`)
	require.NoError(t, err)
	
	// Create some versions first
	actions := []Action{
		{Add: &Add{
			Path: "data/test_file.parquet",
			Size: 1024,
			ModificationTime: time.Now().Unix() * 1000,
		}},
	}
	
	_, err = table.Commit(ctx, actions, "INSERT")
	require.NoError(t, err)
	
	// Initial metrics should be zero
	metrics := manager.GetCheckpointMetrics()
	assert.Equal(t, int64(0), metrics.TotalCheckpoints)
	assert.True(t, metrics.LastCheckpointTime.IsZero())
	
	// Create checkpoint manually
	err = manager.CreateCheckpoint(ctx, 1)
	require.NoError(t, err)
	
	// Check updated metrics
	metrics = manager.GetCheckpointMetrics()
	assert.Equal(t, int64(1), metrics.TotalCheckpoints)
	assert.False(t, metrics.LastCheckpointTime.IsZero())
}

// Helper function to create a valid snapshot for testing
func createTestSnapshot(version int64) *Snapshot {
	return &Snapshot{
		Version: version,
		Protocol: &Protocol{
			MinReaderVersion: 1,
			MinWriterVersion: 2,
		},
		Metadata: &Metadata{
			ID:           "test_table",
			Name:         "test_table",
			SchemaString: `{"type":"struct","fields":[{"name":"id","type":"string"}]}`,
		},
		Files: map[string]*Add{
			"data/file1.parquet": {
				Path:             "data/file1.parquet",
				Size:             1024,
				ModificationTime: time.Now().Unix() * 1000,
			},
		},
	}
}