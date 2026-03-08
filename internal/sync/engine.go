package sync

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/agentbrain/agentbrain/internal/config"
	"github.com/agentbrain/agentbrain/internal/connector"
	"github.com/agentbrain/agentbrain/internal/storage"
	"github.com/agentbrain/agentbrain/internal/storage/delta"
)

// Engine orchestrates the sync lifecycle: discover -> plan -> extract -> write -> commit.
type Engine struct {
	connector           connector.Connector
	s3                  *storage.S3Client
	stateStore          *StateStore
	planner             *Planner
	writer              *storage.ParquetWriter
	layout              storage.Layout
	source              string
	concurrency         int
	objects             []string
	logger              *slog.Logger
	consistencyTracker  *ConsistencyTracker
	consistencyConfig   *config.ConsistencyConfig
}

// NewEngine creates a new sync engine.
func NewEngine(
	conn connector.Connector,
	s3 *storage.S3Client,
	source string,
	concurrency int,
	objects []string,
	logger *slog.Logger,
) *Engine {
	return &Engine{
		connector:   conn,
		s3:          s3,
		stateStore:  NewStateStore(s3, logger),
		planner:     NewPlanner(logger),
		writer:      storage.NewParquetWriter(s3, source, logger),
		source:      source,
		concurrency: concurrency,
		objects:     objects,
		logger:      logger,
	}
}

// NewEngineWithConsistency creates a new sync engine with consistency validation.
func NewEngineWithConsistency(
	conn connector.Connector,
	s3 *storage.S3Client,
	source string,
	concurrency int,
	objects []string,
	consistencyConfig *config.ConsistencyConfig,
	logger *slog.Logger,
) *Engine {
	var consistencyTracker *ConsistencyTracker
	if consistencyConfig != nil && consistencyConfig.Enabled {
		// Convert config.ConsistencyConfig to sync.ConsistencyConfig
		syncConfig := ConsistencyConfig{
			Enabled:         consistencyConfig.Enabled,
			Relationships:   consistencyConfig.Relationships,
			Windows:         consistencyConfig.Windows,
			MaxStaleness:    consistencyConfig.MaxStaleness,
			RequiredObjects: consistencyConfig.RequiredObjects,
			FailOnViolation: consistencyConfig.FailOnViolation,
		}
		consistencyTracker = NewConsistencyTracker(s3, source, syncConfig, logger)
	}

	return &Engine{
		connector:           conn,
		s3:                  s3,
		stateStore:          NewStateStore(s3, logger),
		planner:             NewPlanner(logger),
		writer:              storage.NewParquetWriter(s3, source, logger),
		source:              source,
		concurrency:         concurrency,
		objects:             objects,
		logger:              logger,
		consistencyTracker:  consistencyTracker,
		consistencyConfig:   consistencyConfig,
	}
}

// Run executes a full sync cycle.
func (e *Engine) Run(ctx context.Context) error {
	e.logger.Info("starting sync run", "source", e.source)
	startTime := time.Now()

	// 1. Connect
	if err := e.connector.Connect(ctx); err != nil {
		return fmt.Errorf("connect to %s: %w", e.source, err)
	}
	defer e.connector.Close()

	// 2. Load state
	state, err := e.stateStore.Load(ctx, e.source)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	// 3. Discover metadata
	allObjects, err := e.connector.DiscoverMetadata(ctx)
	if err != nil {
		return fmt.Errorf("discover metadata: %w", err)
	}

	// Save catalog
	if err := e.s3.PutJSON(ctx, e.layout.Catalog(e.source), allObjects); err != nil {
		e.logger.Warn("failed to save catalog", "error", err)
	}

	// 4. Plan each object
	var plans []*ObjectPlan
	for i := range allObjects {
		meta := &allObjects[i]

		// Get detailed metadata with schema
		detailed, err := e.connector.DescribeObject(ctx, meta.Name)
		if err != nil {
			e.logger.Warn("failed to describe object, skipping", "object", meta.Name, "error", err)
			continue
		}

		var objState *ObjectState
		if os, ok := state.Objects[meta.Name]; ok {
			objState = &os
		}

		plan := e.planner.Plan(detailed, objState, e.objects)
		if plan.Mode != SyncModeSkip {
			plans = append(plans, plan)
		}
	}

	e.logger.Info("sync plan ready", "total_objects", len(plans))

	// 5. Execute plans concurrently
	sem := make(chan struct{}, e.concurrency)
	var mu sync.Mutex
	var syncErrors []error

	var wg sync.WaitGroup
	for _, plan := range plans {
		wg.Add(1)
		go func(p *ObjectPlan) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			if err := e.syncObject(ctx, state, p); err != nil {
				e.logger.Error("sync failed", "object", p.ObjectName, "error", err)
				mu.Lock()
				syncErrors = append(syncErrors, fmt.Errorf("%s: %w", p.ObjectName, err))
				mu.Unlock()
			}
		}(plan)
	}
	wg.Wait()

	// 6. Validate consistency (if enabled)
	if e.consistencyTracker != nil {
		e.logger.Info("validating sync consistency", "source", e.source)
		report := e.consistencyTracker.ValidateSync(ctx, plans, state)

		if report.HasViolations() {
			e.logger.Warn("sync consistency violations detected",
				"source", e.source,
				"violations", len(report.Violations),
				"critical_violations", report.HasCriticalViolations(),
			)

			// Store report for analysis
			if err := e.consistencyTracker.StoreReport(ctx, report); err != nil {
				e.logger.Warn("failed to store consistency report", "error", err)
			}

			// Update state with consistency information
			state.LastConsistencyCheck = time.Now()
			state.ConsistencyViolations = report.Violations

			// Optionally fail the sync if configured to do so
			if e.consistencyConfig != nil && e.consistencyConfig.FailOnViolation && report.HasCriticalViolations() {
				return fmt.Errorf("sync failed due to critical consistency violations: %d violations detected", len(report.Violations))
			}
		} else {
			e.logger.Info("sync consistency validation passed", "source", e.source)
			state.LastConsistencyCheck = time.Now()
			state.ConsistencyViolations = nil
		}
	}

	// 7. Update run state
	state.LastRunAt = time.Now()
	state.RunCount++

	if err := e.stateStore.Save(ctx, state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	elapsed := time.Since(startTime)
	e.logger.Info("sync run completed",
		"source", e.source,
		"duration", elapsed,
		"objects", len(plans),
		"errors", len(syncErrors),
	)

	if len(syncErrors) > 0 {
		return fmt.Errorf("%d objects failed to sync", len(syncErrors))
	}

	return nil
}

func (e *Engine) syncObject(ctx context.Context, state *SyncState, plan *ObjectPlan) error {
	e.logger.Info("syncing object",
		"object", plan.ObjectName,
		"mode", plan.Mode.String(),
		"reason", plan.Reason,
	)

	// Initialize delta table
	logPrefix := e.layout.DeltaLogPrefix(e.source, plan.ObjectName)
	table := delta.NewDeltaTable(e.s3, e.source, plan.ObjectName, logPrefix, e.logger)

	if plan.Schema != nil {
		schemaStr := plan.Schema.ToDeltaSchemaString()
		if err := table.Initialize(ctx, schemaStr); err != nil {
			return fmt.Errorf("initialize delta table: %w", err)
		}
	}

	// Extract records
	var recordsCh <-chan connector.RecordBatch
	var errsCh <-chan error

	switch plan.Mode {
	case SyncModeFull:
		recordsCh, errsCh = e.connector.GetFullSnapshot(ctx, plan.ObjectName)
	case SyncModeIncremental:
		objState := state.Objects[plan.ObjectName]
		recordsCh, errsCh = e.connector.GetIncrementalChanges(
			ctx, plan.ObjectName, plan.WatermarkField, objState.WatermarkValue)
	}

	// Process batches
	var totalRecords int64
	var deltaActions []delta.Action

	for batch := range recordsCh {
		if len(batch.Records) == 0 {
			continue
		}

		written, err := e.writer.WriteRecords(ctx, plan.ObjectName, plan.Schema, batch.Records)
		if err != nil {
			return fmt.Errorf("write records: %w", err)
		}

		if written != nil {
			totalRecords += written.NumRows
			deltaActions = append(deltaActions, delta.NewAddAction(
				written.Filename,
				written.Size,
				fmt.Sprintf(`{"numRecords":%d}`, written.NumRows),
			))
		}
	}

	// Check for extraction errors
	if err, ok := <-errsCh; ok && err != nil {
		return fmt.Errorf("extraction error: %w", err)
	}

	// Commit to Delta log
	if len(deltaActions) > 0 {
		operation := "WRITE"
		if plan.Mode == SyncModeFull {
			operation = "WRITE (full sync)"
		}

		version, err := table.Commit(ctx, deltaActions, operation)
		if err != nil {
			return fmt.Errorf("delta commit: %w", err)
		}

		// Update state (only on success)
		objState := ObjectState{
			LastSyncTime:    time.Now(),
			WatermarkField:  plan.WatermarkField,
			WatermarkValue:  time.Now(),
			DeltaVersion:    version,
			TotalRecords:    totalRecords,
			LastSyncRecords: totalRecords,
			SyncType:        plan.Mode.String(),
		}

		if plan.Schema != nil {
			objState.SchemaHash = plan.Schema.ComputeHash()
			objState.SchemaVersion = plan.NewVersion
			objState.PreviousSchema = plan.Schema
		}

		// Thread-safe state update
		state.Objects[plan.ObjectName] = objState

		// Save schema version
		if plan.SchemaChanged && plan.Schema != nil {
			if err := e.stateStore.SaveSchemaVersion(ctx, e.source, plan.ObjectName, plan.NewVersion, plan.Schema); err != nil {
				e.logger.Warn("failed to save schema version", "error", err)
			}
		}

		// Maybe checkpoint (using legacy checkpoint manager for now)
		checkpointKey := e.layout.DeltaLastCheckpoint(e.source, plan.ObjectName)
		cm := delta.NewLegacyCheckpointManager(e.s3, table, checkpointKey, e.layout.DeltaLogPrefix(e.source, plan.ObjectName), e.logger)
		if err := cm.MaybeCheckpoint(ctx, version); err != nil {
			e.logger.Warn("checkpoint failed", "error", err)
		}
	} else {
		e.logger.Info("no new records", "object", plan.ObjectName)
	}

	return nil
}
