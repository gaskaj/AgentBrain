package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiff_NoChanges(t *testing.T) {
	fields := []Field{
		{Name: "Id", Type: FieldTypeString},
		{Name: "Name", Type: FieldTypeString},
	}

	old := NewSchema("Test", fields, 1)
	new := NewSchema("Test", fields, 2)

	diff := Diff(old, new)

	assert.Equal(t, ChangeNone, diff.Change)
	assert.Empty(t, diff.AddedFields)
	assert.Empty(t, diff.RemovedFields)
	assert.Empty(t, diff.TypeChanges)
	assert.False(t, diff.HasChanges())
}

func TestDiff_AdditiveChange(t *testing.T) {
	oldFields := []Field{
		{Name: "Id", Type: FieldTypeString},
	}
	newFields := []Field{
		{Name: "Id", Type: FieldTypeString},
		{Name: "Name", Type: FieldTypeString},
	}

	old := NewSchema("Test", oldFields, 1)
	new := NewSchema("Test", newFields, 2)

	diff := Diff(old, new)

	assert.Equal(t, ChangeAdditive, diff.Change)
	assert.Len(t, diff.AddedFields, 1)
	assert.Equal(t, "Name", diff.AddedFields[0].Name)
	assert.True(t, diff.IsAdditive())
	assert.False(t, diff.IsBreaking())
}

func TestDiff_BreakingRemoval(t *testing.T) {
	oldFields := []Field{
		{Name: "Id", Type: FieldTypeString},
		{Name: "OldField", Type: FieldTypeString},
	}
	newFields := []Field{
		{Name: "Id", Type: FieldTypeString},
	}

	old := NewSchema("Test", oldFields, 1)
	new := NewSchema("Test", newFields, 2)

	diff := Diff(old, new)

	assert.Equal(t, ChangeBreaking, diff.Change)
	assert.Len(t, diff.RemovedFields, 1)
	assert.True(t, diff.IsBreaking())
}

func TestDiff_BreakingTypeChange(t *testing.T) {
	oldFields := []Field{
		{Name: "Amount", Type: FieldTypeString},
	}
	newFields := []Field{
		{Name: "Amount", Type: FieldTypeDouble},
	}

	old := NewSchema("Test", oldFields, 1)
	new := NewSchema("Test", newFields, 2)

	diff := Diff(old, new)

	assert.Equal(t, ChangeBreaking, diff.Change)
	assert.Len(t, diff.TypeChanges, 1)
	assert.Equal(t, "Amount", diff.TypeChanges[0].FieldName)
	assert.Equal(t, FieldTypeString, diff.TypeChanges[0].OldType)
	assert.Equal(t, FieldTypeDouble, diff.TypeChanges[0].NewType)
}
