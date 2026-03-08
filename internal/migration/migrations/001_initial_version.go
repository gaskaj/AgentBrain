package migrations

import (
	"context"
	"encoding/json"
	"fmt"
)

// V001InitialVersion represents the initial migration that wraps existing SyncState in versioning.
type V001InitialVersion struct{}

// Version returns the migration version.
func (m *V001InitialVersion) Version() int {
	return 1
}

// Description returns the migration description.
func (m *V001InitialVersion) Description() string {
	return "Initial version - wrap existing SyncState with versioning support"
}

// Up migrates unversioned SyncState to version 1.
// This is essentially a no-op since version 1 is the baseline.
func (m *V001InitialVersion) Up(ctx context.Context, oldData interface{}) (interface{}, error) {
	// For the initial version, we just validate and pass through the data
	// The data will be in the form of a map[string]interface{} from JSON unmarshaling
	dataMap, ok := oldData.(map[string]interface{})
	if !ok {
		// Try to convert through JSON
		jsonBytes, err := json.Marshal(oldData)
		if err != nil {
			return nil, fmt.Errorf("marshal old data: %w", err)
		}

		dataMap = make(map[string]interface{})
		if err := json.Unmarshal(jsonBytes, &dataMap); err != nil {
			return nil, fmt.Errorf("unmarshal to map: %w", err)
		}
	}

	// Validate required fields exist
	if _, ok := dataMap["source"]; !ok {
		return nil, fmt.Errorf("missing required field: source")
	}

	if _, ok := dataMap["objects"]; !ok {
		dataMap["objects"] = make(map[string]interface{})
	}

	return dataMap, nil
}

// Down migrates version 1 SyncState back to unversioned format.
// This would be used for rollback scenarios.
func (m *V001InitialVersion) Down(ctx context.Context, newData interface{}) (interface{}, error) {
	// For the initial version, down migration just returns the data as-is
	// since version 1 is the same as the original format
	return newData, nil
}

// Validate ensures the data is a valid SyncState structure.
func (m *V001InitialVersion) Validate(ctx context.Context, data interface{}) error {
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("expected map[string]interface{}, got %T", data)
	}

	// Validate source field
	source, ok := dataMap["source"]
	if !ok {
		return fmt.Errorf("missing required field: source")
	}
	if sourceStr, ok := source.(string); !ok || sourceStr == "" {
		return fmt.Errorf("source must be a non-empty string")
	}

	// Validate objects field exists (can be null/empty)
	if _, ok := dataMap["objects"]; !ok {
		return fmt.Errorf("missing required field: objects")
	}

	// Validate objects structure if present
	if objects, ok := dataMap["objects"].(map[string]interface{}); ok {
		for objectName, objectState := range objects {
			if objectName == "" {
				return fmt.Errorf("object name cannot be empty")
			}

			stateMap, ok := objectState.(map[string]interface{})
			if !ok {
				continue // Skip validation if not a map
			}

			// Validate schema version if present
			if schemaVersion, exists := stateMap["schemaVersion"]; exists {
				if version, ok := schemaVersion.(float64); ok && version < 0 {
					return fmt.Errorf("object %s has invalid schema version %v", objectName, version)
				}
			}

			// Validate sync type if present
			if syncType, exists := stateMap["syncType"]; exists {
				if typeStr, ok := syncType.(string); ok {
					if typeStr != "" && typeStr != "full" && typeStr != "incremental" {
						return fmt.Errorf("object %s has invalid sync type %s", objectName, typeStr)
					}
				}
			}
		}
	}

	return nil
}