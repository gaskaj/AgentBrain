package schema

import (
	"github.com/parquet-go/parquet-go"
)

// ToParquetSchema converts an internal Schema to a parquet-go schema.
func ToParquetSchema(s *Schema) *parquet.Schema {
	nodes := make([]parquet.Node, len(s.Fields))
	for i, f := range s.Fields {
		nodes[i] = fieldToParquetNode(f)
	}

	group := parquet.Group{}
	for _, f := range s.Fields {
		group[f.Name] = fieldToParquetNode(f)
	}

	return parquet.NewSchema(s.ObjectName, group)
}

func fieldToParquetNode(f Field) parquet.Node {
	var node parquet.Node

	switch f.Type {
	case FieldTypeString, FieldTypeDate, FieldTypeDatetime:
		node = parquet.String()
	case FieldTypeInt:
		node = parquet.Int(32)
	case FieldTypeLong:
		node = parquet.Int(64)
	case FieldTypeDouble:
		node = parquet.Leaf(parquet.DoubleType)
	case FieldTypeBoolean:
		node = parquet.Leaf(parquet.BooleanType)
	case FieldTypeBinary:
		node = parquet.Leaf(parquet.ByteArrayType)
	default:
		node = parquet.String()
	}

	if f.Nullable {
		node = parquet.Optional(node)
	}

	return node
}
