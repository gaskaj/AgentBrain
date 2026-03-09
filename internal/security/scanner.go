package security

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SecurityScanner performs static security analysis using gosec and custom rules
type SecurityScanner struct {
	config       StaticAnalysisConfig
	customRules  []Rule
	gosecPath    string
	workingDir   string
}

// GosecResult represents a finding from gosec
type GosecResult struct {
	Severity   string `json:"severity"`
	Confidence string `json:"confidence"`
	RuleID     string `json:"rule_id"`
	Details    string `json:"details"`
	File       string `json:"file"`
	Code       string `json:"code"`
	Line       string `json:"line"`
	Column     string `json:"column"`
}

// GosecIssue represents the structure of gosec JSON output
type GosecIssue struct {
	Severity   string `json:"severity"`
	Confidence string `json:"confidence"`
	RuleID     string `json:"rule_id"`
	Details    string `json:"details"`
	File       string `json:"file"`
	Code       string `json:"code"`
	Line       string `json:"line"`
	Column     string `json:"column"`
	Nosec      bool   `json:"nosec"`
	Suppressions []string `json:"suppressions"`
}

// GosecOutput represents the complete gosec JSON output structure
type GosecOutput struct {
	Issues []GosecIssue          `json:"Issues"`
	Stats  map[string]interface{} `json:"Stats"`
	GosecVersion string           `json:"GosecVersion"`
}

// NewSecurityScanner creates a new static security scanner
func NewSecurityScanner(config StaticAnalysisConfig) (*SecurityScanner, error) {
	// Convert config rules to security rules
	var customRules []Rule
	for _, configRule := range config.CustomRules {
		rule := Rule{
			ID:          configRule.ID,
			Name:        configRule.Name,
			Description: configRule.Description,
			Pattern:     configRule.Pattern,
			Severity:    configRule.Severity,
			CWE:         configRule.CWE,
			Fix:         configRule.Fix,
			Tags:        configRule.Tags,
			Metadata:    configRule.Metadata,
		}
		customRules = append(customRules, rule)
	}

	scanner := &SecurityScanner{
		config:      config,
		customRules: customRules,
		workingDir:  ".",
	}

	// Find gosec binary
	gosecPath, err := exec.LookPath("gosec")
	if err != nil {
		// If gosec is not installed, we'll operate in limited mode
		scanner.gosecPath = ""
	} else {
		scanner.gosecPath = gosecPath
	}

	return scanner, nil
}

// Scan performs comprehensive security scanning on the target directory
func (s *SecurityScanner) Scan(ctx context.Context, target string) ([]ScanResult, error) {
	if !s.config.Enabled {
		return nil, fmt.Errorf("static analysis is disabled")
	}

	var allResults []ScanResult

	// Run gosec analysis if available
	if s.gosecPath != "" {
		gosecResults, err := s.runGosec(ctx, target)
		if err != nil {
			// Continue with custom rules even if gosec fails
			fmt.Printf("Warning: gosec analysis failed: %v\n", err)
		} else {
			allResults = append(allResults, gosecResults...)
		}
	}

	// Run custom rule analysis
	customResults, err := s.runCustomRules(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("custom rules analysis failed: %w", err)
	}
	allResults = append(allResults, customResults...)

	// Filter results based on configuration
	filteredResults := s.filterResults(allResults)

	return filteredResults, nil
}

// runGosec executes gosec and parses its output
func (s *SecurityScanner) runGosec(ctx context.Context, target string) ([]ScanResult, error) {
	args := []string{"-fmt", "json", "-out", "-"}
	
	// Add excluded rules
	for _, rule := range s.config.ExcludeRules {
		args = append(args, "-exclude", rule)
	}
	
	// Add skip directories
	for _, dir := range s.config.SkipDirectories {
		args = append(args, "-skip", dir)
	}
	
	args = append(args, target)

	cmd := exec.CommandContext(ctx, s.gosecPath, args...)
	cmd.Dir = s.workingDir

	output, err := cmd.Output()
	if err != nil {
		// gosec returns non-zero exit code when issues are found
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Use stderr output if available
			if len(exitErr.Stderr) > 0 {
				output = exitErr.Stderr
			} else {
				output = []byte(exitErr.String())
			}
		} else {
			return nil, fmt.Errorf("failed to run gosec: %w", err)
		}
	}

	return s.parseGosecOutput(output)
}

// parseGosecOutput parses gosec JSON output into ScanResult structures
func (s *SecurityScanner) parseGosecOutput(output []byte) ([]ScanResult, error) {
	if len(output) == 0 {
		return []ScanResult{}, nil
	}

	var gosecOutput GosecOutput
	if err := json.Unmarshal(output, &gosecOutput); err != nil {
		// If JSON parsing fails, try to extract plain text results
		return s.parseGosecPlainText(string(output))
	}

	var results []ScanResult
	for _, issue := range gosecOutput.Issues {
		if issue.Nosec {
			continue // Skip suppressed issues
		}

		lineNum, _ := strconv.Atoi(issue.Line)
		colNum, _ := strconv.Atoi(issue.Column)

		result := ScanResult{
			RuleID:      issue.RuleID,
			Severity:    strings.ToLower(issue.Severity),
			Rule:        issue.RuleID,
			File:        issue.File,
			Line:        lineNum,
			Column:      colNum,
			Description: issue.Details,
			Evidence:    issue.Code,
			Confidence:  strings.ToLower(issue.Confidence),
			Tags:        []string{"gosec"},
		}

		// Map gosec rule IDs to CWE identifiers
		if cwe := s.mapGosecToCWE(issue.RuleID); cwe != "" {
			result.CWE = cwe
		}

		results = append(results, result)
	}

	return results, nil
}

// parseGosecPlainText parses plain text gosec output as fallback
func (s *SecurityScanner) parseGosecPlainText(output string) ([]ScanResult, error) {
	var results []ScanResult
	lines := strings.Split(output, "\n")
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Try to parse basic information from plain text output
		// This is a simplified parser for fallback scenarios
		if strings.Contains(line, "[") && strings.Contains(line, "]") {
			result := ScanResult{
				RuleID:      "UNKNOWN",
				Severity:    "medium",
				Rule:        "gosec-plaintext",
				Description: line,
				Tags:        []string{"gosec", "fallback"},
			}
			results = append(results, result)
		}
	}

	return results, nil
}

// runCustomRules executes custom security rules against the target
func (s *SecurityScanner) runCustomRules(ctx context.Context, target string) ([]ScanResult, error) {
	var results []ScanResult

	if len(s.customRules) == 0 {
		return results, nil
	}

	err := filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-Go files
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip if in excluded directories
		for _, skipDir := range s.config.SkipDirectories {
			if strings.Contains(path, skipDir) {
				return nil
			}
		}

		fileResults, err := s.scanFileWithCustomRules(path)
		if err != nil {
			// Log error but continue processing other files
			fmt.Printf("Warning: failed to scan file %s with custom rules: %v\n", path, err)
			return nil
		}

		results = append(results, fileResults...)
		return nil
	})

	return results, err
}

// scanFileWithCustomRules scans a single file using custom rules
func (s *SecurityScanner) scanFileWithCustomRules(filePath string) ([]ScanResult, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", filePath, err)
	}

	var results []ScanResult
	fileContent := string(content)
	lines := strings.Split(fileContent, "\n")

	for _, rule := range s.customRules {
		matches := s.findRuleMatches(rule, fileContent, lines)
		for _, match := range matches {
			result := ScanResult{
				RuleID:      rule.ID,
				Severity:    strings.ToLower(rule.Severity),
				Rule:        rule.Name,
				File:        filePath,
				Line:        match.Line,
				Column:      match.Column,
				Description: rule.Description,
				CWE:         rule.CWE,
				Fix:         rule.Fix,
				Evidence:    match.Evidence,
				Tags:        append(rule.Tags, "custom-rule"),
				Metadata:    rule.Metadata,
			}
			results = append(results, result)
		}
	}

	return results, nil
}

// RuleMatch represents a match found by a custom rule
type RuleMatch struct {
	Line     int
	Column   int
	Evidence string
}

// findRuleMatches finds all matches for a rule in the given content
func (s *SecurityScanner) findRuleMatches(rule Rule, content string, lines []string) []RuleMatch {
	var matches []RuleMatch

	// Compile the pattern as a regular expression
	regex, err := regexp.Compile(rule.Pattern)
	if err != nil {
		fmt.Printf("Warning: invalid pattern in rule %s: %v\n", rule.ID, err)
		return matches
	}

	// Find all matches in the content
	allMatches := regex.FindAllStringSubmatchIndex(content, -1)
	
	for _, match := range allMatches {
		if len(match) < 2 {
			continue
		}

		// Calculate line and column numbers
		line, column := s.getLineAndColumn(content, match[0])
		evidence := ""
		if line > 0 && line <= len(lines) {
			evidence = strings.TrimSpace(lines[line-1])
		}

		matches = append(matches, RuleMatch{
			Line:     line,
			Column:   column,
			Evidence: evidence,
		})
	}

	return matches
}

// getLineAndColumn calculates line and column numbers for a byte offset
func (s *SecurityScanner) getLineAndColumn(content string, offset int) (int, int) {
	if offset >= len(content) {
		return 0, 0
	}

	line := 1
	column := 1
	
	for i := 0; i < offset; i++ {
		if content[i] == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}
	
	return line, column
}

// filterResults filters scan results based on configuration
func (s *SecurityScanner) filterResults(results []ScanResult) []ScanResult {
	var filtered []ScanResult

	for _, result := range results {
		// Skip excluded rules
		skip := false
		for _, excludedRule := range s.config.ExcludeRules {
			if result.RuleID == excludedRule {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		filtered = append(filtered, result)
	}

	return filtered
}

// mapGosecToCWE maps gosec rule IDs to Common Weakness Enumeration (CWE) identifiers
func (s *SecurityScanner) mapGosecToCWE(ruleID string) string {
	gosecToCWE := map[string]string{
		"G101": "CWE-798", // Hardcoded credentials
		"G102": "CWE-362", // Bind to all interfaces
		"G103": "CWE-242", // Audit the use of unsafe block
		"G104": "CWE-391", // Audit errors not checked
		"G105": "CWE-95",  // Audit the use of math/big.Int.Exp
		"G106": "CWE-322", // Audit the use of ssh.InsecureIgnoreHostKey
		"G107": "CWE-78",  // Url provided to HTTP request as taint input
		"G108": "CWE-22",  // Profiling endpoint automatically exposed
		"G109": "CWE-190", // Potential Integer overflow made by strconv.Atoi result conversion
		"G110": "CWE-409", // Potential DoS vulnerability via decompression bomb
		"G201": "CWE-89",  // SQL query construction using format string
		"G202": "CWE-89",  // SQL query construction using string concatenation
		"G203": "CWE-79",  // Use of unescaped data in HTML templates
		"G204": "CWE-78",  // Audit use of command execution
		"G301": "CWE-276", // Poor file permissions used when creating a directory
		"G302": "CWE-276", // Poor file permissions used when creation file or using chmod
		"G303": "CWE-377", // Creating tempfile using a predictable path
		"G304": "CWE-22",  // File path provided as taint input
		"G305": "CWE-22",  // File traversal when extracting zip archive
		"G306": "CWE-276", // Poor file permissions used when writing to a file
		"G401": "CWE-327", // Detect the usage of DES, RC4, MD5 or SHA1
		"G402": "CWE-295", // Look for bad TLS connection settings
		"G403": "CWE-310", // Ensure minimum RSA key length of 2048 bits
		"G404": "CWE-338", // Insecure random number source (rand)
		"G501": "CWE-327", // Import blocklist: crypto/md5
		"G502": "CWE-327", // Import blocklist: crypto/des
		"G503": "CWE-327", // Import blocklist: crypto/rc4
		"G504": "CWE-327", // Import blocklist: net/http/cgi
		"G505": "CWE-327", // Import blocklist: crypto/sha1
		"G601": "CWE-118", // Implicit memory aliasing in RangeStmt
	}

	return gosecToCWE[ruleID]
}

// ValidateConfig validates the scanner configuration
func (s *SecurityScanner) ValidateConfig() error {
	if !s.config.Enabled {
		return nil
	}

	// Validate custom rules
	for i, rule := range s.config.CustomRules {
		if rule.ID == "" {
			return fmt.Errorf("custom rule %d missing ID", i)
		}
		if rule.Name == "" {
			return fmt.Errorf("custom rule %s missing name", rule.ID)
		}
		if rule.Pattern == "" {
			return fmt.Errorf("custom rule %s missing pattern", rule.ID)
		}
		if rule.Severity == "" {
			return fmt.Errorf("custom rule %s missing severity", rule.ID)
		}

		// Validate severity level
		validSeverities := []string{"low", "medium", "high", "critical"}
		validSeverity := false
		for _, valid := range validSeverities {
			if strings.ToLower(rule.Severity) == valid {
				validSeverity = true
				break
			}
		}
		if !validSeverity {
			return fmt.Errorf("custom rule %s has invalid severity %s", rule.ID, rule.Severity)
		}

		// Validate pattern as regular expression
		if _, err := regexp.Compile(rule.Pattern); err != nil {
			return fmt.Errorf("custom rule %s has invalid pattern: %w", rule.ID, err)
		}
	}

	return nil
}

// GetSupportedRules returns a list of all supported security rules
func (s *SecurityScanner) GetSupportedRules() ([]Rule, error) {
	var rules []Rule

	// Add gosec rules if available
	if s.gosecPath != "" {
		gosecRules := s.getGosecRules()
		rules = append(rules, gosecRules...)
	}

	// Add custom rules
	rules = append(rules, s.config.CustomRules...)

	return rules, nil
}

// getGosecRules returns the built-in gosec rules
func (s *SecurityScanner) getGosecRules() []Rule {
	return []Rule{
		{ID: "G101", Name: "Hardcoded credentials", Description: "Look for hardcoded credentials", Severity: "HIGH", CWE: "CWE-798"},
		{ID: "G102", Name: "Bind to all interfaces", Description: "Bind to all interfaces", Severity: "MEDIUM", CWE: "CWE-362"},
		{ID: "G103", Name: "Audit use of unsafe block", Description: "Audit the use of unsafe block", Severity: "MEDIUM", CWE: "CWE-242"},
		{ID: "G104", Name: "Audit errors not checked", Description: "Audit errors not checked", Severity: "LOW", CWE: "CWE-391"},
		{ID: "G105", Name: "Audit use of math/big.Int.Exp", Description: "Audit the use of math/big.Int.Exp", Severity: "MEDIUM", CWE: "CWE-95"},
		{ID: "G106", Name: "Audit use of ssh.InsecureIgnoreHostKey", Description: "Audit the use of ssh.InsecureIgnoreHostKey", Severity: "HIGH", CWE: "CWE-322"},
		{ID: "G107", Name: "Url provided to HTTP request as taint input", Description: "Url provided to HTTP request as taint input", Severity: "MEDIUM", CWE: "CWE-78"},
		{ID: "G108", Name: "Profiling endpoint automatically exposed", Description: "Profiling endpoint automatically exposed", Severity: "HIGH", CWE: "CWE-22"},
		{ID: "G109", Name: "Potential Integer overflow", Description: "Potential Integer overflow made by strconv.Atoi result conversion", Severity: "MEDIUM", CWE: "CWE-190"},
		{ID: "G110", Name: "Potential DoS vulnerability", Description: "Potential DoS vulnerability via decompression bomb", Severity: "HIGH", CWE: "CWE-409"},
		{ID: "G201", Name: "SQL query construction using format string", Description: "SQL query construction using format string", Severity: "HIGH", CWE: "CWE-89"},
		{ID: "G202", Name: "SQL query construction using string concatenation", Description: "SQL query construction using string concatenation", Severity: "HIGH", CWE: "CWE-89"},
		{ID: "G203", Name: "Use of unescaped data in HTML templates", Description: "Use of unescaped data in HTML templates", Severity: "HIGH", CWE: "CWE-79"},
		{ID: "G204", Name: "Audit use of command execution", Description: "Audit use of command execution", Severity: "HIGH", CWE: "CWE-78"},
		{ID: "G301", Name: "Poor file permissions used when creating a directory", Description: "Poor file permissions used when creating a directory", Severity: "MEDIUM", CWE: "CWE-276"},
		{ID: "G302", Name: "Poor file permissions used when creation file", Description: "Poor file permissions used when creation file or using chmod", Severity: "MEDIUM", CWE: "CWE-276"},
		{ID: "G303", Name: "Creating tempfile using a predictable path", Description: "Creating tempfile using a predictable path", Severity: "MEDIUM", CWE: "CWE-377"},
		{ID: "G304", Name: "File path provided as taint input", Description: "File path provided as taint input", Severity: "HIGH", CWE: "CWE-22"},
		{ID: "G305", Name: "File traversal when extracting zip archive", Description: "File traversal when extracting zip archive", Severity: "HIGH", CWE: "CWE-22"},
		{ID: "G306", Name: "Poor file permissions used when writing to a file", Description: "Poor file permissions used when writing to a file", Severity: "MEDIUM", CWE: "CWE-276"},
		{ID: "G401", Name: "Detect the usage of weak cryptographic primitives", Description: "Detect the usage of DES, RC4, MD5 or SHA1", Severity: "HIGH", CWE: "CWE-327"},
		{ID: "G402", Name: "Look for bad TLS connection settings", Description: "Look for bad TLS connection settings", Severity: "HIGH", CWE: "CWE-295"},
		{ID: "G403", Name: "Ensure minimum RSA key length", Description: "Ensure minimum RSA key length of 2048 bits", Severity: "MEDIUM", CWE: "CWE-310"},
		{ID: "G404", Name: "Insecure random number source", Description: "Insecure random number source (rand)", Severity: "HIGH", CWE: "CWE-338"},
		{ID: "G501", Name: "Import blocklist: crypto/md5", Description: "Import blocklist: crypto/md5", Severity: "HIGH", CWE: "CWE-327"},
		{ID: "G502", Name: "Import blocklist: crypto/des", Description: "Import blocklist: crypto/des", Severity: "HIGH", CWE: "CWE-327"},
		{ID: "G503", Name: "Import blocklist: crypto/rc4", Description: "Import blocklist: crypto/rc4", Severity: "HIGH", CWE: "CWE-327"},
		{ID: "G504", Name: "Import blocklist: net/http/cgi", Description: "Import blocklist: net/http/cgi", Severity: "MEDIUM", CWE: "CWE-327"},
		{ID: "G505", Name: "Import blocklist: crypto/sha1", Description: "Import blocklist: crypto/sha1", Severity: "HIGH", CWE: "CWE-327"},
		{ID: "G601", Name: "Implicit memory aliasing in RangeStmt", Description: "Implicit memory aliasing in RangeStmt", Severity: "MEDIUM", CWE: "CWE-118"},
	}
}

// GenerateReport generates a comprehensive security report
func (s *SecurityScanner) GenerateReport(ctx context.Context, target string) (*SecurityReport, error) {
	results, err := s.Scan(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	report := &SecurityReport{
		ID:             uuid.New().String(),
		Timestamp:      time.Now(),
		Status:         "completed",
		StaticAnalysis: results,
		GeneratedBy:    "security-scanner",
	}

	// Calculate summary statistics
	summary := SecuritySummary{
		TotalIssues:      len(results),
		IssuesBySeverity: make(map[string]int),
	}

	for _, result := range results {
		summary.IssuesBySeverity[result.Severity]++
		
		if result.Severity == "high" || result.Severity == "critical" {
			summary.HighSeverityIssues++
		}
		if result.Severity == "critical" {
			summary.CriticalIssues++
		}
	}

	// Calculate security score (0-100, where 100 is most secure)
	summary.SecurityScore = s.calculateSecurityScore(results)
	report.Summary = summary
	report.RiskScore = 100.0 - summary.SecurityScore

	return report, nil
}

// calculateSecurityScore calculates a security score based on findings
func (s *SecurityScanner) calculateSecurityScore(results []ScanResult) float64 {
	if len(results) == 0 {
		return 100.0
	}

	score := 100.0
	severityPenalties := map[string]float64{
		"low":      0.5,
		"medium":   2.0,
		"high":     5.0,
		"critical": 10.0,
	}

	for _, result := range results {
		if penalty, exists := severityPenalties[result.Severity]; exists {
			score -= penalty
		}
	}

	// Ensure score is within bounds
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}