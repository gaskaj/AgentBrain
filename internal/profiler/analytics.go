package profiler

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// OperationMetrics holds metrics for a specific operation
type OperationMetrics struct {
	Name        string
	Count       int64
	TotalTime   time.Duration
	MinTime     time.Duration
	MaxTime     time.Duration
	AvgTime     time.Duration
	P50Time     time.Duration
	P95Time     time.Duration
	P99Time     time.Duration
	Errors      int64
	LastUpdated time.Time
	Metadata    map[string]interface{}
}

// PerformanceReport contains comprehensive performance analysis
type PerformanceReport struct {
	GeneratedAt      time.Time
	Period           time.Duration
	Operations       map[string]*OperationMetrics
	ResourceSummary  ResourceSummary
	Bottlenecks      []Bottleneck
	Recommendations  []string
}

// ResourceSummary summarizes resource usage
type ResourceSummary struct {
	AvgMemoryUsage   uint64
	PeakMemoryUsage  uint64
	AvgGoroutines    int
	PeakGoroutines   int
	GCPauseTotal     time.Duration
	GCCount          uint32
	MemoryTrend      MemoryTrend
}

// Bottleneck represents a performance bottleneck
type Bottleneck struct {
	Type        string  // "cpu", "memory", "io", "api"
	Operation   string
	Severity    string  // "low", "medium", "high", "critical"
	Impact      float64 // 0-1 scale
	Description string
	Suggestion  string
}

// Analytics provides performance analytics and insights
type Analytics struct {
	config     ProfilerConfig
	operations map[string]*operationData
	durations  map[string][]time.Duration
	mu         sync.RWMutex
	running    bool
	stopChan   chan struct{}
}

// operationData holds internal operation tracking data
type operationData struct {
	Count       int64
	TotalTime   time.Duration
	MinTime     time.Duration
	MaxTime     time.Duration
	Errors      int64
	LastUpdated time.Time
	Metadata    map[string]interface{}
}

// NewAnalytics creates a new analytics instance
func NewAnalytics(config ProfilerConfig) *Analytics {
	return &Analytics{
		config:     config,
		operations: make(map[string]*operationData),
		durations:  make(map[string][]time.Duration),
		stopChan:   make(chan struct{}),
	}
}

// Start begins analytics collection
func (a *Analytics) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return nil
	}

	a.running = true
	go a.cleanupOldData(ctx)
	return nil
}

// Stop ends analytics collection
func (a *Analytics) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return nil
	}

	close(a.stopChan)
	a.running = false
	return nil
}

// TrackOperation records operation metrics
func (a *Analytics) TrackOperation(name string, duration time.Duration, metadata map[string]interface{}) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	
	// Update operation data
	op, exists := a.operations[name]
	if !exists {
		op = &operationData{
			MinTime: duration,
			MaxTime: duration,
		}
		a.operations[name] = op
	}

	op.Count++
	op.TotalTime += duration
	op.LastUpdated = now

	if duration < op.MinTime {
		op.MinTime = duration
	}
	if duration > op.MaxTime {
		op.MaxTime = duration
	}

	if metadata != nil {
		op.Metadata = metadata
	}

	// Store duration for percentile calculation
	durations := a.durations[name]
	durations = append(durations, duration)
	
	// Keep only last 1000 samples to prevent memory growth
	if len(durations) > 1000 {
		durations = durations[len(durations)-1000:]
	}
	a.durations[name] = durations
}

// TrackError records an error for an operation
func (a *Analytics) TrackError(operationName string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	op, exists := a.operations[operationName]
	if !exists {
		op = &operationData{}
		a.operations[operationName] = op
	}

	op.Errors++
	op.LastUpdated = time.Now()
}

// GetOperationMetrics returns metrics for a specific operation
func (a *Analytics) GetOperationMetrics(name string) (*OperationMetrics, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	op, exists := a.operations[name]
	if !exists {
		return nil, false
	}

	metrics := &OperationMetrics{
		Name:        name,
		Count:       op.Count,
		TotalTime:   op.TotalTime,
		MinTime:     op.MinTime,
		MaxTime:     op.MaxTime,
		Errors:      op.Errors,
		LastUpdated: op.LastUpdated,
		Metadata:    op.Metadata,
	}

	if op.Count > 0 {
		metrics.AvgTime = time.Duration(int64(op.TotalTime) / op.Count)
	}

	// Calculate percentiles
	durations := a.durations[name]
	if len(durations) > 0 {
		sortedDurations := make([]time.Duration, len(durations))
		copy(sortedDurations, durations)
		sort.Slice(sortedDurations, func(i, j int) bool {
			return sortedDurations[i] < sortedDurations[j]
		})

		metrics.P50Time = sortedDurations[len(sortedDurations)*50/100]
		metrics.P95Time = sortedDurations[len(sortedDurations)*95/100]
		metrics.P99Time = sortedDurations[len(sortedDurations)*99/100]
	}

	return metrics, true
}

// GetAllOperationMetrics returns metrics for all operations
func (a *Analytics) GetAllOperationMetrics() map[string]*OperationMetrics {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make(map[string]*OperationMetrics)
	
	for name := range a.operations {
		if metrics, exists := a.getOperationMetricsUnsafe(name); exists {
			result[name] = metrics
		}
	}

	return result
}

// GeneratePerformanceReport creates a comprehensive performance report
func (a *Analytics) GeneratePerformanceReport(monitor *ResourceMonitor, period time.Duration) *PerformanceReport {
	a.mu.RLock()
	defer a.mu.RUnlock()

	report := &PerformanceReport{
		GeneratedAt: time.Now(),
		Period:      period,
		Operations:  a.GetAllOperationMetrics(),
	}

	// Generate resource summary
	if monitor != nil {
		report.ResourceSummary = a.generateResourceSummary(monitor, period)
		report.Bottlenecks = a.identifyBottlenecks(report.Operations, &report.ResourceSummary)
		report.Recommendations = a.generateRecommendations(report.Bottlenecks, &report.ResourceSummary)
	}

	return report
}

// generateResourceSummary creates a resource usage summary
func (a *Analytics) generateResourceSummary(monitor *ResourceMonitor, period time.Duration) ResourceSummary {
	metrics := monitor.GetMetricsHistory()
	
	if len(metrics) == 0 {
		return ResourceSummary{}
	}

	// Filter metrics within the period
	cutoff := time.Now().Add(-period)
	var recentMetrics []ResourceMetrics
	for _, m := range metrics {
		if m.Timestamp.After(cutoff) {
			recentMetrics = append(recentMetrics, m)
		}
	}

	if len(recentMetrics) == 0 {
		recentMetrics = metrics
	}

	var totalMemory uint64
	var peakMemory uint64
	var totalGoroutines int
	var peakGoroutines int
	var gcCount uint32
	var gcPauseTotal time.Duration

	for _, m := range recentMetrics {
		totalMemory += m.MemoryUsage.HeapAlloc
		totalGoroutines += m.GoroutineCount

		if m.MemoryUsage.HeapAlloc > peakMemory {
			peakMemory = m.MemoryUsage.HeapAlloc
		}
		
		if m.GoroutineCount > peakGoroutines {
			peakGoroutines = m.GoroutineCount
		}

		gcCount = m.MemoryUsage.NumGC
		gcPauseTotal = m.MemoryUsage.GCPauseTotal
	}

	return ResourceSummary{
		AvgMemoryUsage:  totalMemory / uint64(len(recentMetrics)),
		PeakMemoryUsage: peakMemory,
		AvgGoroutines:   totalGoroutines / len(recentMetrics),
		PeakGoroutines:  peakGoroutines,
		GCPauseTotal:    gcPauseTotal,
		GCCount:         gcCount,
		MemoryTrend:     monitor.GetMemoryTrend(period),
	}
}

// identifyBottlenecks analyzes metrics to identify performance bottlenecks
func (a *Analytics) identifyBottlenecks(operations map[string]*OperationMetrics, resources *ResourceSummary) []Bottleneck {
	var bottlenecks []Bottleneck

	// Analyze operation performance
	for name, metrics := range operations {
		// High latency operations
		if metrics.P95Time > 5*time.Second {
			severity := "medium"
			if metrics.P95Time > 10*time.Second {
				severity = "high"
			}
			if metrics.P95Time > 30*time.Second {
				severity = "critical"
			}

			bottlenecks = append(bottlenecks, Bottleneck{
				Type:        "latency",
				Operation:   name,
				Severity:    severity,
				Impact:      float64(metrics.P95Time) / float64(30*time.Second),
				Description: fmt.Sprintf("Operation %s has high P95 latency: %v", name, metrics.P95Time),
				Suggestion:  "Consider optimizing the operation or adding caching",
			})
		}

		// High error rate operations
		if metrics.Count > 0 {
			errorRate := float64(metrics.Errors) / float64(metrics.Count)
			if errorRate > 0.05 { // More than 5% error rate
				severity := "medium"
				if errorRate > 0.1 {
					severity = "high"
				}
				if errorRate > 0.2 {
					severity = "critical"
				}

				bottlenecks = append(bottlenecks, Bottleneck{
					Type:        "errors",
					Operation:   name,
					Severity:    severity,
					Impact:      errorRate,
					Description: fmt.Sprintf("Operation %s has high error rate: %.2f%%", name, errorRate*100),
					Suggestion:  "Investigate error causes and improve error handling",
				})
			}
		}
	}

	// Analyze memory usage
	if resources.MemoryTrend.Direction == "increasing" && resources.MemoryTrend.Rate > 1024*1024 { // 1MB/s increase
		severity := "medium"
		if resources.MemoryTrend.Rate > 10*1024*1024 { // 10MB/s
			severity = "high"
		}
		if resources.MemoryTrend.Rate > 100*1024*1024 { // 100MB/s
			severity = "critical"
		}

		bottlenecks = append(bottlenecks, Bottleneck{
			Type:        "memory",
			Operation:   "system",
			Severity:    severity,
			Impact:      resources.MemoryTrend.Rate / (100 * 1024 * 1024), // Normalize to 100MB/s scale
			Description: fmt.Sprintf("Memory usage is increasing at %.2f MB/s", resources.MemoryTrend.Rate/(1024*1024)),
			Suggestion:  "Check for memory leaks and optimize memory usage",
		})
	}

	// Analyze goroutine count
	if resources.PeakGoroutines > 1000 {
		severity := "medium"
		if resources.PeakGoroutines > 5000 {
			severity = "high"
		}
		if resources.PeakGoroutines > 10000 {
			severity = "critical"
		}

		bottlenecks = append(bottlenecks, Bottleneck{
			Type:        "goroutines",
			Operation:   "system",
			Severity:    severity,
			Impact:      float64(resources.PeakGoroutines) / 10000.0,
			Description: fmt.Sprintf("High peak goroutine count: %d", resources.PeakGoroutines),
			Suggestion:  "Review goroutine lifecycle management and prevent goroutine leaks",
		})
	}

	return bottlenecks
}

// generateRecommendations generates optimization recommendations
func (a *Analytics) generateRecommendations(bottlenecks []Bottleneck, resources *ResourceSummary) []string {
	var recommendations []string
	
	// Sort bottlenecks by severity and impact
	sort.Slice(bottlenecks, func(i, j int) bool {
		severityWeight := map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}
		if severityWeight[bottlenecks[i].Severity] != severityWeight[bottlenecks[j].Severity] {
			return severityWeight[bottlenecks[i].Severity] > severityWeight[bottlenecks[j].Severity]
		}
		return bottlenecks[i].Impact > bottlenecks[j].Impact
	})

	// Generate recommendations based on bottlenecks
	for _, bottleneck := range bottlenecks {
		if bottleneck.Severity == "critical" || bottleneck.Severity == "high" {
			recommendations = append(recommendations, bottleneck.Suggestion)
		}
	}

	// General recommendations based on resource usage
	if resources.MemoryTrend.Direction == "increasing" {
		recommendations = append(recommendations, "Monitor memory allocation patterns and implement regular profiling")
	}

	if resources.GCPauseTotal > 100*time.Millisecond {
		recommendations = append(recommendations, "Consider tuning GC parameters to reduce pause times")
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations, "System performance appears healthy - continue monitoring")
	}

	return recommendations
}

// getOperationMetricsUnsafe is an internal method that doesn't acquire locks
func (a *Analytics) getOperationMetricsUnsafe(name string) (*OperationMetrics, bool) {
	op, exists := a.operations[name]
	if !exists {
		return nil, false
	}

	metrics := &OperationMetrics{
		Name:        name,
		Count:       op.Count,
		TotalTime:   op.TotalTime,
		MinTime:     op.MinTime,
		MaxTime:     op.MaxTime,
		Errors:      op.Errors,
		LastUpdated: op.LastUpdated,
		Metadata:    op.Metadata,
	}

	if op.Count > 0 {
		metrics.AvgTime = time.Duration(int64(op.TotalTime) / op.Count)
	}

	// Calculate percentiles
	durations := a.durations[name]
	if len(durations) > 0 {
		sortedDurations := make([]time.Duration, len(durations))
		copy(sortedDurations, durations)
		sort.Slice(sortedDurations, func(i, j int) bool {
			return sortedDurations[i] < sortedDurations[j]
		})

		if len(sortedDurations) > 0 {
			metrics.P50Time = sortedDurations[len(sortedDurations)*50/100]
			metrics.P95Time = sortedDurations[len(sortedDurations)*95/100]
			metrics.P99Time = sortedDurations[len(sortedDurations)*99/100]
		}
	}

	return metrics, true
}

// cleanupOldData periodically cleans up old analytics data
func (a *Analytics) cleanupOldData(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour) // Cleanup every hour
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopChan:
			return
		case <-ticker.C:
			a.performCleanup()
		}
	}
}

// performCleanup removes old analytics data
func (a *Analytics) performCleanup() {
	a.mu.Lock()
	defer a.mu.Unlock()

	cutoff := time.Now().Add(-24 * time.Hour) // Keep data for 24 hours
	
	for name, op := range a.operations {
		if op.LastUpdated.Before(cutoff) {
			delete(a.operations, name)
			delete(a.durations, name)
		}
	}
}