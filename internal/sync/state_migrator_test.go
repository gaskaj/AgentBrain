package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentbrain/agentbrain/internal/migration"
)

func TestCurrentSyncStateVersion(t *testing.T) {
	// Test that the constant is set correctly
	assert.Equal(t, 1, CurrentSyncStateVersion)
}

func TestMigrationIntegration(t *testing.T) {
	// Test that we can create the migration types needed
	registry := migration.NewSchemaRegistry()
	
	// Register the SyncState schema
	err := registry.RegisterSchema(CurrentSyncStateVersion, &SyncState{})
	require.NoError(t, err)
	
	// Test latest version
	assert.Equal(t, CurrentSyncStateVersion, registry.GetLatestVersion())
	
	// Test getting schema
	schema, err := registry.GetSchema(CurrentSyncStateVersion)
	require.NoError(t, err)
	assert.IsType(t, &SyncState{}, schema)
}

func TestStateValidation(t *testing.T) {
	// Test that SyncState can be properly validated
	state := &SyncState{
		Source:  "test-source",
		Objects: make(map[string]ObjectState),
	}
	
	registry := migration.NewSchemaRegistry()
	err := registry.RegisterSchema(CurrentSyncStateVersion, &SyncState{})
	require.NoError(t, err)
	
	err = registry.ValidateSchema(CurrentSyncStateVersion, state)
	assert.NoError(t, err)
}