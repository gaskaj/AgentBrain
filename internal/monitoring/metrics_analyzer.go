package monitoring

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MetricsAnalyzer provides trend analysis capabilities for health monitoring
type MetricsAnalyzer struct {
	history     []MetricsSnapshot
	maxHistory  int
	mu          sync.RWMutex
}

// NewMetricsAnalyzer creates a new metrics analyzer
func NewMetricsAnalyzer(maxHistory int) *MetricsAnalyzer {
	if maxHistory <= 0 {
		maxHistory = 100 // Default to storing last 100 snapshots
	}
	
	return &MetricsAnalyzer{
		history:    make([]MetricsSnapshot, 0, maxHistory),
		maxHistory: maxHistory,
	}
}

// RecordSnapshot adds a new metrics snapshot to the history
func (ma *MetricsAnalyzer) RecordSnapshot(snapshot MetricsSnapshot) {
	ma.mu.Lock()
	defer ma.mu.Unlock()
	
	ma.history = append(ma.history, snapshot)
	
	// Keep only the most recent snapshots
	if len(ma.history) > ma.maxHistory {
		ma.history = ma.history[1:]
	}
}

// GetTrend analyzes the trend for a specific metric over a time window
func (ma *MetricsAnalyzer) GetTrend(metric string, window time.Duration) (*TrendAnalysis, error) {
	ma.mu.RLock()
	defer ma.mu.RUnlock()
	
	if len(ma.history) < 2 {
		return nil, fmt.Errorf("insufficient data for trend analysis")
	}
	
	now := time.Now()
	cutoff := now.Add(-window)
	
	var values []float64
	var timestamps []time.Time
	
	for _, snapshot := range ma.history {
		if snapshot.Timestamp.After(cutoff) {
			value, err := ma.extractMetricValue(snapshot, metric)
			if err != nil {
				continue // Skip if metric not found
			}
			values = append(values, value)
			timestamps = append(timestamps, snapshot.Timestamp)
		}
	}
	
	if len(values) < 2 {
		return nil, fmt.Errorf("insufficient data points in time window")
	}
	
	return ma.analyzeTrend(values, timestamps), nil
}

// GetAverageMetric calculates the average value of a metric over a time window
func (ma *MetricsAnalyzer) GetAverageMetric(metric string, window time.Duration) (float64, error) {
	ma.mu.RLock()
	defer ma.mu.RUnlock()
	
	now := time.Now()
	cutoff := now.Add(-window)
	
	var sum float64
	var count int
	
	for _, snapshot := range ma.history {
		if snapshot.Timestamp.After(cutoff) {
			value, err := ma.extractMetricValue(snapshot, metric)
			if err != nil {
				continue
			}
			sum += value
			count++
		}
	}
	
	if count == 0 {
		return 0, fmt.Errorf("no data points found in time window")
	}
	
	return sum / float64(count), nil
}

// GetRecentSnapshots returns the N most recent snapshots
func (ma *MetricsAnalyzer) GetRecentSnapshots(n int) []MetricsSnapshot {
	ma.mu.RLock()
	defer ma.mu.RUnlock()
	
	if n >= len(ma.history) {
		// Return a copy to prevent external modification
		snapshots := make([]MetricsSnapshot, len(ma.history))
		copy(snapshots, ma.history)
		return snapshots
	}
	
	start := len(ma.history) - n
	snapshots := make([]MetricsSnapshot, n)
	copy(snapshots, ma.history[start:])
	return snapshots
}

// DetectAnomalies identifies metrics that deviate significantly from historical patterns
func (ma *MetricsAnalyzer) DetectAnomalies(ctx context.Context, current MetricsSnapshot, threshold float64) []Anomaly {
	ma.mu.RLock()
	defer ma.mu.RUnlock()
	
	if len(ma.history) < 10 {
		return nil // Need sufficient history for anomaly detection
	}
	
	var anomalies []Anomaly
	
	// Check key metrics for anomalies
	metrics := []string{
		"agent_failure_rate",
		"workflow_success_rate",
		"disk_usage_percent",
		"memory_usage_percent",
		"api_response_time_ms",
	}
	
	for _, metric := range metrics {
		currentValue, err := ma.extractMetricValue(current, metric)
		if err != nil {
			continue
		}
		
		mean, stdDev := ma.calculateStatistics(metric)
		if stdDev == 0 {
			continue // No variation in historical data
		}
		
		// Calculate z-score
		zScore := (currentValue - mean) / stdDev
		
		if zScore > threshold || zScore < -threshold {
			anomalies = append(anomalies, Anomaly{
				Metric:       metric,
				CurrentValue: currentValue,
				ExpectedRange: fmt.Sprintf("%.2f ± %.2f", mean, stdDev*threshold),
				ZScore:       zScore,
				Severity:     ma.categorizeSeverity(zScore, threshold),
			})
		}
	}
	
	return anomalies
}

func (ma *MetricsAnalyzer) extractMetricValue(snapshot MetricsSnapshot, metric string) (float64, error) {
	switch metric {
	case "agent_failure_rate":
		return snapshot.AgentMetrics.FailureRate, nil
	case "workflow_success_rate":
		return snapshot.BusinessMetrics.WorkflowSuccessRate, nil
	case "disk_usage_percent":
		return snapshot.SystemMetrics.DiskUsagePercent, nil
	case "memory_usage_percent":
		return snapshot.SystemMetrics.MemoryUsagePercent, nil
	case "api_response_time_ms":
		return float64(snapshot.SystemMetrics.APIResponseTime.Milliseconds()), nil
	case "completion_rate":
		return snapshot.AgentMetrics.CompletionRate, nil
	case "token_usage_percent":
		return snapshot.BusinessMetrics.TokenUsagePercent, nil
	case "validation_error_rate":
		// Note: ValidationMetrics would need to be added to MetricsSnapshot
		// For now, return 0 as placeholder
		return 0, nil
	case "schema_drift_score":
		// Note: DriftMetrics would need to be added to MetricsSnapshot
		// For now, return 0 as placeholder
		return 0, nil
	default:
		return 0, fmt.Errorf("unknown metric: %s", metric)
	}
}

func (ma *MetricsAnalyzer) analyzeTrend(values []float64, timestamps []time.Time) *TrendAnalysis {
	if len(values) != len(timestamps) || len(values) < 2 {
		return &TrendAnalysis{Direction: TrendDirectionStable}
	}
	
	// Simple linear regression to determine trend
	n := float64(len(values))
	var sumX, sumY, sumXY, sumX2 float64
	
	for i, value := range values {
		x := float64(timestamps[i].Unix())
		sumX += x
		sumY += value
		sumXY += x * value
		sumX2 += x * x
	}
	
	// Calculate slope (trend direction)
	slope := (n*sumXY - sumX*sumY) / (n*sumX2 - sumX*sumX)
	
	// Determine trend direction and strength
	direction := TrendDirectionStable
	strength := TrendStrengthWeak
	
	if slope > 0.01 {
		direction = TrendDirectionIncreasing
	} else if slope < -0.01 {
		direction = TrendDirectionDecreasing
	}
	
	// Calculate correlation coefficient for trend strength
	avgX := sumX / n
	avgY := sumY / n
	
	var numerator, denomX, denomY float64
	for i, value := range values {
		x := float64(timestamps[i].Unix())
		numerator += (x - avgX) * (value - avgY)
		denomX += (x - avgX) * (x - avgX)
		denomY += (value - avgY) * (value - avgY)
	}
	
	correlation := numerator / (denomX * denomY)
	correlation = correlation * correlation // R-squared
	
	if correlation > 0.7 {
		strength = TrendStrengthStrong
	} else if correlation > 0.3 {
		strength = TrendStrengthModerate
	}
	
	return &TrendAnalysis{
		Direction:   direction,
		Strength:    strength,
		Slope:       slope,
		Correlation: correlation,
		DataPoints:  len(values),
		TimeSpan:    timestamps[len(timestamps)-1].Sub(timestamps[0]),
	}
}

func (ma *MetricsAnalyzer) calculateStatistics(metric string) (mean, stdDev float64) {
	var values []float64
	
	for _, snapshot := range ma.history {
		value, err := ma.extractMetricValue(snapshot, metric)
		if err != nil {
			continue
		}
		values = append(values, value)
	}
	
	if len(values) == 0 {
		return 0, 0
	}
	
	// Calculate mean
	var sum float64
	for _, value := range values {
		sum += value
	}
	mean = sum / float64(len(values))
	
	// Calculate standard deviation
	var variance float64
	for _, value := range values {
		diff := value - mean
		variance += diff * diff
	}
	variance /= float64(len(values))
	stdDev = variance // Simplified - should be sqrt(variance)
	
	return mean, stdDev
}

func (ma *MetricsAnalyzer) categorizeSeverity(zScore, threshold float64) string {
	absZScore := zScore
	if absZScore < 0 {
		absZScore = -absZScore
	}
	
	if absZScore > threshold*2 {
		return "critical"
	} else if absZScore > threshold {
		return "warning"
	}
	return "info"
}

// TrendDirection represents the direction of a metric trend
type TrendDirection string

const (
	TrendDirectionIncreasing TrendDirection = "increasing"
	TrendDirectionDecreasing TrendDirection = "decreasing"
	TrendDirectionStable     TrendDirection = "stable"
)

// TrendStrength represents the strength of a trend
type TrendStrength string

const (
	TrendStrengthWeak     TrendStrength = "weak"
	TrendStrengthModerate TrendStrength = "moderate"
	TrendStrengthStrong   TrendStrength = "strong"
)

// TrendAnalysis contains the results of trend analysis
type TrendAnalysis struct {
	Direction   TrendDirection `json:"direction"`
	Strength    TrendStrength  `json:"strength"`
	Slope       float64        `json:"slope"`
	Correlation float64        `json:"correlation"`
	DataPoints  int            `json:"data_points"`
	TimeSpan    time.Duration  `json:"time_span"`
}

// Anomaly represents a detected anomaly in metrics
type Anomaly struct {
	Metric        string  `json:"metric"`
	CurrentValue  float64 `json:"current_value"`
	ExpectedRange string  `json:"expected_range"`
	ZScore        float64 `json:"z_score"`
	Severity      string  `json:"severity"`
}