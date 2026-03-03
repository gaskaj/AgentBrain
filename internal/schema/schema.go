package schema

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// FieldType represents the data type of a schema field.
type FieldType string

const (
	FieldTypeString   FieldType = "string"
	FieldTypeInt      FieldType = "integer"
	FieldTypeLong     FieldType = "long"
	FieldTypeDouble   FieldType = "double"
	FieldTypeBoolean  FieldType = "boolean"
	FieldTypeDate     FieldType = "date"
	FieldTypeDatetime FieldType = "datetime"
	FieldTypeBinary   FieldType = "binary"
)

// Field represents a single field in a schema.
type Field struct {
	Name     string    `json:"name"`
	Type     FieldType `json:"type"`
	Nullable bool      `json:"nullable"`
}

// Schema is a source-agnostic schema representation.
type Schema struct {
	ObjectName string  `json:"objectName"`
	Fields     []Field `json:"fields"`
	Version    int     `json:"version"`
	Hash       string  `json:"hash"`
}

// NewSchema creates a new schema and computes its hash.
func NewSchema(objectName string, fields []Field, version int) *Schema {
	s := &Schema{
		ObjectName: objectName,
		Fields:     fields,
		Version:    version,
	}
	s.Hash = s.ComputeHash()
	return s
}

// ComputeHash generates a deterministic hash of the schema fields.
// The hash is based on field names and types (order-independent).
func (s *Schema) ComputeHash() string {
	type hashField struct {
		Name string    `json:"name"`
		Type FieldType `json:"type"`
	}

	fields := make([]hashField, len(s.Fields))
	for i, f := range s.Fields {
		fields[i] = hashField{Name: f.Name, Type: f.Type}
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })

	data, _ := json.Marshal(fields)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:8])
}

// FieldNames returns a sorted list of field names.
func (s *Schema) FieldNames() []string {
	names := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		names[i] = f.Name
	}
	sort.Strings(names)
	return names
}

// FieldMap returns a map of field name to field.
func (s *Schema) FieldMap() map[string]Field {
	m := make(map[string]Field, len(s.Fields))
	for _, f := range s.Fields {
		m[f.Name] = f
	}
	return m
}

// ToDeltaSchemaString serializes the schema to a Delta-compatible JSON schema string.
func (s *Schema) ToDeltaSchemaString() string {
	type deltaField struct {
		Name     string            `json:"name"`
		Type     string            `json:"type"`
		Nullable bool              `json:"nullable"`
		Metadata map[string]string `json:"metadata"`
	}
	type deltaSchema struct {
		Type   string       `json:"type"`
		Fields []deltaField `json:"fields"`
	}

	ds := deltaSchema{Type: "struct"}
	for _, f := range s.Fields {
		ds.Fields = append(ds.Fields, deltaField{
			Name:     f.Name,
			Type:     string(f.Type),
			Nullable: f.Nullable,
			Metadata: map[string]string{},
		})
	}

	data, _ := json.Marshal(ds)
	return string(data)
}
