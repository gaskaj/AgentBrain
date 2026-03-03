package delta

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeltaTable_InitializeAndSnapshot(t *testing.T) {
	store := newMockS3Store()
	logger := slog.Default()
	table := NewDeltaTable(store, "sf", "Account", "data/sf/Account/_delta_log/", logger)

	ctx := context.Background()

	// Initialize
	err := table.Initialize(ctx, `{"type":"struct","fields":[]}`)
	require.NoError(t, err)

	// Should be idempotent
	err = table.Initialize(ctx, `{"type":"struct","fields":[]}`)
	require.NoError(t, err)

	// Get snapshot
	snap, err := table.Snapshot(ctx, -1)
	require.NoError(t, err)
	assert.Equal(t, int64(0), snap.Version)
	assert.NotNil(t, snap.Protocol)
	assert.NotNil(t, snap.Metadata)
	assert.Empty(t, snap.Files)
}

func TestDeltaTable_CommitAndSnapshot(t *testing.T) {
	store := newMockS3Store()
	logger := slog.Default()
	table := NewDeltaTable(store, "sf", "Account", "data/sf/Account/_delta_log/", logger)

	ctx := context.Background()

	// Initialize
	err := table.Initialize(ctx, `{"type":"struct","fields":[]}`)
	require.NoError(t, err)

	// Commit with add actions
	actions := []Action{
		NewAddAction("part-00000-abc.snappy.parquet", 1024, `{"numRecords":100}`),
		NewAddAction("part-00001-def.snappy.parquet", 2048, `{"numRecords":200}`),
	}
	v1, err := table.Commit(ctx, actions, "WRITE")
	require.NoError(t, err)
	assert.Equal(t, int64(1), v1)

	// Snapshot should show both files
	snap, err := table.Snapshot(ctx, -1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), snap.Version)
	assert.Len(t, snap.Files, 2)
}

func TestDeltaTable_TimeTravel(t *testing.T) {
	store := newMockS3Store()
	logger := slog.Default()
	table := NewDeltaTable(store, "sf", "Account", "data/sf/Account/_delta_log/", logger)

	ctx := context.Background()

	// v0: init
	err := table.Initialize(ctx, `{"type":"struct","fields":[]}`)
	require.NoError(t, err)

	// v1: add file A
	_, err = table.Commit(ctx, []Action{
		NewAddAction("fileA.parquet", 100, ""),
	}, "WRITE")
	require.NoError(t, err)

	// v2: add file B, remove file A
	_, err = table.Commit(ctx, []Action{
		NewAddAction("fileB.parquet", 200, ""),
		NewRemoveAction("fileA.parquet"),
	}, "OVERWRITE")
	require.NoError(t, err)

	// Snapshot at v1 should have only file A
	snapV1, err := table.Snapshot(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, snapV1.Files, 1)
	assert.Contains(t, snapV1.Files, "fileA.parquet")

	// Snapshot at v2 should have only file B
	snapV2, err := table.Snapshot(ctx, 2)
	require.NoError(t, err)
	assert.Len(t, snapV2.Files, 1)
	assert.Contains(t, snapV2.Files, "fileB.parquet")
}

func TestDeltaTable_ActiveFiles(t *testing.T) {
	store := newMockS3Store()
	logger := slog.Default()
	table := NewDeltaTable(store, "sf", "Account", "data/sf/Account/_delta_log/", logger)

	ctx := context.Background()

	err := table.Initialize(ctx, `{}`)
	require.NoError(t, err)

	_, err = table.Commit(ctx, []Action{
		NewAddAction("file1.parquet", 100, ""),
		NewAddAction("file2.parquet", 200, ""),
	}, "WRITE")
	require.NoError(t, err)

	files, err := table.ActiveFiles(ctx)
	require.NoError(t, err)
	assert.Len(t, files, 2)
}
