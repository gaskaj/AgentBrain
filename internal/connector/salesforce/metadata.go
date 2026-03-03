package salesforce

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/agentbrain/agentbrain/internal/connector"
	"github.com/agentbrain/agentbrain/internal/schema"
)

// DescribeGlobal fetches metadata for all SObjects.
func (c *Client) DescribeGlobal(ctx context.Context) ([]connector.ObjectMetadata, error) {
	path := c.BaseURL() + "/sobjects/"
	data, err := c.Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("describe global: %w", err)
	}

	var result DescribeGlobalResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode describe global: %w", err)
	}

	var objects []connector.ObjectMetadata
	for _, obj := range result.SObjects {
		if !obj.Queryable {
			continue
		}
		watermark := "SystemModstamp"
		if !obj.Replicateable {
			watermark = "LastModifiedDate"
		}
		objects = append(objects, connector.ObjectMetadata{
			Name:           obj.Name,
			Label:          obj.Label,
			Queryable:      obj.Queryable,
			Retrievable:    obj.Retrievable,
			Replicateable:  obj.Replicateable,
			WatermarkField: watermark,
		})
	}

	return objects, nil
}

// DescribeObject fetches detailed metadata for a single SObject, including schema.
func (c *Client) DescribeObject(ctx context.Context, objectName string) (*connector.ObjectMetadata, error) {
	path := fmt.Sprintf("%s/sobjects/%s/describe", c.BaseURL(), objectName)
	data, err := c.Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("describe %s: %w", objectName, err)
	}

	var result DescribeResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode describe %s: %w", objectName, err)
	}

	fields := make([]schema.Field, 0, len(result.Fields))
	for _, f := range result.Fields {
		fields = append(fields, schema.Field{
			Name:     f.Name,
			Type:     mapSalesforceType(f.Type),
			Nullable: f.Nillable,
		})
	}

	s := schema.NewSchema(objectName, fields, 1)

	return &connector.ObjectMetadata{
		Name:           result.Name,
		Label:          result.Label,
		Queryable:      true,
		Retrievable:    true,
		WatermarkField: "SystemModstamp",
		Schema:         s,
	}, nil
}

func mapSalesforceType(sfType string) schema.FieldType {
	switch sfType {
	case "string", "textarea", "url", "email", "phone", "picklist", "multipicklist",
		"combobox", "reference", "id", "encryptedstring":
		return schema.FieldTypeString
	case "int":
		return schema.FieldTypeInt
	case "long":
		return schema.FieldTypeLong
	case "double", "currency", "percent":
		return schema.FieldTypeDouble
	case "boolean":
		return schema.FieldTypeBoolean
	case "date":
		return schema.FieldTypeDate
	case "datetime":
		return schema.FieldTypeDatetime
	case "base64":
		return schema.FieldTypeBinary
	default:
		return schema.FieldTypeString
	}
}
