package migration

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentbrain/agentbrain/internal/migration/migrations"
)

func TestEndToEndMigration(t *testing.T) {
	// Create registry and register schema
	registry := NewSchemaRegistry()
	
	// Define a simple state structure for testing
	type TestState struct {
		Source  string            `json:"source"`
		Objects map[string]string `json:"objects"`
	}
	
	err := registry.RegisterSchema(1, &TestState{})
	require.NoError(t, err)

	// Create migration engine
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config := &Config{
		AutoMigrate:         true,
		BackupBeforeMigrate: false, // Skip backup for test
		ValidationMode:      "strict",
		MaxMigrationTime:    30 * time.Second,
	}

	engine := NewMigrationEngine(registry, nil, logger, config)

	// Register the initial migration
	initialMigration := &migrations.V001InitialVersion{}
	err = engine.RegisterMigration(initialMigration)
	require.NoError(t, err)

	// Test migrating legacy unversioned data
	legacyData := map[string]interface{}{
		"source":  "test-source",
		"objects": map[string]interface{}{"table1": "synced"},
	}

	legacyBytes, _ := json.Marshal(legacyData)
	
	// Migrate to version 1
	migratedBytes, err := engine.MigrateData(context.Background(), legacyBytes, 1)
	require.NoError(t, err)

	// Verify result is properly versioned
	var result VersionedState
	err = json.Unmarshal(migratedBytes, &result)
	require.NoError(t, err)
	
	assert.Equal(t, 1, result.Version)
	assert.NotZero(t, result.CreatedAt)
	assert.NotZero(t, result.UpdatedAt)

	// Verify data integrity
	dataMap, ok := result.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "test-source", dataMap["source"])
	assert.NotNil(t, dataMap["objects"])
}

func TestVersionDetection(t *testing.T) {
	// Test legacy data detection
	legacyData := map[string]interface{}{"field": "value"}
	legacyBytes, _ := json.Marshal(legacyData)
	
	version, err := GetCurrentVersion(legacyBytes)
	require.NoError(t, err)
	assert.Equal(t, 1, version)

	// Test versioned data detection
	versionedData := VersionedState{
		Version: 2,
		Data:    map[string]interface{}{"field": "value"},
	}
	versionedBytes, _ := json.Marshal(versionedData)
	
	version, err = GetCurrentVersion(versionedBytes)
	require.NoError(t, err)
	assert.Equal(t, 2, version)
}