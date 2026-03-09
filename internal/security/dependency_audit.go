package security

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DependencyAuditScanner integrates with govulncheck for dependency vulnerability scanning
type DependencyAuditScanner struct {
	logger          *slog.Logger
	govulncheckPath string
	cacheDir        string
}

// GovulncheckFinding represents a vulnerability finding from govulncheck
type GovulncheckFinding struct {
	OSV          string   `json:"osv"`
	FixedVersion string   `json:"fixed_version"`
	Trace        []string `json:"trace"`
	Package      string   `json:"package"`
	Module       string   `json:"module"`
}

// GovulncheckOutput represents the complete govulncheck JSON output
type GovulncheckOutput struct {
	Vulns []struct {
		OSV string `json:"osv"`
		Modules []struct {
			Path         string `json:"path"`
			FixedVersion string `json:"fixed_version"`
			Packages     []struct {
				Path      string `json:"path"`
				CallStacks []struct {
					Summary string `json:"summary"`
					Frames  []struct {
						Function string `json:"function"`
						Package  string `json:"package"`
						Position struct {
							Filename string `json:"filename"`
							Line     int    `json:"line"`
							Column   int    `json:"column"`
						} `json:"position"`
					} `json:"frames"`
				} `json:"call_stacks"`
			} `json:"packages"`
		} `json:"modules"`
	} `json:"vulns"`
}

// CVEDatabase represents a local cache of CVE information
type CVEDatabase struct {
	cacheDir    string
	lastUpdated time.Time
	logger      *slog.Logger
}

// CVEEntry represents a single CVE entry
type CVEEntry struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	Severity    string    `json:"severity"`
	CVSS        float64   `json:"cvss"`
	Published   time.Time `json:"published"`
	Modified    time.Time `json:"modified"`
	References  []string  `json:"references"`
	Affected    []struct {
		Package   string `json:"package"`
		Ecosystem string `json:"ecosystem"`
		Ranges    []struct {
			Type   string `json:"type"`
			Events []struct {
				Introduced string `json:"introduced,omitempty"`
				Fixed      string `json:"fixed,omitempty"`
			} `json:"events"`
		} `json:"ranges"`
	} `json:"affected"`
}

// NewDependencyAuditScanner creates a new dependency audit scanner
func NewDependencyAuditScanner(logger *slog.Logger) *DependencyAuditScanner {
	homeDir, _ := os.UserHomeDir()
	cacheDir := filepath.Join(homeDir, ".agentbrain", "security", "vulndb")
	
	scanner := &DependencyAuditScanner{
		logger:          logger,
		govulncheckPath: findGovulncheck(),
		cacheDir:        cacheDir,
	}

	// Ensure cache directory exists
	os.MkdirAll(cacheDir, 0755)

	return scanner
}

// Name returns the scanner name
func (s *DependencyAuditScanner) Name() string {
	return "dependency_audit"
}

// Scan performs dependency vulnerability scanning
func (s *DependencyAuditScanner) Scan(ctx context.Context, config ScanConfig) ([]SecurityFinding, error) {
	s.logger.Info("Starting dependency vulnerability scan")
	startTime := time.Now()

	var allFindings []SecurityFinding

	// Run govulncheck if available
	if s.govulncheckPath != "" {
		govulnFindings, err := s.runGovulncheck(ctx, config.Paths)
		if err != nil {
			s.logger.Warn("Govulncheck scan failed", "error", err)
		} else {
			allFindings = append(allFindings, govulnFindings...)
		}
	} else {
		s.logger.Warn("Govulncheck not found, installing...")
		if err := s.installGovulncheck(ctx); err != nil {
			s.logger.Error("Failed to install govulncheck", "error", err)
			return nil, fmt.Errorf("govulncheck not available and installation failed: %w", err)
		}
		// Retry after installation
		if govulnFindings, err := s.runGovulncheck(ctx, config.Paths); err == nil {
			allFindings = append(allFindings, govulnFindings...)
		}
	}

	// Check go.mod for known vulnerable dependencies
	goModFindings, err := s.scanGoMod(ctx, config.Paths)
	if err != nil {
		s.logger.Warn("Go.mod scan failed", "error", err)
	} else {
		allFindings = append(allFindings, goModFindings...)
	}

	duration := time.Since(startTime)
	s.logger.Info("Dependency vulnerability scan completed", 
		"findings", len(allFindings), 
		"duration", duration)

	return allFindings, nil
}

// HealthCheck verifies the scanner can operate properly
func (s *DependencyAuditScanner) HealthCheck() error {
	// Check if Go is available
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("go command not found: %w", err)
	}

	// Check govulncheck availability
	if s.govulncheckPath == "" {
		return fmt.Errorf("govulncheck not found, will attempt to install during scan")
	}

	// Test govulncheck execution
	cmd := exec.Command(s.govulncheckPath, "-version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("govulncheck health check failed: %w", err)
	}

	return nil
}

// runGovulncheck executes govulncheck and parses its output
func (s *DependencyAuditScanner) runGovulncheck(ctx context.Context, paths []string) ([]SecurityFinding, error) {
	var findings []SecurityFinding

	// If no paths specified, scan current directory
	if len(paths) == 0 {
		paths = []string{"."}
	}

	for _, path := range paths {
		pathFindings, err := s.scanPathWithGovulncheck(ctx, path)
		if err != nil {
			s.logger.Warn("Govulncheck scan failed for path", "path", path, "error", err)
			continue
		}
		findings = append(findings, pathFindings...)
	}

	return findings, nil
}

// scanPathWithGovulncheck scans a specific path with govulncheck
func (s *DependencyAuditScanner) scanPathWithGovulncheck(ctx context.Context, path string) ([]SecurityFinding, error) {
	args := []string{
		"-json",
		"-C", path, // Change to directory
		"./...",
	}

	cmd := exec.CommandContext(ctx, s.govulncheckPath, args...)
	output, err := cmd.Output()
	if err != nil {
		// govulncheck returns non-zero exit code when vulnerabilities are found
		if len(output) == 0 {
			return nil, fmt.Errorf("govulncheck execution failed: %w", err)
		}
	}

	// Parse each line as a JSON object (govulncheck uses JSON Lines format)
	lines := strings.Split(string(output), "\n")
	var findings []SecurityFinding

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			continue // Skip malformed lines
		}

		// Look for vulnerability entries
		if result["osv"] != nil {
			finding := s.convertGovulncheckFinding(result, path)
			if finding != nil {
				findings = append(findings, *finding)
			}
		}
	}

	return findings, nil
}

// convertGovulncheckFinding converts govulncheck output to SecurityFinding format
func (s *DependencyAuditScanner) convertGovulncheckFinding(result map[string]interface{}, basePath string) *SecurityFinding {
	osvID, ok := result["osv"].(string)
	if !ok {
		return nil
	}

	// Extract basic vulnerability information
	var packageName, moduleName, fixedVersion string
	var callStack []string

	if modules, ok := result["modules"].([]interface{}); ok && len(modules) > 0 {
		if mod, ok := modules[0].(map[string]interface{}); ok {
			if path, ok := mod["path"].(string); ok {
				moduleName = path
			}
			if fixed, ok := mod["fixed_version"].(string); ok {
				fixedVersion = fixed
			}
			if packages, ok := mod["packages"].([]interface{}); ok && len(packages) > 0 {
				if pkg, ok := packages[0].(map[string]interface{}); ok {
					if path, ok := pkg["path"].(string); ok {
						packageName = path
					}
					// Extract call stack information
					if callStacks, ok := pkg["call_stacks"].([]interface{}); ok {
						for _, cs := range callStacks {
							if csMap, ok := cs.(map[string]interface{}); ok {
								if summary, ok := csMap["summary"].(string); ok {
									callStack = append(callStack, summary)
								}
							}
						}
					}
				}
			}
		}
	}

	level := s.determineVulnerabilityLevel(osvID)
	
	description := fmt.Sprintf("Vulnerability %s found in %s", osvID, packageName)
	if moduleName != "" {
		description += fmt.Sprintf(" (module: %s)", moduleName)
	}

	remediation := "Update to a fixed version"
	if fixedVersion != "" {
		remediation = fmt.Sprintf("Update %s to version %s or later", packageName, fixedVersion)
	}

	finding := SecurityFinding{
		ID:          uuid.New().String(),
		Level:       level,
		Title:       fmt.Sprintf("Vulnerability: %s", osvID),
		Description: description,
		Category:    "dependency_vulnerability",
		Rule:        osvID,
		Remediation: remediation,
		References:  []string{fmt.Sprintf("https://osv.dev/vulnerability/%s", osvID)},
		Timestamp:   time.Now(),
		Scanner:     "govulncheck",
	}

	return &finding
}

// determineVulnerabilityLevel determines the security level based on CVE/OSV ID
func (s *DependencyAuditScanner) determineVulnerabilityLevel(osvID string) SecurityLevel {
	// This is a simplified implementation
	// In a full implementation, you would query the OSV database for CVSS scores
	
	// Some heuristics based on ID patterns
	if strings.Contains(strings.ToLower(osvID), "critical") {
		return SecurityLevelCritical
	}
	if strings.Contains(strings.ToLower(osvID), "high") {
		return SecurityLevelHigh
	}
	if strings.Contains(strings.ToLower(osvID), "medium") {
		return SecurityLevelMedium
	}
	if strings.Contains(strings.ToLower(osvID), "low") {
		return SecurityLevelLow
	}

	// Default to medium for unknown severities
	return SecurityLevelMedium
}

// scanGoMod scans go.mod files for known vulnerable dependencies
func (s *DependencyAuditScanner) scanGoMod(ctx context.Context, paths []string) ([]SecurityFinding, error) {
	var findings []SecurityFinding

	if len(paths) == 0 {
		paths = []string{"."}
	}

	for _, path := range paths {
		goModPath := filepath.Join(path, "go.mod")
		if _, err := os.Stat(goModPath); os.IsNotExist(err) {
			continue
		}

		pathFindings, err := s.analyzeGoMod(goModPath)
		if err != nil {
			s.logger.Warn("Failed to analyze go.mod", "path", goModPath, "error", err)
			continue
		}
		findings = append(findings, pathFindings...)
	}

	return findings, nil
}

// analyzeGoMod analyzes a go.mod file for security issues
func (s *DependencyAuditScanner) analyzeGoMod(goModPath string) ([]SecurityFinding, error) {
	content, err := os.ReadFile(goModPath)
	if err != nil {
		return nil, fmt.Errorf("read go.mod: %w", err)
	}

	var findings []SecurityFinding
	lines := strings.Split(string(content), "\n")

	// Check for known vulnerable patterns
	for lineNum, line := range lines {
		line = strings.TrimSpace(line)
		
		// Check for old/vulnerable Go versions
		if strings.HasPrefix(line, "go ") {
			if finding := s.checkGoVersion(line, goModPath, lineNum+1); finding != nil {
				findings = append(findings, *finding)
			}
		}

		// Check for known vulnerable dependencies
		if strings.Contains(line, " v") && !strings.HasPrefix(line, "//") {
			if finding := s.checkVulnerableDependency(line, goModPath, lineNum+1); finding != nil {
				findings = append(findings, *finding)
			}
		}
	}

	return findings, nil
}

// checkGoVersion checks if the Go version has known vulnerabilities
func (s *DependencyAuditScanner) checkGoVersion(line, file string, lineNum int) *SecurityFinding {
	// Extract version
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return nil
	}

	version := parts[1]
	
	// Check against known vulnerable versions
	vulnerableVersions := map[string]SecurityLevel{
		"1.19": SecurityLevelMedium, // Example: older versions may have security issues
		"1.18": SecurityLevelMedium,
		"1.17": SecurityLevelHigh,
		"1.16": SecurityLevelHigh,
	}

	for vulnVersion, level := range vulnerableVersions {
		if strings.HasPrefix(version, vulnVersion) {
			return &SecurityFinding{
				ID:          uuid.New().String(),
				Level:       level,
				Title:       "Outdated Go version",
				Description: fmt.Sprintf("Go version %s may have known security vulnerabilities", version),
				Category:    "outdated_runtime",
				File:        file,
				Line:        lineNum,
				Remediation: "Update to the latest stable Go version",
				Timestamp:   time.Now(),
				Scanner:     "go_mod_analyzer",
			}
		}
	}

	return nil
}

// checkVulnerableDependency checks if a dependency has known vulnerabilities
func (s *DependencyAuditScanner) checkVulnerableDependency(line, file string, lineNum int) *SecurityFinding {
	// This is a simplified implementation
	// In practice, you would check against a comprehensive vulnerability database
	
	knownVulnerable := map[string]struct {
		level       SecurityLevel
		description string
		remediation string
	}{
		"github.com/gin-gonic/gin v1.6.0": {
			SecurityLevelMedium,
			"Known XSS vulnerability in older Gin versions",
			"Update to gin v1.7.0 or later",
		},
		"github.com/gorilla/websocket v1.4.0": {
			SecurityLevelLow,
			"Potential DoS vulnerability in WebSocket implementation",
			"Update to latest version",
		},
		// Add more known vulnerable packages here
	}

	for vulnPkg, details := range knownVulnerable {
		if strings.Contains(line, vulnPkg) {
			return &SecurityFinding{
				ID:          uuid.New().String(),
				Level:       details.level,
				Title:       "Known vulnerable dependency",
				Description: details.description,
				Category:    "vulnerable_dependency",
				File:        file,
				Line:        lineNum,
				Remediation: details.remediation,
				Timestamp:   time.Now(),
				Scanner:     "dependency_analyzer",
			}
		}
	}

	return nil
}

// installGovulncheck attempts to install govulncheck
func (s *DependencyAuditScanner) installGovulncheck(ctx context.Context) error {
	s.logger.Info("Installing govulncheck...")
	
	cmd := exec.CommandContext(ctx, "go", "install", "golang.org/x/vuln/cmd/govulncheck@latest")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install govulncheck: %w", err)
	}

	// Update the path
	s.govulncheckPath = findGovulncheck()
	if s.govulncheckPath == "" {
		return fmt.Errorf("govulncheck installation succeeded but binary not found in PATH")
	}

	s.logger.Info("Govulncheck installed successfully", "path", s.govulncheckPath)
	return nil
}

// findGovulncheck attempts to find govulncheck in the system PATH
func findGovulncheck() string {
	path, err := exec.LookPath("govulncheck")
	if err != nil {
		return ""
	}
	return path
}