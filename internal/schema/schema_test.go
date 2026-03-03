package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSchema(t *testing.T) {
	fields := []Field{
		{Name: "Id", Type: FieldTypeString, Nullable: false},
		{Name: "Name", Type: FieldTypeString, Nullable: true},
		{Name: "Amount", Type: FieldTypeDouble, Nullable: true},
	}

	s := NewSchema("Account", fields, 1)

	assert.Equal(t, "Account", s.ObjectName)
	assert.Equal(t, 3, len(s.Fields))
	assert.Equal(t, 1, s.Version)
	assert.NotEmpty(t, s.Hash)
}

func TestComputeHash_OrderIndependent(t *testing.T) {
	fields1 := []Field{
		{Name: "Id", Type: FieldTypeString},
		{Name: "Name", Type: FieldTypeString},
	}
	fields2 := []Field{
		{Name: "Name", Type: FieldTypeString},
		{Name: "Id", Type: FieldTypeString},
	}

	s1 := NewSchema("Test", fields1, 1)
	s2 := NewSchema("Test", fields2, 1)

	assert.Equal(t, s1.Hash, s2.Hash, "hash should be order-independent")
}

func TestComputeHash_DifferentTypes(t *testing.T) {
	fields1 := []Field{
		{Name: "Amount", Type: FieldTypeDouble},
	}
	fields2 := []Field{
		{Name: "Amount", Type: FieldTypeString},
	}

	s1 := NewSchema("Test", fields1, 1)
	s2 := NewSchema("Test", fields2, 1)

	assert.NotEqual(t, s1.Hash, s2.Hash, "different types should produce different hashes")
}

func TestFieldNames(t *testing.T) {
	fields := []Field{
		{Name: "Zebra", Type: FieldTypeString},
		{Name: "Apple", Type: FieldTypeString},
		{Name: "Mango", Type: FieldTypeString},
	}

	s := NewSchema("Test", fields, 1)
	names := s.FieldNames()

	assert.Equal(t, []string{"Apple", "Mango", "Zebra"}, names)
}

func TestToDeltaSchemaString(t *testing.T) {
	fields := []Field{
		{Name: "Id", Type: FieldTypeString, Nullable: false},
		{Name: "Value", Type: FieldTypeDouble, Nullable: true},
	}

	s := NewSchema("Test", fields, 1)
	schemaStr := s.ToDeltaSchemaString()

	require.Contains(t, schemaStr, `"type":"struct"`)
	require.Contains(t, schemaStr, `"name":"Id"`)
	require.Contains(t, schemaStr, `"name":"Value"`)
}
