package delta

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockS3Store is an in-memory implementation of S3Store for testing.
type mockS3Store struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMockS3Store() *mockS3Store {
	return &mockS3Store{data: make(map[string][]byte)}
}

func (m *mockS3Store) Upload(_ context.Context, key string, data []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = append([]byte{}, data...)
	return nil
}

func (m *mockS3Store) Download(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return d, nil
}

func (m *mockS3Store) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for k := range m.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *mockS3Store) PutJSON(_ context.Context, key string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = data
	return nil
}

func (m *mockS3Store) GetJSON(_ context.Context, key string, v any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.data[key]
	if !ok {
		return fmt.Errorf("not found: %s", key)
	}
	return json.Unmarshal(d, v)
}

func (m *mockS3Store) Exists(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[key]
	return ok, nil
}

func TestTransactionLog_WriteAndRead(t *testing.T) {
	store := newMockS3Store()
	log := NewTransactionLog(store, "data/test/obj/_delta_log/")

	ctx := context.Background()

	actions := []Action{
		NewProtocolAction(),
		NewMetadataAction("test_obj", "obj", `{"type":"struct","fields":[]}`),
	}

	err := log.WriteVersion(ctx, 0, actions)
	require.NoError(t, err)

	read, err := log.ReadVersion(ctx, 0)
	require.NoError(t, err)
	require.Len(t, read, 2)
	assert.NotNil(t, read[0].Protocol)
	assert.NotNil(t, read[1].MetaData)
}

func TestTransactionLog_ListVersions(t *testing.T) {
	store := newMockS3Store()
	log := NewTransactionLog(store, "data/test/obj/_delta_log/")

	ctx := context.Background()

	for i := int64(0); i < 5; i++ {
		err := log.WriteVersion(ctx, i, []Action{NewCommitInfoAction("TEST", i-1, true)})
		require.NoError(t, err)
	}

	versions, err := log.ListVersions(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int64{0, 1, 2, 3, 4}, versions)
}

func TestTransactionLog_LatestVersion(t *testing.T) {
	store := newMockS3Store()
	log := NewTransactionLog(store, "data/test/obj/_delta_log/")

	ctx := context.Background()

	// No versions
	latest, err := log.LatestVersion(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(-1), latest)

	// Write a version
	err = log.WriteVersion(ctx, 0, []Action{NewProtocolAction()})
	require.NoError(t, err)

	latest, err = log.LatestVersion(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), latest)
}
