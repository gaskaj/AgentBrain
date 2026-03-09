package security

import (
	"context"
	"encoding/json"
	"time"
)

// SecurityManager coordinates all security operations
type SecurityManager struct {
	scanner          *SecurityScanner
	dependencyAuditor *DependencyAuditor
	runtimeMonitor   *RuntimeMonitor
	config           SecurityConfig
}

// SecurityConfig holds all security framework configuration
type SecurityConfig struct {
	Enabled          bool                    `yaml:"enabled"`
	StaticAnalysis   StaticAnalysisConfig    `yaml:"static_analysis"`
	DependencyAudit  DependencyAuditConfig   `yaml:"dependency_audit"`
	RuntimeMonitoring RuntimeMonitoringConfig `yaml:"runtime_monitoring"`
	Encryption       EncryptionConfig        `yaml:"encryption"`
}

// StaticAnalysisConfig configures static security analysis
type StaticAnalysisConfig struct {
	Enabled         bool     `yaml:"enabled"`
	FailOnHigh      bool     `yaml:"fail_on_high"`
	SkipDirectories []string `yaml:"skip_directories"`
	ExcludeRules    []string `yaml:"exclude_rules"`
	CustomRules     []Rule   `yaml:"custom_rules"`
}

// DependencyAuditConfig configures dependency vulnerability scanning
type DependencyAuditConfig struct {
	Enabled          bool          `yaml:"enabled"`
	FailOnHigh       bool          `yaml:"fail_on_high"`
	CheckInterval    time.Duration `yaml:"check_interval"`
	NotifyChannels   []string      `yaml:"notify_channels"`
	MaxCVSSScore     float64       `yaml:"max_cvss_score"`
	IgnorePackages   []string      `yaml:"ignore_packages"`
}

// RuntimeMonitoringConfig configures runtime security monitoring
type RuntimeMonitoringConfig struct {
	Enabled                   bool  `yaml:"enabled"`
	AuthFailureThreshold      int   `yaml:"auth_failure_threshold"`
	NetworkAnomalyDetection   bool  `yaml:"network_anomaly_detection"`
	FileAccessMonitoring      bool  `yaml:"file_access_monitoring"`
	MemoryAnomalyDetection    bool  `yaml:"memory_anomaly_detection"`
	ProcessMonitoring         bool  `yaml:"process_monitoring"`
	NetworkConnectionTracking bool  `yaml:"network_connection_tracking"`
}

// EncryptionConfig configures encryption requirements
type EncryptionConfig struct {
	EnforceTLS             bool   `yaml:"enforce_tls"`
	MinTLSVersion          string `yaml:"min_tls_version"`
	CredentialEncryption   bool   `yaml:"credential_encryption"`
	DataAtRestEncryption   bool   `yaml:"data_at_rest_encryption"`
	TransitEncryption      bool   `yaml:"transit_encryption"`
}

// Rule represents a custom security rule
type Rule struct {
	ID          string            `yaml:"id" json:"id"`
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description" json:"description"`
	Pattern     string            `yaml:"pattern" json:"pattern"`
	Severity    string            `yaml:"severity" json:"severity"`
	CWE         string            `yaml:"cwe,omitempty" json:"cwe,omitempty"`
	Fix         string            `yaml:"fix,omitempty" json:"fix,omitempty"`
	Tags        []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Metadata    map[string]string `yaml:"metadata,omitempty" json:"metadata,omitempty"`
}

// ScanResult represents a security finding from static analysis
type ScanResult struct {
	RuleID      string            `json:"rule_id"`
	Severity    string            `json:"severity"`
	Rule        string            `json:"rule"`
	File        string            `json:"file"`
	Line        int               `json:"line"`
	Column      int               `json:"column,omitempty"`
	Description string            `json:"description"`
	CWE         string            `json:"cwe,omitempty"`
	Fix         string            `json:"fix,omitempty"`
	Evidence    string            `json:"evidence,omitempty"`
	Confidence  string            `json:"confidence,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Vulnerability represents a CVE or security vulnerability
type Vulnerability struct {
	ID              string                 `json:"id"`
	Package         string                 `json:"package"`
	Version         string                 `json:"version"`
	CVE             string                 `json:"cve"`
	Severity        string                 `json:"severity"`
	Description     string                 `json:"description"`
	FixedIn         string                 `json:"fixed_in,omitempty"`
	CVSS            float64                `json:"cvss"`
	CVSSVector      string                 `json:"cvss_vector,omitempty"`
	References      []string               `json:"references,omitempty"`
	PublishedDate   time.Time              `json:"published_date,omitempty"`
	ModifiedDate    time.Time              `json:"modified_date,omitempty"`
	AffectedRanges  []VersionRange         `json:"affected_ranges,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// VersionRange represents a range of affected versions
type VersionRange struct {
	Type       string `json:"type"`
	Introduced string `json:"introduced,omitempty"`
	Fixed      string `json:"fixed,omitempty"`
}

// SecurityMetrics contains runtime security monitoring metrics
type SecurityMetrics struct {
	FailedAuthAttempts         int64             `json:"failed_auth_attempts"`
	UnexpectedNetworkIO        int64             `json:"unexpected_network_io"`
	SuspiciousFileAccess       int64             `json:"suspicious_file_access"`
	MemoryAnomalies            int64             `json:"memory_anomalies"`
	ProcessAnomalies           int64             `json:"process_anomalies"`
	UnauthorizedNetworkConns   int64             `json:"unauthorized_network_connections"`
	TLSViolations              int64             `json:"tls_violations"`
	CredentialExposures        int64             `json:"credential_exposures"`
	DataIntegrityViolations    int64             `json:"data_integrity_violations"`
	AccessControlViolations    int64             `json:"access_control_violations"`
	LastUpdated                time.Time         `json:"last_updated"`
	MetricsByCategory          map[string]int64  `json:"metrics_by_category"`
	TrendData                  []MetricDataPoint `json:"trend_data,omitempty"`
}

// MetricDataPoint represents a time-series data point for security metrics
type MetricDataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     int64     `json:"value"`
	Category  string    `json:"category"`
}

// SecurityReport contains the results of a comprehensive security scan
type SecurityReport struct {
	ID                  string               `json:"id"`
	Timestamp           time.Time            `json:"timestamp"`
	Status              string               `json:"status"`
	Summary             SecuritySummary      `json:"summary"`
	StaticAnalysis      []ScanResult         `json:"static_analysis,omitempty"`
	Vulnerabilities     []Vulnerability      `json:"vulnerabilities,omitempty"`
	RuntimeMetrics      SecurityMetrics      `json:"runtime_metrics,omitempty"`
	Recommendations     []Recommendation     `json:"recommendations,omitempty"`
	ComplianceStatus    map[string]bool      `json:"compliance_status,omitempty"`
	RiskScore           float64              `json:"risk_score"`
	GeneratedBy         string               `json:"generated_by"`
	ConfigSnapshot      json.RawMessage      `json:"config_snapshot,omitempty"`
}

// SecuritySummary provides high-level security scan results
type SecuritySummary struct {
	TotalIssues         int                    `json:"total_issues"`
	IssuesBySeverity    map[string]int         `json:"issues_by_severity"`
	VulnerabilitiesFound int                   `json:"vulnerabilities_found"`
	HighSeverityIssues  int                    `json:"high_severity_issues"`
	CriticalIssues      int                    `json:"critical_issues"`
	SecurityScore       float64                `json:"security_score"`
	ComplianceScore     float64                `json:"compliance_score"`
	TrendComparison     *SecurityTrendSummary  `json:"trend_comparison,omitempty"`
	TopRisks            []string               `json:"top_risks,omitempty"`
	ImprovementAreas    []string               `json:"improvement_areas,omitempty"`
}

// SecurityTrendSummary compares current scan with historical data
type SecurityTrendSummary struct {
	IssuesChange              int     `json:"issues_change"`
	VulnerabilitiesChange     int     `json:"vulnerabilities_change"`
	SecurityScoreChange       float64 `json:"security_score_change"`
	Trend                     string  `json:"trend"` // "improving", "degrading", "stable"
	PreviousScanTimestamp     time.Time `json:"previous_scan_timestamp,omitempty"`
}

// Recommendation represents a security improvement suggestion
type Recommendation struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Severity    string            `json:"severity"`
	Category    string            `json:"category"`
	Fix         string            `json:"fix"`
	References  []string          `json:"references,omitempty"`
	Effort      string            `json:"effort"` // "low", "medium", "high"
	Impact      string            `json:"impact"` // "low", "medium", "high"
	Tags        []string          `json:"tags,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// StaticSecurityScanner interface defines the contract for security scanners
type StaticSecurityScanner interface {
	Scan(ctx context.Context, target string) ([]ScanResult, error)
	ValidateConfig() error
	GetSupportedRules() ([]Rule, error)
}

// VulnerabilityScanner interface defines the contract for vulnerability scanners
type VulnerabilityScanner interface {
	ScanDependencies(ctx context.Context, manifestPath string) ([]Vulnerability, error)
	CheckPackage(ctx context.Context, pkg, version string) ([]Vulnerability, error)
	UpdateDatabase(ctx context.Context) error
	GetDatabaseInfo() (map[string]interface{}, error)
}

// RuntimeSecurityMonitor interface defines the contract for runtime monitoring
type RuntimeSecurityMonitor interface {
	Start(ctx context.Context) error
	Stop() error
	GetMetrics() SecurityMetrics
	RecordEvent(event SecurityEvent) error
	IsHealthy() bool
}

// SecurityEvent represents a runtime security event
type SecurityEvent struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Type        string                 `json:"type"`
	Severity    string                 `json:"severity"`
	Source      string                 `json:"source"`
	Target      string                 `json:"target,omitempty"`
	Description string                 `json:"description"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	RemediationSuggestion string       `json:"remediation_suggestion,omitempty"`
}

// AlertManager interface for security alert dispatch
type AlertManager interface {
	SendSecurityAlert(ctx context.Context, alert SecurityAlert) error
	GetAlertHistory(ctx context.Context, limit int) ([]SecurityAlert, error)
}

// SecurityAlert represents a security alert to be sent
type SecurityAlert struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Severity    string                 `json:"severity"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Source      string                 `json:"source"`
	Remediation string                 `json:"remediation,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// NewSecurityManager creates a new security manager instance
func NewSecurityManager(config SecurityConfig) (*SecurityManager, error) {
	if !config.Enabled {
		return &SecurityManager{config: config}, nil
	}

	scanner, err := NewSecurityScanner(config.StaticAnalysis)
	if err != nil {
		return nil, err
	}

	auditor, err := NewDependencyAuditor(config.DependencyAudit)
	if err != nil {
		return nil, err
	}

	monitor, err := NewRuntimeMonitor(config.RuntimeMonitoring)
	if err != nil {
		return nil, err
	}

	return &SecurityManager{
		scanner:           scanner,
		dependencyAuditor: auditor,
		runtimeMonitor:    monitor,
		config:            config,
	}, nil
}

// IsEnabled returns whether security framework is enabled
func (sm *SecurityManager) IsEnabled() bool {
	return sm.config.Enabled
}

// GetConfig returns the security configuration
func (sm *SecurityManager) GetConfig() SecurityConfig {
	return sm.config
}