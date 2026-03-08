package storage

import "fmt"

// Layout defines all S3 key path conventions for the data lake.
type Layout struct{}

// DataPrefix returns the base prefix for a source's data.
func (Layout) DataPrefix(source, object string) string {
	return fmt.Sprintf("data/%s/%s", source, object)
}

// DeltaLogPrefix returns the path to an object's Delta transaction log directory.
func (Layout) DeltaLogPrefix(source, object string) string {
	return fmt.Sprintf("data/%s/%s/_delta_log/", source, object)
}

// DeltaLogEntry returns the path to a specific Delta log version file.
func (Layout) DeltaLogEntry(source, object string, version int64) string {
	return fmt.Sprintf("data/%s/%s/_delta_log/%020d.json", source, object, version)
}

// DeltaCheckpoint returns the path to a Delta checkpoint file.
func (Layout) DeltaCheckpoint(source, object string, version int64) string {
	return fmt.Sprintf("data/%s/%s/_delta_log/%020d.checkpoint.parquet", source, object, version)
}

// DeltaLastCheckpoint returns the path to the _last_checkpoint marker file.
func (Layout) DeltaLastCheckpoint(source, object string) string {
	return fmt.Sprintf("data/%s/%s/_delta_log/_last_checkpoint", source, object)
}

// ParquetFile returns the path to a data Parquet file.
func (Layout) ParquetFile(source, object, filename string) string {
	return fmt.Sprintf("data/%s/%s/%s", source, object, filename)
}

// SyncState returns the path to a source's sync state file.
func (Layout) SyncState(source string) string {
	return fmt.Sprintf("state/%s/sync_state.json", source)
}

// SchemaVersion returns the path to a specific schema version file.
func (Layout) SchemaVersion(source, object string, version int) string {
	return fmt.Sprintf("state/%s/schema_history/%s/v%d.json", source, object, version)
}

// SchemaHistoryPrefix returns the prefix for all schema versions of an object.
func (Layout) SchemaHistoryPrefix(source, object string) string {
	return fmt.Sprintf("state/%s/schema_history/%s/", source, object)
}

// Catalog returns the path to a source's object catalog.
func (Layout) Catalog(source string) string {
	return fmt.Sprintf("metadata/%s/catalog.json", source)
}

// ValidationReport returns the path to a validation report.
func (Layout) ValidationReport(source, syncID string) string {
	return fmt.Sprintf("validation/%s/reports/%s.json", source, syncID)
}

// ValidationReportsPrefix returns the prefix for all validation reports of a source.
func (Layout) ValidationReportsPrefix(source string) string {
	return fmt.Sprintf("validation/%s/reports/", source)
}

// DriftMetrics returns the path to drift metrics for an object.
func (Layout) DriftMetrics(source, object string) string {
	return fmt.Sprintf("validation/%s/%s/drift_metrics.json", source, object)
}

// FieldSnapshots returns the path to field snapshots for an object.
func (Layout) FieldSnapshots(source, object string) string {
	return fmt.Sprintf("validation/%s/%s/field_snapshots.json", source, object)
}
