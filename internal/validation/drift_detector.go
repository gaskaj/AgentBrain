package validation

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/agentbrain/agentbrain/internal/storage"
)

// DriftDetector monitors data patterns over time to detect schema drift.
type DriftDetector struct {
	config        DriftDetectorConfig
	history       map[string][]FieldSnapshot // object -> field snapshots
	s3Client      *storage.S3Client
	storageLayout storage.Layout
	logger        *slog.Logger
}

// DriftDetectorConfig configures drift detection behavior.
type DriftDetectorConfig struct {
	Threshold     float64 `json:"threshold"`      // Threshold for detecting significant drift (0-1)
	WindowSize    int     `json:"window_size"`    // Number of historical snapshots to keep
	MinSampleSize int     `json:"min_sample_size"` // Minimum samples before drift detection
}

// FieldSnapshot captures field statistics at a point in time.
type FieldSnapshot struct {
	Timestamp        time.Time          `json:"timestamp"`
	PopulationRate   float64            `json:"population_rate"`
	NullRate         float64            `json:"null_rate"`
	TypeDistribution map[string]float64 `json:"type_distribution"`
	UniqueValueCount int                `json:"unique_value_count"`
	AverageLength    float64            `json:"average_length,omitempty"`    // For string fields
	NumericStats     *NumericStats      `json:"numeric_stats,omitempty"`     // For numeric fields
}

// NumericStats contains statistical information about numeric fields.
type NumericStats struct {
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"std_dev"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

// DriftReport contains the results of drift detection analysis.
type DriftReport struct {
	ObjectName      string              `json:"object_name"`
	Timestamp       time.Time           `json:"timestamp"`
	DriftDetected   bool                `json:"drift_detected"`
	FieldDrifts     []FieldDriftDetails `json:"field_drifts,omitempty"`
	OverallScore    float64             `json:"overall_score"`     // 0-1, where 1 is maximum drift
	Recommendations []string            `json:"recommendations,omitempty"`
}

// FieldDriftDetails contains drift information for a specific field.
type FieldDriftDetails struct {
	FieldName           string             `json:"field_name"`
	DriftType           string             `json:"drift_type"` // "population", "type", "distribution", "statistical"
	Severity            string             `json:"severity"`   // "low", "medium", "high"
	CurrentValue        float64            `json:"current_value"`
	HistoricalAverage   float64            `json:"historical_average"`
	ChangePercent       float64            `json:"change_percent"`
	Description         string             `json:"description"`
	TypeDistributionOld map[string]float64 `json:"type_distribution_old,omitempty"`
	TypeDistributionNew map[string]float64 `json:"type_distribution_new,omitempty"`
}

// NewDriftDetector creates a new drift detector.
func NewDriftDetector(config DriftDetectorConfig) *DriftDetector {
	return &DriftDetector{
		config:  config,
		history: make(map[string][]FieldSnapshot),
	}
}

// NewDriftDetectorWithStorage creates a drift detector with S3 storage backend.
func NewDriftDetectorWithStorage(config DriftDetectorConfig, s3Client *storage.S3Client, logger *slog.Logger) *DriftDetector {
	return &DriftDetector{
		config:        config,
		history:       make(map[string][]FieldSnapshot),
		s3Client:      s3Client,
		storageLayout: storage.Layout{},
		logger:        logger,
	}
}

// UpdateStats updates the drift detection statistics with new field data.
func (d *DriftDetector) UpdateStats(objectName string, fieldStats map[string]FieldStats) {
	timestamp := time.Now()
	
	// Initialize history for this object if needed
	if _, exists := d.history[objectName]; !exists {
		d.history[objectName] = make([]FieldSnapshot, 0)
	}
	
	// Convert field stats to snapshots
	for fieldName, stats := range fieldStats {
		snapshot := FieldSnapshot{
			Timestamp:        timestamp,
			PopulationRate:   stats.PopulationRate,
			NullRate:         float64(stats.NullCount) / float64(stats.NullCount + stats.NonNullCount),
			TypeDistribution: make(map[string]float64),
			UniqueValueCount: stats.UniqueValues,
		}
		
		// Convert type distribution counts to percentages
		totalTypes := 0
		for _, count := range stats.TypeDistribution {
			totalTypes += count
		}
		
		if totalTypes > 0 {
			for typeName, count := range stats.TypeDistribution {
				snapshot.TypeDistribution[typeName] = float64(count) / float64(totalTypes)
			}
		}
		
		// Add numeric statistics if available
		if stats.NumericStats != nil {
			snapshot.NumericStats = &NumericStats{
				Mean:   stats.NumericStats.Mean,
				StdDev: stats.NumericStats.StdDev,
				Min:    stats.NumericStats.Min,
				Max:    stats.NumericStats.Max,
			}
		}
		
		// Calculate average length for string fields if sample values are available
		if len(stats.SampleValues) > 0 {
			var totalLength int
			var stringCount int
			for _, value := range stats.SampleValues {
				if str, ok := value.(string); ok {
					totalLength += len(str)
					stringCount++
				}
			}
			if stringCount > 0 {
				snapshot.AverageLength = float64(totalLength) / float64(stringCount)
			}
		}
		
		// Store snapshot (using field name as key for now)
		key := fmt.Sprintf("%s.%s", objectName, fieldName)
		snapshots := d.history[key]
		snapshots = append(snapshots, snapshot)
		
		// Keep only the most recent snapshots
		if len(snapshots) > d.config.WindowSize {
			snapshots = snapshots[len(snapshots)-d.config.WindowSize:]
		}
		
		d.history[key] = snapshots
	}
}

// DetectDrift analyzes recent data patterns to detect schema drift.
func (d *DriftDetector) DetectDrift(objectName string) (*DriftReport, error) {
	report := &DriftReport{
		ObjectName:    objectName,
		Timestamp:     time.Now(),
		DriftDetected: false,
		FieldDrifts:   make([]FieldDriftDetails, 0),
	}
	
	var totalDriftScore float64
	var fieldCount int
	
	// Analyze each field for this object
	for key, snapshots := range d.history {
		if !d.isFieldForObject(key, objectName) {
			continue
		}
		
		fieldName := d.extractFieldName(key, objectName)
		
		if len(snapshots) < d.config.MinSampleSize {
			continue // Not enough data for drift detection
		}
		
		fieldDrifts := d.analyzeFieldDrift(fieldName, snapshots)
		
		for _, drift := range fieldDrifts {
			report.FieldDrifts = append(report.FieldDrifts, drift)
			
			// Calculate drift score based on severity
			var driftScore float64
			switch drift.Severity {
			case "high":
				driftScore = 1.0
			case "medium":
				driftScore = 0.6
			case "low":
				driftScore = 0.3
			}
			
			totalDriftScore += driftScore
			report.DriftDetected = true
		}
		
		fieldCount++
	}
	
	// Calculate overall drift score
	if fieldCount > 0 {
		report.OverallScore = totalDriftScore / float64(fieldCount)
	}
	
	// Generate recommendations based on drift patterns
	report.Recommendations = d.generateRecommendations(report.FieldDrifts)
	
	return report, nil
}

// analyzeFieldDrift analyzes drift for a single field.
func (d *DriftDetector) analyzeFieldDrift(fieldName string, snapshots []FieldSnapshot) []FieldDriftDetails {
	var drifts []FieldDriftDetails
	
	if len(snapshots) < 2 {
		return drifts
	}
	
	current := snapshots[len(snapshots)-1]
	
	// Calculate historical averages (excluding current)
	historical := snapshots[:len(snapshots)-1]
	
	// Check for population rate drift
	if drift := d.checkPopulationDrift(fieldName, current, historical); drift != nil {
		drifts = append(drifts, *drift)
	}
	
	// Check for type distribution drift
	if drift := d.checkTypeDistributionDrift(fieldName, current, historical); drift != nil {
		drifts = append(drifts, *drift)
	}
	
	// Check for statistical drift (numeric fields)
	if current.NumericStats != nil {
		if drift := d.checkStatisticalDrift(fieldName, current, historical); drift != nil {
			drifts = append(drifts, *drift)
		}
	}
	
	return drifts
}

// checkPopulationDrift checks for changes in field population rates.
func (d *DriftDetector) checkPopulationDrift(fieldName string, current FieldSnapshot, historical []FieldSnapshot) *FieldDriftDetails {
	if len(historical) == 0 {
		return nil
	}
	
	// Calculate historical average population rate
	var sum float64
	for _, snapshot := range historical {
		sum += snapshot.PopulationRate
	}
	historicalAvg := sum / float64(len(historical))
	
	// Calculate change percentage
	changePercent := math.Abs(current.PopulationRate-historicalAvg) / historicalAvg
	
	if changePercent > d.config.Threshold {
		severity := "low"
		if changePercent > 0.5 {
			severity = "high"
		} else if changePercent > 0.2 {
			severity = "medium"
		}
		
		return &FieldDriftDetails{
			FieldName:         fieldName,
			DriftType:         "population",
			Severity:          severity,
			CurrentValue:      current.PopulationRate,
			HistoricalAverage: historicalAvg,
			ChangePercent:     changePercent * 100,
			Description: fmt.Sprintf("Population rate changed from %.2f%% to %.2f%% (%.1f%% change)",
				historicalAvg*100, current.PopulationRate*100, changePercent*100),
		}
	}
	
	return nil
}

// checkTypeDistributionDrift checks for changes in type distributions.
func (d *DriftDetector) checkTypeDistributionDrift(fieldName string, current FieldSnapshot, historical []FieldSnapshot) *FieldDriftDetails {
	if len(historical) == 0 {
		return nil
	}
	
	// Calculate historical average type distribution
	typeAverages := make(map[string]float64)
	for _, snapshot := range historical {
		for typeName, percentage := range snapshot.TypeDistribution {
			typeAverages[typeName] += percentage
		}
	}
	
	// Normalize to averages
	for typeName := range typeAverages {
		typeAverages[typeName] /= float64(len(historical))
	}
	
	// Calculate drift score
	var totalDrift float64
	allTypes := make(map[string]bool)
	
	// Collect all types (current and historical)
	for typeName := range current.TypeDistribution {
		allTypes[typeName] = true
	}
	for typeName := range typeAverages {
		allTypes[typeName] = true
	}
	
	// Calculate drift for each type
	for typeName := range allTypes {
		currentPct := current.TypeDistribution[typeName]
		historicalPct := typeAverages[typeName]
		
		if historicalPct > 0 {
			drift := math.Abs(currentPct - historicalPct)
			totalDrift += drift
		} else if currentPct > 0 {
			// New type appeared
			totalDrift += currentPct
		}
	}
	
	if totalDrift > d.config.Threshold {
		severity := "low"
		if totalDrift > 0.5 {
			severity = "high"
		} else if totalDrift > 0.2 {
			severity = "medium"
		}
		
		return &FieldDriftDetails{
			FieldName:           fieldName,
			DriftType:           "type",
			Severity:            severity,
			CurrentValue:        totalDrift,
			HistoricalAverage:   0, // Not directly applicable
			ChangePercent:       totalDrift * 100,
			Description:         fmt.Sprintf("Type distribution changed significantly (%.1f%% drift)", totalDrift*100),
			TypeDistributionOld: typeAverages,
			TypeDistributionNew: current.TypeDistribution,
		}
	}
	
	return nil
}

// checkStatisticalDrift checks for statistical changes in numeric fields.
func (d *DriftDetector) checkStatisticalDrift(fieldName string, current FieldSnapshot, historical []FieldSnapshot) *FieldDriftDetails {
	if current.NumericStats == nil {
		return nil
	}
	
	// Calculate historical averages for numeric stats
	var meanSum, minSum, maxSum float64
	var count int
	
	for _, snapshot := range historical {
		if snapshot.NumericStats != nil {
			meanSum += snapshot.NumericStats.Mean
			minSum += snapshot.NumericStats.Min
			maxSum += snapshot.NumericStats.Max
			count++
		}
	}
	
	if count == 0 {
		return nil
	}
	
	historicalMean := meanSum / float64(count)
	
	// Check for significant change in mean
	changePercent := math.Abs(current.NumericStats.Mean-historicalMean) / math.Abs(historicalMean)
	
	if changePercent > d.config.Threshold && math.Abs(historicalMean) > 0.001 { // Avoid division by very small numbers
		severity := "low"
		if changePercent > 0.5 {
			severity = "high"
		} else if changePercent > 0.2 {
			severity = "medium"
		}
		
		return &FieldDriftDetails{
			FieldName:         fieldName,
			DriftType:         "statistical",
			Severity:          severity,
			CurrentValue:      current.NumericStats.Mean,
			HistoricalAverage: historicalMean,
			ChangePercent:     changePercent * 100,
			Description: fmt.Sprintf("Mean value changed from %.2f to %.2f (%.1f%% change)",
				historicalMean, current.NumericStats.Mean, changePercent*100),
		}
	}
	
	return nil
}

// generateRecommendations generates actionable recommendations based on drift patterns.
func (d *DriftDetector) generateRecommendations(fieldDrifts []FieldDriftDetails) []string {
	var recommendations []string
	
	highSeverityCount := 0
	populationDriftFields := make([]string, 0)
	typeDriftFields := make([]string, 0)
	
	for _, drift := range fieldDrifts {
		if drift.Severity == "high" {
			highSeverityCount++
		}
		
		switch drift.DriftType {
		case "population":
			populationDriftFields = append(populationDriftFields, drift.FieldName)
		case "type":
			typeDriftFields = append(typeDriftFields, drift.FieldName)
		}
	}
	
	// Generate specific recommendations
	if highSeverityCount > 0 {
		recommendations = append(recommendations, 
			fmt.Sprintf("Investigate %d fields with high-severity drift - may indicate upstream system changes", highSeverityCount))
	}
	
	if len(populationDriftFields) > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("Population rate changes detected in fields: %v - check for data completeness issues", populationDriftFields))
	}
	
	if len(typeDriftFields) > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("Type distribution changes detected in fields: %v - verify data transformation logic", typeDriftFields))
	}
	
	if len(fieldDrifts) > len(fieldDrifts)/2 { // More than 50% of fields have drift
		recommendations = append(recommendations,
			"Widespread drift detected - consider reviewing connector configuration and source system changes")
	}
	
	return recommendations
}

// SaveDriftMetrics saves drift metrics to S3 storage.
func (d *DriftDetector) SaveDriftMetrics(ctx context.Context, source, objectName string, report *DriftReport) error {
	if d.s3Client == nil {
		return fmt.Errorf("S3 client not configured for drift metrics storage")
	}
	
	key := d.driftMetricsPath(source, objectName)
	
	if err := d.s3Client.PutJSON(ctx, key, report); err != nil {
		return fmt.Errorf("save drift metrics: %w", err)
	}
	
	if d.logger != nil {
		d.logger.Debug("drift metrics saved",
			"source", source,
			"object", objectName,
			"drift_detected", report.DriftDetected,
			"overall_score", report.OverallScore,
		)
	}
	
	return nil
}

// LoadDriftMetrics loads historical drift metrics from S3 storage.
func (d *DriftDetector) LoadDriftMetrics(ctx context.Context, source, objectName string) (*DriftReport, error) {
	if d.s3Client == nil {
		return nil, fmt.Errorf("S3 client not configured for drift metrics storage")
	}
	
	key := d.driftMetricsPath(source, objectName)
	
	var report DriftReport
	if err := d.s3Client.GetJSON(ctx, key, &report); err != nil {
		return nil, fmt.Errorf("load drift metrics: %w", err)
	}
	
	return &report, nil
}

// SaveFieldSnapshots saves field snapshots to S3 for persistence.
func (d *DriftDetector) SaveFieldSnapshots(ctx context.Context, source, objectName string) error {
	if d.s3Client == nil {
		return fmt.Errorf("S3 client not configured for field snapshot storage")
	}
	
	// Filter snapshots for this object
	objectSnapshots := make(map[string][]FieldSnapshot)
	for key, snapshots := range d.history {
		if d.isFieldForObject(key, objectName) {
			objectSnapshots[key] = snapshots
		}
	}
	
	if len(objectSnapshots) == 0 {
		return nil // Nothing to save
	}
	
	key := d.fieldSnapshotsPath(source, objectName)
	
	if err := d.s3Client.PutJSON(ctx, key, objectSnapshots); err != nil {
		return fmt.Errorf("save field snapshots: %w", err)
	}
	
	return nil
}

// LoadFieldSnapshots loads field snapshots from S3 storage.
func (d *DriftDetector) LoadFieldSnapshots(ctx context.Context, source, objectName string) error {
	if d.s3Client == nil {
		return fmt.Errorf("S3 client not configured for field snapshot storage")
	}
	
	key := d.fieldSnapshotsPath(source, objectName)
	
	var objectSnapshots map[string][]FieldSnapshot
	if err := d.s3Client.GetJSON(ctx, key, &objectSnapshots); err != nil {
		// If file doesn't exist, that's okay - we'll start fresh
		return nil
	}
	
	// Merge loaded snapshots into history
	for key, snapshots := range objectSnapshots {
		if existing, exists := d.history[key]; exists {
			// Merge and deduplicate by timestamp
			merged := d.mergeSnapshots(existing, snapshots)
			d.history[key] = merged
		} else {
			d.history[key] = snapshots
		}
	}
	
	return nil
}

// Helper methods

func (d *DriftDetector) driftMetricsPath(source, objectName string) string {
	return fmt.Sprintf("validation/%s/%s/drift_metrics.json", source, objectName)
}

func (d *DriftDetector) fieldSnapshotsPath(source, objectName string) string {
	return fmt.Sprintf("validation/%s/%s/field_snapshots.json", source, objectName)
}

func (d *DriftDetector) isFieldForObject(key, objectName string) bool {
	return len(key) > len(objectName)+1 && key[:len(objectName)+1] == objectName+"."
}

func (d *DriftDetector) extractFieldName(key, objectName string) string {
	if d.isFieldForObject(key, objectName) {
		return key[len(objectName)+1:]
	}
	return key
}

func (d *DriftDetector) mergeSnapshots(existing, new []FieldSnapshot) []FieldSnapshot {
	// Simple merge - in practice, you might want more sophisticated deduplication
	timestampMap := make(map[int64]FieldSnapshot)
	
	for _, snapshot := range existing {
		timestampMap[snapshot.Timestamp.Unix()] = snapshot
	}
	
	for _, snapshot := range new {
		timestampMap[snapshot.Timestamp.Unix()] = snapshot
	}
	
	merged := make([]FieldSnapshot, 0, len(timestampMap))
	for _, snapshot := range timestampMap {
		merged = append(merged, snapshot)
	}
	
	// Sort by timestamp
	for i := range merged {
		for j := i + 1; j < len(merged); j++ {
			if merged[i].Timestamp.After(merged[j].Timestamp) {
				merged[i], merged[j] = merged[j], merged[i]
			}
		}
	}
	
	// Keep only the most recent snapshots
	if len(merged) > d.config.WindowSize {
		merged = merged[len(merged)-d.config.WindowSize:]
	}
	
	return merged
}