package salesforce

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/agentbrain/agentbrain/internal/connector"
)

// QueryREST executes a SOQL query via the REST API and streams results.
// Suitable for smaller result sets. Automatically follows nextRecordsUrl for pagination.
func (c *Client) QueryREST(ctx context.Context, soql string, objectName string) (<-chan connector.RecordBatch, <-chan error) {
	records := make(chan connector.RecordBatch, 1)
	errs := make(chan error, 1)

	go func() {
		defer close(records)
		defer close(errs)

		path := fmt.Sprintf("%s/query/?q=%s", c.BaseURL(), url.QueryEscape(soql))

		for {
			data, err := c.Get(ctx, path)
			if err != nil {
				errs <- fmt.Errorf("REST query: %w", err)
				return
			}

			var result QueryResult
			if err := json.Unmarshal(data, &result); err != nil {
				errs <- fmt.Errorf("decode query result: %w", err)
				return
			}

			// Strip the "attributes" key Salesforce adds to each record
			cleaned := make([]map[string]any, 0, len(result.Records))
			for _, rec := range result.Records {
				delete(rec, "attributes")
				cleaned = append(cleaned, rec)
			}

			if len(cleaned) > 0 {
				select {
				case records <- connector.RecordBatch{Records: cleaned, Object: objectName}:
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				}
			}

			if result.Done {
				return
			}

			path = result.NextRecordsURL
		}
	}()

	return records, errs
}

// BuildIncrementalSOQL builds a SOQL query for incremental extraction.
func BuildIncrementalSOQL(objectName string, fields []string, watermarkField string, since time.Time) string {
	fieldList := joinFields(fields)
	ts := since.UTC().Format("2006-01-02T15:04:05.000Z")
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s > %s ORDER BY %s ASC",
		fieldList, objectName, watermarkField, ts, watermarkField)
}

// BuildFullSOQL builds a SOQL query for a full snapshot extraction.
func BuildFullSOQL(objectName string, fields []string) string {
	return fmt.Sprintf("SELECT %s FROM %s", joinFields(fields), objectName)
}

func joinFields(fields []string) string {
	result := ""
	for i, f := range fields {
		if i > 0 {
			result += ", "
		}
		result += f
	}
	return result
}
