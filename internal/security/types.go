package security

import (
	"context"
	"time"
)

// SecurityLevel represents the severity of a security finding
type SecurityLevel string

const (
	SecurityLevelCritical SecurityLevel = "critical"
	SecurityLevelHigh     SecurityLevel = "high"
	SecurityLevelMedium   SecurityLevel = "medium"
	SecurityLevelLow      SecurityLevel = "low"
	SecurityLevelInfo     SecurityLevel = "info"
)

// SecurityFinding represents a security issue discovered during audit
type SecurityFinding struct {
	ID          string        `json:"id"`
	Level       SecurityLevel `json:"level"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Category    string        `json:"category"`
	File        string        `json:"file,omitempty"`
	Line        int           `json:"line,omitempty"`
	Column      int           `json:"column,omitempty"`
	Rule        string        `json:"rule,omitempty"`
	Remediation string        `json:"remediation,omitempty"`
	References  []string      `json:"references,omitempty"`
	Timestamp   time.Time     `json:"timestamp"`
	Scanner     string        `json:"scanner"`
}

// SecurityReport represents the result of a security audit
type SecurityReport struct {
	ScanID      string            `json:"scan_id"`
	Timestamp   time.Time         `json:"timestamp"`
	Duration    time.Duration     `json:"duration"`
	Summary     SecuritySummary   `json:"summary"`
	Findings    []SecurityFinding `json:"findings"`
	ScanConfig  ScanConfig        `json:"scan_config"`
	Metadata    map[string]string `json:"metadata"`
}

// SecuritySummary provides overview statistics
type SecuritySummary struct {
	Total    int `json:"total"`
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Info     int `json:"info"`
}

// ScanConfig defines what to scan and how
type ScanConfig struct {
	StaticAnalysis    bool                   `json:"static_analysis"`
	DependencyAudit   bool                   `json:"dependency_audit"`
	RuntimeMonitoring bool                   `json:"runtime_monitoring"`
	ConfigValidation  bool                   `json:"config_validation"`
	Paths             []string               `json:"paths"`
	Excludes          []string               `json:"excludes"`
	Rules             map[string]interface{} `json:"rules"`
	FailOnSeverity    SecurityLevel          `json:"fail_on_severity"`
}

// Scanner interface for different types of security scanners
type Scanner interface {
	Name() string
	Scan(ctx context.Context, config ScanConfig) ([]SecurityFinding, error)
	HealthCheck() error
}

// Validator interface for security validation checks
type Validator interface {
	Name() string
	Description() string
	Validate(ctx context.Context, target interface{}) (*ValidationResult, error)
}

// ValidationResult represents the result of a security validation
type ValidationResult struct {
	Valid       bool              `json:"valid"`
	Findings    []SecurityFinding `json:"findings"`
	Remediation []string          `json:"remediation"`
	Score       float64           `json:"score"` // 0-100 security score
	Metadata    map[string]string `json:"metadata"`
}

// RuntimeEvent represents a runtime security event
type RuntimeEvent struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Type      string            `json:"type"`
	Level     SecurityLevel     `json:"level"`
	Source    string            `json:"source"`
	Message   string            `json:"message"`
	Details   map[string]string `json:"details"`
	Action    string            `json:"action"` // taken or recommended action
}

// SecurityMetrics tracks security-related metrics
type SecurityMetrics struct {
	ScansRun           int                          `json:"scans_run"`
	FindingsTotal      int                          `json:"findings_total"`
	FindingsByLevel    map[SecurityLevel]int        `json:"findings_by_level"`
	FindingsByCategory map[string]int               `json:"findings_by_category"`
	LastScanTime       time.Time                    `json:"last_scan_time"`
	AverageScore       float64                      `json:"average_score"`
	RuntimeEvents      int                          `json:"runtime_events"`
	EventsByType       map[string]int               `json:"events_by_type"`
}

// VulnerabilityInfo represents information about a known vulnerability
type VulnerabilityInfo struct {
	ID          string    `json:"id"`
	CVE         string    `json:"cve,omitempty"`
	Package     string    `json:"package"`
	Version     string    `json:"version"`
	FixedIn     string    `json:"fixed_in,omitempty"`
	Severity    string    `json:"severity"`
	CVSS        float64   `json:"cvss,omitempty"`
	Description string    `json:"description"`
	References  []string  `json:"references"`
	PublishedAt time.Time `json:"published_at"`
}

// ConfigSecurityIssue represents a configuration security problem
type ConfigSecurityIssue struct {
	Path        string        `json:"path"`
	Field       string        `json:"field"`
	Issue       string        `json:"issue"`
	Level       SecurityLevel `json:"level"`
	Remediation string        `json:"remediation"`
	Value       interface{}   `json:"value,omitempty"`
}

// CredentialExposure represents a potential credential leak
type CredentialExposure struct {
	Type       string `json:"type"`       // api_key, password, token, etc.
	File       string `json:"file"`
	Line       int    `json:"line"`
	Context    string `json:"context"`    // surrounding code/config
	Confidence string `json:"confidence"` // high, medium, low
	Masked     string `json:"masked"`     // partially masked value
}

// Permission represents a permission or privilege
type Permission struct {
	Type        string   `json:"type"`        // file, network, api, etc.
	Resource    string   `json:"resource"`    // specific resource
	Actions     []string `json:"actions"`     // read, write, execute, etc.
	Granted     bool     `json:"granted"`     // whether permission is granted
	Required    bool     `json:"required"`    // whether permission is required
	Excessive   bool     `json:"excessive"`   // whether permission is excessive
	Remediation string   `json:"remediation"` // how to fix if excessive
}

// NetworkConnection represents a network connection being monitored
type NetworkConnection struct {
	LocalAddr   string    `json:"local_addr"`
	RemoteAddr  string    `json:"remote_addr"`
	Protocol    string    `json:"protocol"`
	State       string    `json:"state"`
	Process     string    `json:"process"`
	PID         int       `json:"pid"`
	Allowed     bool      `json:"allowed"`
	Timestamp   time.Time `json:"timestamp"`
	BytesIn     int64     `json:"bytes_in"`
	BytesOut    int64     `json:"bytes_out"`
}