package security

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSecurityScanner tests the static security scanner functionality
func TestSecurityScanner(t *testing.T) {
	tests := []struct {
		name     string
		config   StaticAnalysisConfig
		testCode string
		expected int
	}{
		{
			name: "enabled scanner with no issues",
			config: StaticAnalysisConfig{
				Enabled: true,
			},
			testCode: `package main
import "fmt"
func main() {
	fmt.Println("Hello, World!")
}`,
			expected: 0,
		},
		{
			name: "disabled scanner",
			config: StaticAnalysisConfig{
				Enabled: false,
			},
			testCode: `package main
import "fmt"
func main() {
	fmt.Println("Hello, World!")
}`,
			expected: 0,
		},
		{
			name: "custom rule detection",
			config: StaticAnalysisConfig{
				Enabled: true,
				CustomRules: []Rule{
					{
						ID:          "TEST001",
						Name:        "Test Rule",
						Description: "Detects test pattern",
						Pattern:     `fmt\.Print`,
						Severity:    "low",
					},
				},
			},
			testCode: `package main
import "fmt"
func main() {
	fmt.Println("Hello, World!")
}`,
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner, err := NewSecurityScanner(tt.config)
			require.NoError(t, err)

			// Create temporary test file
			tmpDir := t.TempDir()
			testFile := filepath.Join(tmpDir, "test.go")
			err = os.WriteFile(testFile, []byte(tt.testCode), 0644)
			require.NoError(t, err)

			if tt.config.Enabled {
				results, err := scanner.Scan(context.Background(), tmpDir)
				require.NoError(t, err)
				assert.Equal(t, tt.expected, len(results))
			} else {
				_, err := scanner.Scan(context.Background(), tmpDir)
				assert.Error(t, err)
			}
		})
	}
}

func TestSecurityScannerValidateConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    StaticAnalysisConfig
		expectErr bool
	}{
		{
			name: "valid config",
			config: StaticAnalysisConfig{
				Enabled: true,
				CustomRules: []Rule{
					{
						ID:          "VALID001",
						Name:        "Valid Rule",
						Description: "A valid rule",
						Pattern:     `test`,
						Severity:    "medium",
					},
				},
			},
			expectErr: false,
		},
		{
			name: "missing rule ID",
			config: StaticAnalysisConfig{
				Enabled: true,
				CustomRules: []Rule{
					{
						Name:        "Invalid Rule",
						Description: "Missing ID",
						Pattern:     `test`,
						Severity:    "medium",
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid severity",
			config: StaticAnalysisConfig{
				Enabled: true,
				CustomRules: []Rule{
					{
						ID:          "INVALID001",
						Name:        "Invalid Severity Rule",
						Description: "Invalid severity",
						Pattern:     `test`,
						Severity:    "invalid",
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid regex pattern",
			config: StaticAnalysisConfig{
				Enabled: true,
				CustomRules: []Rule{
					{
						ID:          "INVALID002",
						Name:        "Invalid Pattern Rule",
						Description: "Invalid regex",
						Pattern:     `[unclosed`,
						Severity:    "medium",
					},
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner, err := NewSecurityScanner(tt.config)
			require.NoError(t, err)

			err = scanner.ValidateConfig()
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSecurityScannerGetSupportedRules(t *testing.T) {
	config := StaticAnalysisConfig{
		Enabled: true,
		CustomRules: []Rule{
			{
				ID:          "CUSTOM001",
				Name:        "Custom Rule",
				Description: "A custom rule",
				Pattern:     `custom`,
				Severity:    "low",
			},
		},
	}

	scanner, err := NewSecurityScanner(config)
	require.NoError(t, err)

	rules, err := scanner.GetSupportedRules()
	require.NoError(t, err)
	
	// Should contain at least gosec rules and our custom rule
	assert.Greater(t, len(rules), 0)

	// Find our custom rule
	found := false
	for _, rule := range rules {
		if rule.ID == "CUSTOM001" {
			found = true
			assert.Equal(t, "Custom Rule", rule.Name)
			break
		}
	}
	assert.True(t, found, "Custom rule should be included in supported rules")
}

// TestDependencyAuditor tests the dependency vulnerability scanner
func TestDependencyAuditor(t *testing.T) {
	tests := []struct {
		name    string
		config  DependencyAuditConfig
		enabled bool
	}{
		{
			name: "enabled auditor",
			config: DependencyAuditConfig{
				Enabled:    true,
				FailOnHigh: true,
			},
			enabled: true,
		},
		{
			name: "disabled auditor",
			config: DependencyAuditConfig{
				Enabled: false,
			},
			enabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auditor, err := NewDependencyAuditor(tt.config)
			require.NoError(t, err)

			if tt.enabled {
				vulns, err := auditor.ScanDependencies(context.Background(), ".")
				// govulncheck may not be available, so allow errors but check that
				// we get either results or an appropriate error
				if err == nil {
					assert.NotNil(t, vulns, "vulns should not be nil when no error")
				} else {
					t.Logf("Expected error due to missing govulncheck: %v", err)
					// Just verify it's a dependency scanning related error
					assert.Contains(t, err.Error(), "govulncheck", "error should mention govulncheck")
				}
			} else {
				_, err := auditor.ScanDependencies(context.Background(), ".")
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "disabled")
			}
		})
	}
}

func TestDependencyAuditorFiltering(t *testing.T) {
	config := DependencyAuditConfig{
		Enabled:        true,
		MaxCVSSScore:   7.0,
		IgnorePackages: []string{"test-package"},
	}

	auditor, err := NewDependencyAuditor(config)
	require.NoError(t, err)

	vulns := []Vulnerability{
		{
			Package:  "test-package",
			Severity: "high",
			CVSS:     8.5,
		},
		{
			Package:  "good-package",
			Severity: "medium",
			CVSS:     6.0,
		},
		{
			Package:  "high-cvss-package",
			Severity: "high",
			CVSS:     9.0,
		},
	}

	filtered := auditor.filterVulnerabilities(vulns)
	
	// Should filter out ignored package and high CVSS package
	assert.Equal(t, 1, len(filtered))
	assert.Equal(t, "good-package", filtered[0].Package)
}

// TestRuntimeMonitor tests the runtime security monitor
func TestRuntimeMonitor(t *testing.T) {
	config := RuntimeMonitoringConfig{
		Enabled:                  true,
		AuthFailureThreshold:     5,
		NetworkAnomalyDetection:  true,
		MemoryAnomalyDetection:   true,
	}

	monitor, err := NewRuntimeMonitor(config)
	require.NoError(t, err)

	// Test starting and stopping
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = monitor.Start(ctx)
	assert.NoError(t, err)

	// Test health check
	assert.True(t, monitor.IsHealthy())

	// Test recording events
	event := SecurityEvent{
		Type:        "auth_failure",
		Severity:    "medium",
		Source:      "test",
		Description: "Test authentication failure",
	}

	err = monitor.RecordEvent(event)
	assert.NoError(t, err)

	// Test metrics
	metrics := monitor.GetMetrics()
	assert.Equal(t, int64(1), metrics.FailedAuthAttempts)
	assert.Contains(t, metrics.MetricsByCategory, "auth_failures")

	// Test recent events
	events := monitor.GetRecentEvents(10)
	assert.Equal(t, 1, len(events))
	assert.Equal(t, "auth_failure", events[0].Type)

	// Test security score
	score := monitor.GetSecurityScore()
	assert.True(t, score >= 0 && score <= 100)

	err = monitor.Stop()
	assert.NoError(t, err)
}

func TestRuntimeMonitorDisabled(t *testing.T) {
	config := RuntimeMonitoringConfig{
		Enabled: false,
	}

	monitor, err := NewRuntimeMonitor(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = monitor.Start(ctx)
	assert.NoError(t, err) // Should not error, just do nothing

	// Recording events should be no-op
	event := SecurityEvent{
		Type:     "test",
		Severity: "low",
	}
	err = monitor.RecordEvent(event)
	assert.NoError(t, err)

	metrics := monitor.GetMetrics()
	assert.Equal(t, int64(0), metrics.FailedAuthAttempts)
}

func TestSecurityEventMetricsUpdate(t *testing.T) {
	config := RuntimeMonitoringConfig{
		Enabled: true,
	}

	monitor, err := NewRuntimeMonitor(config)
	require.NoError(t, err)

	events := []SecurityEvent{
		{Type: "auth_failure", Severity: "medium"},
		{Type: "network_anomaly", Severity: "high"},
		{Type: "file_access", Severity: "low"},
		{Type: "memory_anomaly", Severity: "medium"},
		{Type: "auth_failure", Severity: "medium"}, // Second auth failure
	}

	for _, event := range events {
		err := monitor.RecordEvent(event)
		assert.NoError(t, err)
	}

	metrics := monitor.GetMetrics()
	assert.Equal(t, int64(2), metrics.FailedAuthAttempts)
	assert.Equal(t, int64(1), metrics.UnexpectedNetworkIO)
	assert.Equal(t, int64(1), metrics.SuspiciousFileAccess)
	assert.Equal(t, int64(1), metrics.MemoryAnomalies)
}

// TestSecurityManager tests the main security manager
func TestSecurityManager(t *testing.T) {
	tests := []struct {
		name    string
		config  SecurityConfig
		enabled bool
	}{
		{
			name: "enabled security manager",
			config: SecurityConfig{
				Enabled: true,
				StaticAnalysis: StaticAnalysisConfig{
					Enabled: true,
				},
				DependencyAudit: DependencyAuditConfig{
					Enabled: true,
				},
				RuntimeMonitoring: RuntimeMonitoringConfig{
					Enabled: true,
				},
			},
			enabled: true,
		},
		{
			name: "disabled security manager",
			config: SecurityConfig{
				Enabled: false,
			},
			enabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewSecurityManager(tt.config)
			require.NoError(t, err)

			assert.Equal(t, tt.enabled, manager.IsEnabled())
			
			config := manager.GetConfig()
			assert.Equal(t, tt.config.Enabled, config.Enabled)
		})
	}
}

// TestSecurityScoreCalculation tests security score calculation logic
func TestSecurityScoreCalculation(t *testing.T) {
	scanner, err := NewSecurityScanner(StaticAnalysisConfig{Enabled: true})
	require.NoError(t, err)

	tests := []struct {
		name     string
		results  []ScanResult
		expected float64
	}{
		{
			name:     "no issues",
			results:  []ScanResult{},
			expected: 100.0,
		},
		{
			name: "low severity issues",
			results: []ScanResult{
				{Severity: "low"},
				{Severity: "low"},
			},
			expected: 99.0, // 100 - 2*0.5
		},
		{
			name: "mixed severity issues",
			results: []ScanResult{
				{Severity: "low"},
				{Severity: "medium"},
				{Severity: "high"},
			},
			expected: 92.5, // 100 - 0.5 - 2.0 - 5.0
		},
		{
			name: "critical issues",
			results: []ScanResult{
				{Severity: "critical"},
				{Severity: "critical"},
			},
			expected: 80.0, // 100 - 2*10.0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scanner.calculateSecurityScore(tt.results)
			assert.Equal(t, tt.expected, score)
		})
	}
}

// TestGosecToCWEMapping tests the mapping from gosec rules to CWE identifiers
func TestGosecToCWEMapping(t *testing.T) {
	scanner, err := NewSecurityScanner(StaticAnalysisConfig{Enabled: true})
	require.NoError(t, err)

	tests := []struct {
		ruleID   string
		expected string
	}{
		{"G101", "CWE-798"}, // Hardcoded credentials
		{"G401", "CWE-327"}, // Weak crypto
		{"G204", "CWE-78"},  // Command execution
		{"UNKNOWN", ""},     // Unknown rule
	}

	for _, tt := range tests {
		t.Run(tt.ruleID, func(t *testing.T) {
			cwe := scanner.mapGosecToCWE(tt.ruleID)
			assert.Equal(t, tt.expected, cwe)
		})
	}
}

// TestVulnerabilityConversion tests conversion from OSV to Vulnerability format
func TestVulnerabilityConversion(t *testing.T) {
	auditor, err := NewDependencyAuditor(DependencyAuditConfig{Enabled: true})
	require.NoError(t, err)

	publishedTime := time.Now()
	osv := OSVEntry{
		ID:        "GO-2023-1234",
		Summary:   "Test vulnerability",
		Details:   "Detailed description",
		Published: &publishedTime,
		Aliases:   []string{"CVE-2023-1234", "GHSA-xxxx-yyyy-zzzz"},
		Severity: []OSVSeverity{
			{Type: "CVSS_V3", Score: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"},
		},
		References: []OSVReference{
			{Type: "ADVISORY", URL: "https://example.com/advisory"},
		},
	}

	modules := []VulnModule{
		{
			Path:         "example.com/vulnerable",
			FoundVersion: "v1.0.0",
			FixedVersion: "v1.1.0",
		},
	}

	vuln := auditor.convertOSVToVulnerability(osv, modules)

	assert.Equal(t, "GO-2023-1234", vuln.ID)
	assert.Equal(t, "CVE-2023-1234", vuln.CVE)
	assert.Equal(t, "Test vulnerability", vuln.Description)
	assert.Equal(t, "example.com/vulnerable", vuln.Package)
	assert.Equal(t, "v1.0.0", vuln.Version)
	assert.Equal(t, "v1.1.0", vuln.FixedIn)
	assert.Equal(t, publishedTime, vuln.PublishedDate)
	assert.Contains(t, vuln.References, "https://example.com/advisory")
	assert.Contains(t, vuln.Metadata, "scanner")
}

// TestSecurityReportGeneration tests security report generation
func TestSecurityReportGeneration(t *testing.T) {
	// Test static analysis report
	scanner, err := NewSecurityScanner(StaticAnalysisConfig{Enabled: true})
	require.NoError(t, err)

	// Create test file with issues
	tmpDir := t.TempDir()
	testCode := `package main
import "crypto/md5"
func main() {
	h := md5.New()
	_ = h
}`
	testFile := filepath.Join(tmpDir, "test.go")
	err = os.WriteFile(testFile, []byte(testCode), 0644)
	require.NoError(t, err)

	report, err := scanner.GenerateReport(context.Background(), tmpDir)
	require.NoError(t, err)

	assert.NotEmpty(t, report.ID)
	assert.Equal(t, "completed", report.Status)
	assert.Equal(t, "security-scanner", report.GeneratedBy)
	assert.NotNil(t, report.Summary)

	// Skip if no security issues were found (gosec may not be available)
	t.Logf("Found %d static analysis results", len(report.StaticAnalysis))

	// Test runtime monitor report
	monitor, err := NewRuntimeMonitor(RuntimeMonitoringConfig{Enabled: true})
	require.NoError(t, err)

	// Add some test events
	events := []SecurityEvent{
		{Type: "auth_failure", Severity: "high"},
		{Type: "memory_anomaly", Severity: "medium"},
	}

	for _, event := range events {
		err := monitor.RecordEvent(event)
		assert.NoError(t, err)
	}

	runtimeReport := monitor.GenerateSecurityReport()
	assert.NotNil(t, runtimeReport)
	assert.Equal(t, "runtime-monitor", runtimeReport.GeneratedBy)
	assert.NotNil(t, runtimeReport.RuntimeMetrics)
	// Recommendations may be empty if no significant security events were recorded
	t.Logf("Runtime report has %d recommendations", len(runtimeReport.Recommendations))
}

// TestCustomRuleMatching tests custom rule pattern matching
func TestCustomRuleMatching(t *testing.T) {
	scanner, err := NewSecurityScanner(StaticAnalysisConfig{
		Enabled: true,
		CustomRules: []Rule{
			{
				ID:          "TEST001",
				Name:        "Println Usage",
				Description: "Detects fmt.Println usage",
				Pattern:     `fmt\.Println\(`,
				Severity:    "low",
			},
		},
	})
	require.NoError(t, err)

	testCode := `package main
import "fmt"
func main() {
	fmt.Println("Hello")
	fmt.Printf("World")
	fmt.Println("Again")
}`

	lines := strings.Split(testCode, "\n")
	rule := scanner.customRules[0]
	
	matches := scanner.findRuleMatches(rule, testCode, lines)
	assert.Equal(t, 2, len(matches)) // Two fmt.Println calls

	// Check line numbers are correct - note the file starts at line 1
	expectedLines := []int{4, 6} // Lines are 1-indexed and we have imports
	for i, match := range matches {
		if i < len(expectedLines) {
			assert.Equal(t, expectedLines[i], match.Line)
			assert.Contains(t, match.Evidence, "fmt.Println")
		}
	}
}

// BenchmarkSecurityScanner benchmarks the security scanner performance
func BenchmarkSecurityScanner(b *testing.B) {
	config := StaticAnalysisConfig{
		Enabled: true,
		CustomRules: []Rule{
			{
				ID:          "BENCH001",
				Name:        "Benchmark Rule",
				Description: "Test rule for benchmarking",
				Pattern:     `fmt\.Print`,
				Severity:    "low",
			},
		},
	}

	scanner, err := NewSecurityScanner(config)
	require.NoError(b, err)

	// Create test directory with multiple files
	tmpDir := b.TempDir()
	testCode := `package main
import "fmt"
func main() {
	fmt.Println("Hello, World!")
	fmt.Printf("Formatted output: %s\n", "test")
}`

	// Create multiple test files
	for i := 0; i < 10; i++ {
		testFile := filepath.Join(tmpDir, fmt.Sprintf("test%d.go", i))
		err = os.WriteFile(testFile, []byte(testCode), 0644)
		require.NoError(b, err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scanner.Scan(context.Background(), tmpDir)
		require.NoError(b, err)
	}
}

// TestConfigValidation tests security configuration validation
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    SecurityConfig
		expectErr bool
	}{
		{
			name: "valid config",
			config: SecurityConfig{
				Enabled: true,
				StaticAnalysis: StaticAnalysisConfig{
					Enabled: true,
					CustomRules: []Rule{
						{
							ID:          "VALID001",
							Name:        "Valid Rule",
							Description: "A valid rule",
							Pattern:     `test`,
							Severity:    "medium",
						},
					},
				},
			},
			expectErr: false,
		},
		{
			name: "disabled config",
			config: SecurityConfig{
				Enabled: false,
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewSecurityManager(tt.config)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, manager)
			}
		})
	}
}