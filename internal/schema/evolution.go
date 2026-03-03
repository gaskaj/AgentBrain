package schema

// ChangeType categorizes a schema change.
type ChangeType int

const (
	ChangeNone     ChangeType = iota
	ChangeAdditive            // new columns added (safe for incremental)
	ChangeBreaking            // columns removed or types changed (requires full resync)
)

// SchemaDiff describes the differences between two schemas.
type SchemaDiff struct {
	Change       ChangeType
	AddedFields  []Field
	RemovedFields []Field
	TypeChanges  []TypeChange
}

// TypeChange records a field whose type changed.
type TypeChange struct {
	FieldName string
	OldType   FieldType
	NewType   FieldType
}

// Diff compares old and new schemas and returns the diff.
func Diff(old, new *Schema) *SchemaDiff {
	diff := &SchemaDiff{Change: ChangeNone}

	oldMap := old.FieldMap()
	newMap := new.FieldMap()

	// Find added fields
	for name, newField := range newMap {
		if _, exists := oldMap[name]; !exists {
			diff.AddedFields = append(diff.AddedFields, newField)
		}
	}

	// Find removed fields
	for name, oldField := range oldMap {
		if _, exists := newMap[name]; !exists {
			diff.RemovedFields = append(diff.RemovedFields, oldField)
		}
	}

	// Find type changes
	for name, oldField := range oldMap {
		if newField, exists := newMap[name]; exists {
			if oldField.Type != newField.Type {
				diff.TypeChanges = append(diff.TypeChanges, TypeChange{
					FieldName: name,
					OldType:   oldField.Type,
					NewType:   newField.Type,
				})
			}
		}
	}

	// Determine overall change type
	if len(diff.RemovedFields) > 0 || len(diff.TypeChanges) > 0 {
		diff.Change = ChangeBreaking
	} else if len(diff.AddedFields) > 0 {
		diff.Change = ChangeAdditive
	}

	return diff
}

// IsBreaking returns true if the diff contains breaking changes.
func (d *SchemaDiff) IsBreaking() bool {
	return d.Change == ChangeBreaking
}

// IsAdditive returns true if the diff only contains additive changes.
func (d *SchemaDiff) IsAdditive() bool {
	return d.Change == ChangeAdditive
}

// HasChanges returns true if there are any schema changes.
func (d *SchemaDiff) HasChanges() bool {
	return d.Change != ChangeNone
}
