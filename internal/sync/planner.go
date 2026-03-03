package sync

import (
	"log/slog"

	"github.com/agentbrain/agentbrain/internal/connector"
	"github.com/agentbrain/agentbrain/internal/schema"
)

// SyncMode determines how an object should be synced.
type SyncMode int

const (
	SyncModeFull        SyncMode = iota // Full snapshot
	SyncModeIncremental                 // Incremental from watermark
	SyncModeSkip                        // Skip this object
)

func (m SyncMode) String() string {
	switch m {
	case SyncModeFull:
		return "full"
	case SyncModeIncremental:
		return "incremental"
	case SyncModeSkip:
		return "skip"
	default:
		return "unknown"
	}
}

// ObjectPlan describes what to do for a single object in a sync run.
type ObjectPlan struct {
	ObjectName     string
	Mode           SyncMode
	Reason         string
	WatermarkField string
	Schema         *schema.Schema
	SchemaChanged  bool
	NewVersion     int
}

// Planner determines whether each object needs a full or incremental sync.
type Planner struct {
	logger *slog.Logger
}

// NewPlanner creates a sync planner.
func NewPlanner(logger *slog.Logger) *Planner {
	return &Planner{logger: logger}
}

// Plan evaluates each object and returns a plan for syncing it.
func (p *Planner) Plan(
	objectMeta *connector.ObjectMetadata,
	objectState *ObjectState,
	allowedObjects []string,
) *ObjectPlan {
	// If we have an explicit allow list, check it
	if len(allowedObjects) > 0 && !contains(allowedObjects, objectMeta.Name) {
		return &ObjectPlan{
			ObjectName: objectMeta.Name,
			Mode:       SyncModeSkip,
			Reason:     "not in allowed objects list",
		}
	}

	plan := &ObjectPlan{
		ObjectName:     objectMeta.Name,
		WatermarkField: objectMeta.WatermarkField,
		Schema:         objectMeta.Schema,
	}

	// No prior state -> full sync
	if objectState == nil {
		plan.Mode = SyncModeFull
		plan.Reason = "no prior sync state"
		plan.NewVersion = 1
		p.logger.Info("plan: full sync (first run)", "object", objectMeta.Name)
		return plan
	}

	// Check schema changes
	if objectMeta.Schema != nil && objectState.SchemaHash != "" {
		newHash := objectMeta.Schema.ComputeHash()
		if newHash != objectState.SchemaHash {
			plan.SchemaChanged = true
			plan.NewVersion = objectState.SchemaVersion + 1

			// If the previous schema fields are available, do a proper diff.
			// Otherwise, treat any hash change as breaking (safe default).
			if objectState.PreviousSchema != nil {
				diff := schema.Diff(objectState.PreviousSchema, objectMeta.Schema)
				if diff.IsBreaking() {
					plan.Mode = SyncModeFull
					plan.Reason = "breaking schema change detected"
					p.logger.Info("plan: full sync (breaking schema change)",
						"object", objectMeta.Name,
						"removedFields", len(diff.RemovedFields),
						"typeChanges", len(diff.TypeChanges),
					)
					return plan
				}
			} else {
				plan.Mode = SyncModeFull
				plan.Reason = "schema changed (no prior schema for diffing)"
				p.logger.Info("plan: full sync (schema hash changed)",
					"object", objectMeta.Name,
					"oldHash", objectState.SchemaHash,
					"newHash", newHash,
				)
				return plan
			}
		}
	}

	// Has prior state and no breaking changes -> incremental
	plan.Mode = SyncModeIncremental
	plan.Reason = "incremental from watermark"
	if plan.NewVersion == 0 {
		plan.NewVersion = objectState.SchemaVersion
	}

	p.logger.Info("plan: incremental sync",
		"object", objectMeta.Name,
		"watermark", objectState.WatermarkValue,
	)

	return plan
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
