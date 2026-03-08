package migration

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaRegistry(t *testing.T) {
	registry := NewSchemaRegistry()

	// Test registering schemas
	type TestSchema1 struct {
		Field1 string `json:"field1"`
		Field2 int    `json:"field2"`
	}

	type TestSchema2 struct {
		Field1 string `json:"field1"`
		Field2 int    `json:"field2"`
		Field3 bool   `json:"field3"`
	}

	err := registry.RegisterSchema(1, &TestSchema1{})
	require.NoError(t, err)

	err = registry.RegisterSchema(2, &TestSchema2{})
	require.NoError(t, err)

	// Test getting latest version
	assert.Equal(t, 2, registry.GetLatestVersion())

	// Test getting schema
	schema1, err := registry.GetSchema(1)
	require.NoError(t, err)
	assert.IsType(t, &TestSchema1{}, schema1)

	schema2, err := registry.GetSchema(2)
	require.NoError(t, err)
	assert.IsType(t, &TestSchema2{}, schema2)

	// Test validation
	testData1 := &TestSchema1{Field1: "test", Field2: 123}
	err = registry.ValidateSchema(1, testData1)
	assert.NoError(t, err)

	// Test validation failure (wrong type)
	err = registry.ValidateSchema(1, testData1)
	assert.NoError(t, err)

	// Test getting non-existent schema
	_, err = registry.GetSchema(99)
	assert.Error(t, err)
}

func TestVersionedState(t *testing.T) {
	registry := NewSchemaRegistry()

	type TestData struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	err := registry.RegisterSchema(1, &TestData{})
	require.NoError(t, err)

	originalData := &TestData{Name: "test", Count: 42}

	// Test wrapping data
	versionedState := registry.GetVersionedState(originalData)
	assert.Equal(t, 1, versionedState.Version)
	assert.NotZero(t, versionedState.CreatedAt)
	assert.NotZero(t, versionedState.UpdatedAt)

	// Test extracting data
	var extractedData TestData
	err = registry.ExtractData(versionedState, &extractedData)
	require.NoError(t, err)
	assert.Equal(t, "test", extractedData.Name)
	assert.Equal(t, 42, extractedData.Count)
}

func TestIsVersioned(t *testing.T) {
	// Test versioned data
	versionedData := map[string]interface{}{
		"version":    1,
		"data":       map[string]interface{}{"field": "value"},
		"created_at": time.Now(),
		"updated_at": time.Now(),
	}
	jsonBytes, _ := json.Marshal(versionedData)
	assert.True(t, IsVersioned(jsonBytes))

	// Test unversioned data
	unversionedData := map[string]interface{}{
		"field": "value",
	}
	jsonBytes, _ = json.Marshal(unversionedData)
	assert.False(t, IsVersioned(jsonBytes))

	// Test invalid JSON
	assert.False(t, IsVersioned([]byte("invalid json")))
}

func TestWrapLegacyData(t *testing.T) {
	legacyData := map[string]interface{}{
		"field1": "value1",
		"field2": 42,
	}

	wrappedData := WrapLegacyData(legacyData)
	assert.Equal(t, 1, wrappedData.Version)
	assert.Equal(t, legacyData, wrappedData.Data)
	assert.NotZero(t, wrappedData.CreatedAt)
	assert.NotZero(t, wrappedData.UpdatedAt)
	assert.Equal(t, wrappedData.CreatedAt, wrappedData.UpdatedAt)
}