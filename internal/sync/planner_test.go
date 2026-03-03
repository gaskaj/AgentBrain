package sync

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/agentbrain/agentbrain/internal/connector"
	"github.com/agentbrain/agentbrain/internal/schema"
)

func TestPlanner_FirstRun(t *testing.T) {
	p := NewPlanner(slog.Default())

	meta := &connector.ObjectMetadata{
		Name:           "Account",
		WatermarkField: "SystemModstamp",
		Schema: schema.NewSchema("Account", []schema.Field{
			{Name: "Id", Type: schema.FieldTypeString},
		}, 1),
	}

	plan := p.Plan(meta, nil, nil)

	assert.Equal(t, SyncModeFull, plan.Mode)
	assert.Equal(t, "no prior sync state", plan.Reason)
	assert.Equal(t, 1, plan.NewVersion)
}

func TestPlanner_IncrementalSync(t *testing.T) {
	p := NewPlanner(slog.Default())

	fields := []schema.Field{{Name: "Id", Type: schema.FieldTypeString}}
	s := schema.NewSchema("Account", fields, 1)

	meta := &connector.ObjectMetadata{
		Name:           "Account",
		WatermarkField: "SystemModstamp",
		Schema:         s,
	}

	state := &ObjectState{
		LastSyncTime:  time.Now().Add(-1 * time.Hour),
		SchemaHash:    s.ComputeHash(),
		SchemaVersion: 1,
	}

	plan := p.Plan(meta, state, nil)

	assert.Equal(t, SyncModeIncremental, plan.Mode)
}

func TestPlanner_BreakingSchemaChange(t *testing.T) {
	p := NewPlanner(slog.Default())

	oldFields := []schema.Field{
		{Name: "Id", Type: schema.FieldTypeString},
		{Name: "Amount", Type: schema.FieldTypeString},
	}
	oldSchema := schema.NewSchema("Account", oldFields, 1)

	newFields := []schema.Field{
		{Name: "Id", Type: schema.FieldTypeString},
		{Name: "Amount", Type: schema.FieldTypeDouble},
	}
	newSchema := schema.NewSchema("Account", newFields, 2)

	meta := &connector.ObjectMetadata{
		Name:           "Account",
		WatermarkField: "SystemModstamp",
		Schema:         newSchema,
	}

	state := &ObjectState{
		SchemaHash:     oldSchema.ComputeHash(),
		SchemaVersion:  1,
		PreviousSchema: oldSchema,
	}

	plan := p.Plan(meta, state, nil)

	assert.Equal(t, SyncModeFull, plan.Mode)
	assert.Contains(t, plan.Reason, "breaking schema change")
}

func TestPlanner_SchemaChangedNoPriorSchema(t *testing.T) {
	p := NewPlanner(slog.Default())

	newFields := []schema.Field{
		{Name: "Id", Type: schema.FieldTypeString},
	}
	newSchema := schema.NewSchema("Account", newFields, 2)

	meta := &connector.ObjectMetadata{
		Name:           "Account",
		WatermarkField: "SystemModstamp",
		Schema:         newSchema,
	}

	state := &ObjectState{
		SchemaHash:    "different-hash",
		SchemaVersion: 1,
	}

	plan := p.Plan(meta, state, nil)

	assert.Equal(t, SyncModeFull, plan.Mode)
	assert.Contains(t, plan.Reason, "schema changed")
}

func TestPlanner_SkipNotInAllowList(t *testing.T) {
	p := NewPlanner(slog.Default())

	meta := &connector.ObjectMetadata{
		Name: "CustomObject__c",
	}

	plan := p.Plan(meta, nil, []string{"Account", "Contact"})

	assert.Equal(t, SyncModeSkip, plan.Mode)
}
