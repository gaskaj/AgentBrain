package security

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// StaticAnalysisScanner integrates with gosec for Go-specific vulnerability detection
type StaticAnalysisScanner struct {
	logger     *slog.Logger
	gosecPath  string
	customRules []CustomSecurityRule
}

// CustomSecurityRule represents a custom security rule for AgentBrain patterns
type CustomSecurityRule struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Pattern     *regexp.Regexp `json:"-"`
	PatternStr  string        `json:"pattern"`
	Level       SecurityLevel `json:"level"`
	Category    string        `json:"category"`
	Description string        `json:"description"`
	Remediation string        `json:"remediation"`
	FileTypes   []string      `json:"file_types"`
}

// GosecFinding represents a finding from gosec
type GosecFinding struct {
	Severity   string `json:"severity"`
	Confidence string `json:"confidence"`
	RuleID     string `json:"rule_id"`
	Details    string `json:"details"`
	File       string `json:"file"`
	Code       string `json:"code"`
	Line       string `json:"line"`
	Column     string `json:"column"`
}

// GosecOutput represents the complete gosec JSON output
type GosecOutput struct {
	Issues []GosecFinding `json:"Issues"`
	Stats  struct {
		Files  int `json:"files"`
		Lines  int `json:"lines"`
		NoSec  int `json:"nosec"`
		Found  int `json:"found"`
	} `json:"Stats"`
}

// NewStaticAnalysisScanner creates a new static analysis scanner
func NewStaticAnalysisScanner(logger *slog.Logger) *StaticAnalysisScanner {
	scanner := &StaticAnalysisScanner{
		logger:      logger,
		gosecPath:   findGosec(),
		customRules: getAgentBrainCustomRules(),
	}

	return scanner
}

// Name returns the scanner name
func (s *StaticAnalysisScanner) Name() string {
	return "static_analysis"
}

// Scan performs static analysis on the codebase
func (s *StaticAnalysisScanner) Scan(ctx context.Context, config ScanConfig) ([]SecurityFinding, error) {
	s.logger.Info("Starting static analysis scan")
	startTime := time.Now()

	var allFindings []SecurityFinding

	// Run gosec if available
	if s.gosecPath != "" {
		gosecFindings, err := s.runGosec(ctx, config.Paths, config.Excludes)
		if err != nil {
			s.logger.Warn("Gosec scan failed", "error", err)
		} else {
			allFindings = append(allFindings, gosecFindings...)
		}
	} else {
		s.logger.Warn("Gosec not found, skipping gosec scan")
	}

	// Run custom AgentBrain security rules
	customFindings, err := s.runCustomRules(ctx, config.Paths, config.Excludes)
	if err != nil {
		s.logger.Warn("Custom rules scan failed", "error", err)
	} else {
		allFindings = append(allFindings, customFindings...)
	}

	duration := time.Since(startTime)
	s.logger.Info("Static analysis scan completed", 
		"findings", len(allFindings), 
		"duration", duration)

	return allFindings, nil
}

// HealthCheck verifies the scanner can operate properly
func (s *StaticAnalysisScanner) HealthCheck() error {
	if s.gosecPath == "" {
		return fmt.Errorf("gosec not found in PATH")
	}

	// Test gosec execution
	cmd := exec.Command(s.gosecPath, "-version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gosec health check failed: %w", err)
	}

	return nil
}

// runGosec executes gosec and parses its output
func (s *StaticAnalysisScanner) runGosec(ctx context.Context, paths, excludes []string) ([]SecurityFinding, error) {
	args := []string{
		"-quiet",
		"-fmt", "json",
		"-no-fail",
	}

	// Add exclude patterns
	for _, exclude := range excludes {
		args = append(args, "-exclude-dir", exclude)
	}

	// Add paths to scan (default to current directory)
	if len(paths) == 0 {
		args = append(args, "./...")
	} else {
		for _, path := range paths {
			args = append(args, path+"/...")
		}
	}

	cmd := exec.CommandContext(ctx, s.gosecPath, args...)
	output, err := cmd.Output()
	if err != nil {
		// Gosec returns non-zero exit code when findings are found
		// Check if it's actually an execution error
		if len(output) == 0 {
			return nil, fmt.Errorf("gosec execution failed: %w", err)
		}
	}

	var gosecOutput GosecOutput
	if err := json.Unmarshal(output, &gosecOutput); err != nil {
		return nil, fmt.Errorf("failed to parse gosec output: %w", err)
	}

	return s.convertGosecFindings(gosecOutput.Issues), nil
}

// convertGosecFindings converts gosec findings to SecurityFinding format
func (s *StaticAnalysisScanner) convertGosecFindings(issues []GosecFinding) []SecurityFinding {
	findings := make([]SecurityFinding, 0, len(issues))

	for _, issue := range issues {
		line, _ := strconv.Atoi(issue.Line)
		column, _ := strconv.Atoi(issue.Column)

		level := s.mapGosecSeverity(issue.Severity, issue.Confidence)
		
		finding := SecurityFinding{
			ID:          uuid.New().String(),
			Level:       level,
			Title:       fmt.Sprintf("Gosec: %s", issue.RuleID),
			Description: issue.Details,
			Category:    "static_analysis",
			File:        issue.File,
			Line:        line,
			Column:      column,
			Rule:        issue.RuleID,
			Remediation: s.getGosecRemediation(issue.RuleID),
			Timestamp:   time.Now(),
			Scanner:     "gosec",
		}

		findings = append(findings, finding)
	}

	return findings
}

// mapGosecSeverity maps gosec severity/confidence to SecurityLevel
func (s *StaticAnalysisScanner) mapGosecSeverity(severity, confidence string) SecurityLevel {
	// Combine severity and confidence to determine final level
	switch {
	case severity == "HIGH" && confidence == "HIGH":
		return SecurityLevelCritical
	case severity == "HIGH":
		return SecurityLevelHigh
	case severity == "MEDIUM" && confidence == "HIGH":
		return SecurityLevelHigh
	case severity == "MEDIUM":
		return SecurityLevelMedium
	case severity == "LOW":
		return SecurityLevelLow
	default:
		return SecurityLevelInfo
	}
}

// getGosecRemediation provides remediation advice for gosec rules
func (s *StaticAnalysisScanner) getGosecRemediation(ruleID string) string {
	remediations := map[string]string{
		"G101": "Remove hardcoded credentials and use environment variables or secure credential stores",
		"G102": "Use crypto/rand instead of math/rand for cryptographic purposes",
		"G103": "Avoid unsafe operations; validate inputs and use safe alternatives",
		"G104": "Always check error returns from functions that can fail",
		"G105": "Use strconv.Atoi or similar safe parsing functions",
		"G106": "Validate and sanitize all inputs from external sources",
		"G107": "Use secure URL construction methods and validate URLs",
		"G108": "Set secure HTTP headers and use HTTPS",
		"G109": "Use crypto/rand instead of math/rand for security-sensitive operations",
		"G110": "Handle potential DoS conditions in decompression operations",
		"G201": "Use parameterized queries to prevent SQL injection",
		"G202": "Use parameterized queries to prevent SQL injection",
		"G203": "Validate and sanitize HTML templates",
		"G204": "Use exec.Command with separate arguments instead of shell commands",
		"G301": "Set appropriate file permissions (avoid 0777)",
		"G302": "Set appropriate file permissions for sensitive files",
		"G303": "Use secure temporary directories",
		"G304": "Validate file paths to prevent directory traversal",
		"G305": "Validate archive paths to prevent zip slip",
		"G306": "Set appropriate file permissions",
		"G401": "Use stronger cryptographic hash functions (avoid MD5)",
		"G402": "Use secure TLS configurations",
		"G403": "Use strong cryptographic ciphers",
		"G404": "Use crypto/rand for cryptographic random number generation",
		"G501": "Use secure import paths and validate imported packages",
		"G502": "Use secure import paths",
		"G503": "Use secure import paths",
		"G504": "Use secure import paths",
		"G505": "Use secure import paths",
		"G601": "Avoid implicit memory aliasing in range loops",
	}

	if remediation, exists := remediations[ruleID]; exists {
		return remediation
	}
	return "Review the finding and apply appropriate security measures"
}

// runCustomRules runs AgentBrain-specific security rules
func (s *StaticAnalysisScanner) runCustomRules(ctx context.Context, paths, excludes []string) ([]SecurityFinding, error) {
	var findings []SecurityFinding

	for _, path := range paths {
		pathFindings, err := s.scanPath(path, excludes)
		if err != nil {
			s.logger.Warn("Failed to scan path with custom rules", "path", path, "error", err)
			continue
		}
		findings = append(findings, pathFindings...)
	}

	return findings, nil
}

// scanPath scans a specific path for custom security issues
func (s *StaticAnalysisScanner) scanPath(scanPath string, excludes []string) ([]SecurityFinding, error) {
	var findings []SecurityFinding

	err := filepath.Walk(scanPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and excluded paths
		if info.IsDir() || s.isExcluded(filePath, excludes) {
			return nil
		}

		// Check each custom rule against this file
		for _, rule := range s.customRules {
			if s.matchesFileType(filePath, rule.FileTypes) {
				ruleFindings, scanErr := s.applyRuleToFile(rule, filePath)
				if scanErr != nil {
					s.logger.Debug("Error applying rule to file", 
						"rule", rule.ID, 
						"file", filePath, 
						"error", scanErr)
					continue
				}
				findings = append(findings, ruleFindings...)
			}
		}

		return nil
	})

	return findings, err
}

// findGosec attempts to find gosec in the system PATH
func findGosec() string {
	path, err := exec.LookPath("gosec")
	if err != nil {
		return ""
	}
	return path
}

// getAgentBrainCustomRules returns custom security rules specific to AgentBrain
func getAgentBrainCustomRules() []CustomSecurityRule {
	return []CustomSecurityRule{
		{
			ID:          "AB001",
			Name:        "Hardcoded AWS Credentials",
			PatternStr:  `(?i)(aws_access_key_id|aws_secret_access_key)\s*[:=]\s*["'][^"']{10,}["']`,
			Level:       SecurityLevelCritical,
			Category:    "credential_exposure",
			Description: "Hardcoded AWS credentials detected",
			Remediation: "Use IAM roles, environment variables, or AWS credential files instead of hardcoding credentials",
			FileTypes:   []string{".go", ".yaml", ".yml", ".json", ".env"},
		},
		{
			ID:          "AB002", 
			Name:        "Salesforce Token Exposure",
			PatternStr:  `(?i)security_token\s*[:=]\s*["'][^"']{15,}["']`,
			Level:       SecurityLevelHigh,
			Category:    "credential_exposure",
			Description: "Hardcoded Salesforce security token detected",
			Remediation: "Use environment variables or secure credential storage for Salesforce tokens",
			FileTypes:   []string{".go", ".yaml", ".yml", ".json"},
		},
		{
			ID:          "AB003",
			Name:        "Insecure S3 Endpoint",
			PatternStr:  `endpoint\s*[:=]\s*["']http://[^"']+["']`,
			Level:       SecurityLevelMedium,
			Category:    "insecure_transport",
			Description: "Insecure HTTP endpoint for S3 operations",
			Remediation: "Use HTTPS endpoints for all S3 operations to ensure encryption in transit",
			FileTypes:   []string{".go", ".yaml", ".yml", ".json"},
		},
		{
			ID:          "AB004",
			Name:        "Weak Encryption Configuration",
			PatternStr:  `(?i)(encryption|cipher)\s*[:=]\s*["']?(none|null|false|disabled?)["']?`,
			Level:       SecurityLevelHigh,
			Category:    "weak_crypto",
			Description: "Encryption is disabled or set to weak configuration",
			Remediation: "Enable strong encryption for data at rest and in transit",
			FileTypes:   []string{".go", ".yaml", ".yml", ".json"},
		},
		{
			ID:          "AB005",
			Name:        "Debug Mode in Production",
			PatternStr:  `(?i)(debug|log_level)\s*[:=]\s*["']?(debug|trace)["']?`,
			Level:       SecurityLevelMedium,
			Category:    "information_disclosure",
			Description: "Debug logging enabled which may expose sensitive information",
			Remediation: "Set log level to 'info' or 'warn' in production environments",
			FileTypes:   []string{".go", ".yaml", ".yml", ".json"},
		},
		{
			ID:          "AB006",
			Name:        "Unsafe File Permissions",
			PatternStr:  `(?i)(chmod|permission|mode)\s*[:=]\s*["']?(777|666|755)["']?`,
			Level:       SecurityLevelMedium,
			Category:    "file_permissions",
			Description: "Potentially unsafe file permissions detected",
			Remediation: "Use more restrictive file permissions, especially for sensitive files",
			FileTypes:   []string{".go", ".yaml", ".yml"},
		},
		{
			ID:          "AB007",
			Name:        "Missing TLS Configuration",
			PatternStr:  `(?i)(tls|ssl)\s*[:=]\s*["']?(false|disabled?|off)["']?`,
			Level:       SecurityLevelHigh,
			Category:    "insecure_transport",
			Description: "TLS/SSL is disabled for secure communications",
			Remediation: "Enable TLS for all network communications",
			FileTypes:   []string{".go", ".yaml", ".yml", ".json"},
		},
	}
}

// matchesFileType checks if file matches any of the specified types
func (s *StaticAnalysisScanner) matchesFileType(filePath string, fileTypes []string) bool {
	if len(fileTypes) == 0 {
		return true // Match all files if no types specified
	}

	ext := filepath.Ext(filePath)
	for _, fileType := range fileTypes {
		if ext == fileType {
			return true
		}
	}
	return false
}

// isExcluded checks if a file path should be excluded from scanning
func (s *StaticAnalysisScanner) isExcluded(filePath string, excludes []string) bool {
	for _, exclude := range excludes {
		if strings.Contains(filePath, exclude) {
			return true
		}
	}
	
	// Default exclusions
	defaultExcludes := []string{
		"vendor/",
		".git/",
		"node_modules/",
		"coverage.out",
		"coverage.html",
		"bin/",
		".DS_Store",
		"__pycache__/",
		"*.pyc",
		"*.pyo",
		"*.tmp",
		"*.log",
	}
	
	for _, exclude := range defaultExcludes {
		if strings.Contains(filePath, exclude) {
			return true
		}
	}
	
	return false
}

// applyRuleToFile applies a custom security rule to a specific file
func (s *StaticAnalysisScanner) applyRuleToFile(rule CustomSecurityRule, filePath string) ([]SecurityFinding, error) {
	// Compile pattern if not already compiled
	if rule.Pattern == nil {
		pattern, err := regexp.Compile(rule.PatternStr)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern for rule %s: %w", rule.ID, err)
		}
		rule.Pattern = pattern
	}

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", filePath, err)
	}

	var findings []SecurityFinding
	lines := strings.Split(string(content), "\n")

	for lineNum, line := range lines {
		matches := rule.Pattern.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			finding := SecurityFinding{
				ID:          uuid.New().String(),
				Level:       rule.Level,
				Title:       rule.Name,
				Description: rule.Description,
				Category:    rule.Category,
				File:        filePath,
				Line:        lineNum + 1, // 1-based line numbering
				Rule:        rule.ID,
				Remediation: rule.Remediation,
				Timestamp:   time.Now(),
				Scanner:     "agentbrain_custom",
			}
			findings = append(findings, finding)
		}
	}

	return findings, nil
}