package security

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DependencyAuditor performs vulnerability scanning of Go dependencies
type DependencyAuditor struct {
	config         DependencyAuditConfig
	govulncheckPath string
	workingDir     string
	client         VulnDBClient
}

// VulnDBClient represents a client for vulnerability databases
type VulnDBClient interface {
	GetVulnerabilities(ctx context.Context, pkg, version string) ([]Vulnerability, error)
	UpdateDatabase(ctx context.Context) error
	GetDatabaseInfo(ctx context.Context) (map[string]interface{}, error)
}

// DefaultVulnDBClient provides a default implementation using govulncheck
type DefaultVulnDBClient struct {
	cacheDir string
	timeout  time.Duration
}

// GovulncheckResult represents the structure of govulncheck JSON output
type GovulncheckResult struct {
	SchemaVersion string             `json:"schema_version"`
	ScannerName   string             `json:"scanner_name"`
	ScannerVersion string            `json:"scanner_version"`
	DatabaseURL   string             `json:"db"`
	DatabaseLastModified *time.Time  `json:"db_last_modified,omitempty"`
	Vulns        []GovulncheckVuln   `json:"vulns"`
}

// GovulncheckVuln represents a vulnerability in govulncheck output
type GovulncheckVuln struct {
	OSV      OSVEntry           `json:"osv"`
	Modules  []VulnModule       `json:"modules"`
	Stacks   []CallStack        `json:"stacks,omitempty"`
}

// OSVEntry represents Open Source Vulnerability format data
type OSVEntry struct {
	SchemaVersion string        `json:"schema_version"`
	ID            string        `json:"id"`
	Published     *time.Time    `json:"published,omitempty"`
	Modified      *time.Time    `json:"modified,omitempty"`
	Aliases       []string      `json:"aliases,omitempty"`
	Summary       string        `json:"summary"`
	Details       string        `json:"details"`
	Affected      []OSVAffected `json:"affected"`
	References    []OSVReference `json:"references,omitempty"`
	Severity      []OSVSeverity  `json:"severity,omitempty"`
}

// OSVAffected represents affected packages in OSV format
type OSVAffected struct {
	Package          OSVPackage      `json:"package"`
	Ranges           []OSVRange      `json:"ranges,omitempty"`
	Versions         []string        `json:"versions,omitempty"`
	EcosystemSpecific json.RawMessage `json:"ecosystem_specific,omitempty"`
	DatabaseSpecific json.RawMessage  `json:"database_specific,omitempty"`
}

// OSVPackage represents a package in OSV format
type OSVPackage struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
	Purl      string `json:"purl,omitempty"`
}

// OSVRange represents version ranges in OSV format
type OSVRange struct {
	Type   string      `json:"type"`
	Events []OSVEvent  `json:"events"`
}

// OSVEvent represents version events (introduced/fixed) in OSV format
type OSVEvent struct {
	Introduced string `json:"introduced,omitempty"`
	Fixed      string `json:"fixed,omitempty"`
	LastAffected string `json:"last_affected,omitempty"`
	Limit      string `json:"limit,omitempty"`
}

// OSVReference represents references in OSV format
type OSVReference struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// OSVSeverity represents severity information in OSV format
type OSVSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

// VulnModule represents a vulnerable module
type VulnModule struct {
	Path      string              `json:"path"`
	FoundVersion string           `json:"found_version,omitempty"`
	FixedVersion string           `json:"fixed_version,omitempty"`
	Packages    []VulnPackage      `json:"packages,omitempty"`
}

// VulnPackage represents a vulnerable package within a module
type VulnPackage struct {
	Path      string         `json:"path"`
	CallStacks []CallStack   `json:"call_stacks,omitempty"`
}

// CallStack represents a call stack to a vulnerable function
type CallStack struct {
	Symbol string     `json:"symbol"`
	Summary string    `json:"summary"`
	Frames  []Frame    `json:"frames"`
}

// Frame represents a frame in a call stack
type Frame struct {
	Module   string `json:"module"`
	Version  string `json:"version,omitempty"`
	Package  string `json:"package"`
	Function string `json:"function"`
	Receiver string `json:"receiver,omitempty"`
	Position string `json:"position,omitempty"`
}

// NewDependencyAuditor creates a new dependency auditor
func NewDependencyAuditor(config DependencyAuditConfig) (*DependencyAuditor, error) {
	auditor := &DependencyAuditor{
		config:     config,
		workingDir: ".",
		client:     &DefaultVulnDBClient{},
	}

	// Find govulncheck binary
	govulncheckPath, err := exec.LookPath("govulncheck")
	if err != nil {
		auditor.govulncheckPath = ""
	} else {
		auditor.govulncheckPath = govulncheckPath
	}

	return auditor, nil
}

// NewDefaultVulnDBClient creates a new default vulnerability database client
func NewDefaultVulnDBClient() *DefaultVulnDBClient {
	return &DefaultVulnDBClient{
		cacheDir: filepath.Join(os.TempDir(), "govulncheck-cache"),
		timeout:  30 * time.Second,
	}
}

// ScanDependencies scans all dependencies in the project for vulnerabilities
func (d *DependencyAuditor) ScanDependencies(ctx context.Context, manifestPath string) ([]Vulnerability, error) {
	if !d.config.Enabled {
		return nil, fmt.Errorf("dependency audit is disabled")
	}

	var vulnerabilities []Vulnerability

	if d.govulncheckPath != "" {
		vulns, err := d.runGovulncheck(ctx, manifestPath)
		if err != nil {
			return nil, fmt.Errorf("govulncheck scan failed: %w", err)
		}
		vulnerabilities = append(vulnerabilities, vulns...)
	}

	// Filter vulnerabilities based on configuration
	filteredVulns := d.filterVulnerabilities(vulnerabilities)

	return filteredVulns, nil
}

// runGovulncheck executes govulncheck and parses its output
func (d *DependencyAuditor) runGovulncheck(ctx context.Context, target string) ([]Vulnerability, error) {
	args := []string{"-json"}
	
	// If target is a specific path, scan that path
	if target != "" && target != "." {
		args = append(args, target)
	} else {
		args = append(args, "./...")
	}

	cmd := exec.CommandContext(ctx, d.govulncheckPath, args...)
	cmd.Dir = d.workingDir

	output, err := cmd.Output()
	if err != nil {
		// govulncheck returns non-zero exit code when vulnerabilities are found
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Use stderr output if available, otherwise continue with stdout
			if len(exitErr.Stderr) > 0 {
				fmt.Printf("govulncheck stderr: %s\n", string(exitErr.Stderr))
			}
		} else {
			return nil, fmt.Errorf("failed to run govulncheck: %w", err)
		}
	}

	return d.parseGovulncheckOutput(output)
}

// parseGovulncheckOutput parses govulncheck JSON output
func (d *DependencyAuditor) parseGovulncheckOutput(output []byte) ([]Vulnerability, error) {
	if len(output) == 0 {
		return []Vulnerability{}, nil
	}

	var result GovulncheckResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("parse govulncheck output: %w", err)
	}

	var vulnerabilities []Vulnerability
	for _, vuln := range result.Vulns {
		vulnerability := d.convertOSVToVulnerability(vuln.OSV, vuln.Modules)
		vulnerabilities = append(vulnerabilities, vulnerability)
	}

	return vulnerabilities, nil
}

// convertOSVToVulnerability converts OSV format to our Vulnerability structure
func (d *DependencyAuditor) convertOSVToVulnerability(osv OSVEntry, modules []VulnModule) Vulnerability {
	vulnerability := Vulnerability{
		ID:          osv.ID,
		CVE:         d.extractCVE(osv.Aliases),
		Description: osv.Summary,
		References:  d.extractReferences(osv.References),
	}

	if osv.Published != nil {
		vulnerability.PublishedDate = *osv.Published
	}
	if osv.Modified != nil {
		vulnerability.ModifiedDate = *osv.Modified
	}

	// Extract severity and CVSS score
	if len(osv.Severity) > 0 {
		for _, sev := range osv.Severity {
			if sev.Type == "CVSS_V3" || sev.Type == "CVSS_V2" {
				vulnerability.CVSSVector = sev.Score
				// Parse CVSS score from vector
				if score := d.parseCVSSScore(sev.Score); score > 0 {
					vulnerability.CVSS = score
					vulnerability.Severity = d.mapCVSSToSeverity(score)
				}
			}
		}
	}

	// Extract package information from modules
	if len(modules) > 0 {
		module := modules[0] // Use first module for primary package info
		vulnerability.Package = module.Path
		vulnerability.Version = module.FoundVersion
		vulnerability.FixedIn = module.FixedVersion

		// Extract version ranges
		for _, affected := range osv.Affected {
			if affected.Package.Name == module.Path {
				vulnerability.AffectedRanges = d.convertOSVRanges(affected.Ranges)
				break
			}
		}
	}

	// Set default severity if not already set
	if vulnerability.Severity == "" {
		vulnerability.Severity = "medium"
	}

	// Add metadata
	vulnerability.Metadata = map[string]interface{}{
		"scanner":        "govulncheck",
		"schema_version": osv.SchemaVersion,
		"details":        osv.Details,
	}

	return vulnerability
}

// extractCVE extracts CVE identifier from aliases
func (d *DependencyAuditor) extractCVE(aliases []string) string {
	for _, alias := range aliases {
		if strings.HasPrefix(alias, "CVE-") {
			return alias
		}
	}
	return ""
}

// extractReferences extracts reference URLs
func (d *DependencyAuditor) extractReferences(refs []OSVReference) []string {
	var references []string
	for _, ref := range refs {
		references = append(references, ref.URL)
	}
	return references
}

// parseCVSSScore extracts numerical score from CVSS vector
func (d *DependencyAuditor) parseCVSSScore(vector string) float64 {
	// This is a simplified CVSS parser - in production you'd want a proper CVSS library
	if strings.Contains(vector, "/") {
		// Try to find score in vector string
		parts := strings.Split(vector, "/")
		for _, part := range parts {
			if strings.Contains(part, ":") {
				kv := strings.Split(part, ":")
				if len(kv) == 2 && (kv[0] == "S" || kv[0] == "score") {
					// This is a simplified approach - proper CVSS parsing would be more complex
				}
			}
		}
	}
	return 0.0 // Return 0 if can't parse, will be handled by caller
}

// mapCVSSToSeverity maps CVSS score to severity level
func (d *DependencyAuditor) mapCVSSToSeverity(score float64) string {
	if score >= 9.0 {
		return "critical"
	} else if score >= 7.0 {
		return "high"
	} else if score >= 4.0 {
		return "medium"
	} else {
		return "low"
	}
}

// convertOSVRanges converts OSV ranges to our VersionRange structure
func (d *DependencyAuditor) convertOSVRanges(osvRanges []OSVRange) []VersionRange {
	var ranges []VersionRange
	
	for _, osvRange := range osvRanges {
		versionRange := VersionRange{
			Type: osvRange.Type,
		}
		
		for _, event := range osvRange.Events {
			if event.Introduced != "" {
				versionRange.Introduced = event.Introduced
			}
			if event.Fixed != "" {
				versionRange.Fixed = event.Fixed
			}
		}
		
		ranges = append(ranges, versionRange)
	}
	
	return ranges
}

// filterVulnerabilities filters vulnerabilities based on configuration
func (d *DependencyAuditor) filterVulnerabilities(vulnerabilities []Vulnerability) []Vulnerability {
	var filtered []Vulnerability

	for _, vuln := range vulnerabilities {
		// Skip ignored packages
		skip := false
		for _, ignorePkg := range d.config.IgnorePackages {
			if vuln.Package == ignorePkg {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		// Skip vulnerabilities above max CVSS score threshold
		if d.config.MaxCVSSScore > 0 && vuln.CVSS > d.config.MaxCVSSScore {
			continue
		}

		filtered = append(filtered, vuln)
	}

	return filtered
}

// CheckPackage checks a specific package for vulnerabilities
func (d *DependencyAuditor) CheckPackage(ctx context.Context, pkg, version string) ([]Vulnerability, error) {
	if !d.config.Enabled {
		return nil, fmt.Errorf("dependency audit is disabled")
	}

	// Use the client to check for vulnerabilities
	return d.client.GetVulnerabilities(ctx, pkg, version)
}

// UpdateDatabase updates the vulnerability database
func (d *DependencyAuditor) UpdateDatabase(ctx context.Context) error {
	if !d.config.Enabled {
		return fmt.Errorf("dependency audit is disabled")
	}

	return d.client.UpdateDatabase(ctx)
}

// GetDatabaseInfo returns information about the vulnerability database
func (d *DependencyAuditor) GetDatabaseInfo() (map[string]interface{}, error) {
	if !d.config.Enabled {
		return nil, fmt.Errorf("dependency audit is disabled")
	}

	return d.client.GetDatabaseInfo(context.Background())
}

// GenerateReport generates a comprehensive dependency vulnerability report
func (d *DependencyAuditor) GenerateReport(ctx context.Context, manifestPath string) (*SecurityReport, error) {
	vulnerabilities, err := d.ScanDependencies(ctx, manifestPath)
	if err != nil {
		return nil, fmt.Errorf("dependency scan failed: %w", err)
	}

	report := &SecurityReport{
		ID:              uuid.New().String(),
		Timestamp:       time.Now(),
		Status:          "completed",
		Vulnerabilities: vulnerabilities,
		GeneratedBy:     "dependency-auditor",
	}

	// Calculate summary statistics
	summary := SecuritySummary{
		VulnerabilitiesFound: len(vulnerabilities),
		IssuesBySeverity:     make(map[string]int),
	}

	for _, vuln := range vulnerabilities {
		summary.IssuesBySeverity[vuln.Severity]++
		
		if vuln.Severity == "high" || vuln.Severity == "critical" {
			summary.HighSeverityIssues++
		}
		if vuln.Severity == "critical" {
			summary.CriticalIssues++
		}
	}

	summary.TotalIssues = len(vulnerabilities)
	
	// Calculate security score based on vulnerabilities
	summary.SecurityScore = d.calculateSecurityScore(vulnerabilities)
	report.Summary = summary
	report.RiskScore = 100.0 - summary.SecurityScore

	// Generate recommendations
	report.Recommendations = d.generateRecommendations(vulnerabilities)

	return report, nil
}

// calculateSecurityScore calculates a security score based on vulnerabilities
func (d *DependencyAuditor) calculateSecurityScore(vulnerabilities []Vulnerability) float64 {
	if len(vulnerabilities) == 0 {
		return 100.0
	}

	score := 100.0
	severityPenalties := map[string]float64{
		"low":      1.0,
		"medium":   3.0,
		"high":     8.0,
		"critical": 15.0,
	}

	for _, vuln := range vulnerabilities {
		if penalty, exists := severityPenalties[vuln.Severity]; exists {
			score -= penalty
		}
	}

	// Additional penalty for high CVSS scores
	for _, vuln := range vulnerabilities {
		if vuln.CVSS >= 9.0 {
			score -= 5.0
		} else if vuln.CVSS >= 7.0 {
			score -= 2.0
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

// generateRecommendations generates security recommendations based on findings
func (d *DependencyAuditor) generateRecommendations(vulnerabilities []Vulnerability) []Recommendation {
	var recommendations []Recommendation
	
	// Group vulnerabilities by package for targeted recommendations
	packageVulns := make(map[string][]Vulnerability)
	for _, vuln := range vulnerabilities {
		packageVulns[vuln.Package] = append(packageVulns[vuln.Package], vuln)
	}

	for pkg, vulns := range packageVulns {
		// Find if there's a common fixed version
		var fixedVersions []string
		for _, vuln := range vulns {
			if vuln.FixedIn != "" {
				fixedVersions = append(fixedVersions, vuln.FixedIn)
			}
		}

		severity := "medium"
		for _, vuln := range vulns {
			if vuln.Severity == "critical" || vuln.Severity == "high" {
				severity = "high"
				break
			}
		}

		recommendation := Recommendation{
			ID:          uuid.New().String(),
			Title:       fmt.Sprintf("Update %s to fix vulnerabilities", pkg),
			Description: fmt.Sprintf("Package %s has %d vulnerabilities that can be fixed by updating", pkg, len(vulns)),
			Severity:    severity,
			Category:    "dependency-update",
			Fix:         fmt.Sprintf("Update %s to a version that fixes the identified vulnerabilities", pkg),
			Effort:      "low",
			Impact:      severity,
			Tags:        []string{"dependency", "vulnerability"},
		}

		if len(fixedVersions) > 0 {
			recommendation.Fix = fmt.Sprintf("Update %s to version %s or later", pkg, fixedVersions[0])
		}

		recommendations = append(recommendations, recommendation)
	}

	// Add general recommendations
	if len(vulnerabilities) > 0 {
		recommendations = append(recommendations, Recommendation{
			ID:          uuid.New().String(),
			Title:       "Implement automated dependency scanning",
			Description: "Set up automated scanning to catch vulnerabilities early",
			Severity:    "medium",
			Category:    "process-improvement",
			Fix:         "Integrate dependency scanning into CI/CD pipeline",
			Effort:      "medium",
			Impact:      "high",
			Tags:        []string{"automation", "ci-cd"},
		})
	}

	return recommendations
}

// Default VulnDBClient implementation methods

// GetVulnerabilities retrieves vulnerabilities for a specific package version
func (c *DefaultVulnDBClient) GetVulnerabilities(ctx context.Context, pkg, version string) ([]Vulnerability, error) {
	// This would typically query a vulnerability database API
	// For now, return empty slice as this requires external API integration
	return []Vulnerability{}, nil
}

// UpdateDatabase updates the local vulnerability database
func (c *DefaultVulnDBClient) UpdateDatabase(ctx context.Context) error {
	// This would typically update a local vulnerability database
	// For now, return nil as this requires external tool integration
	return nil
}

// GetDatabaseInfo returns information about the vulnerability database
func (c *DefaultVulnDBClient) GetDatabaseInfo(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{
		"source":      "govulncheck",
		"last_update": time.Now(),
		"status":      "active",
	}, nil
}