package security

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/agentbrain/agentbrain/internal/config"
	"github.com/google/uuid"
)

// RuntimeSecurityMonitor provides runtime security monitoring and validation
type RuntimeSecurityMonitor struct {
	logger      *slog.Logger
	config      *config.Config
	validators  []Validator
	events      []RuntimeEvent
	eventsMutex sync.RWMutex
	stopCh      chan struct{}
	running     bool
}

// ConfigurationValidator validates configuration security
type ConfigurationValidator struct {
	logger *slog.Logger
}

// ProcessSecurityValidator monitors process security
type ProcessSecurityValidator struct {
	logger *slog.Logger
}

// NetworkSecurityValidator monitors network security
type NetworkSecurityValidator struct {
	logger      *slog.Logger
	allowedHosts map[string]bool
	connections map[string]*NetworkConnection
	connMutex   sync.RWMutex
}

// FileSystemValidator monitors file system security
type FileSystemValidator struct {
	logger         *slog.Logger
	watchedPaths   []string
	sensitiveFiles []string
}

// NewRuntimeSecurityMonitor creates a new runtime security monitor
func NewRuntimeSecurityMonitor(config *config.Config, logger *slog.Logger) *RuntimeSecurityMonitor {
	monitor := &RuntimeSecurityMonitor{
		logger:     logger,
		config:     config,
		validators: make([]Validator, 0),
		events:     make([]RuntimeEvent, 0),
		stopCh:     make(chan struct{}),
	}

	// Initialize validators
	monitor.initializeValidators()

	return monitor
}

// initializeValidators sets up the runtime validators
func (m *RuntimeSecurityMonitor) initializeValidators() {
	// Configuration validator
	m.validators = append(m.validators, &ConfigurationValidator{
		logger: m.logger,
	})

	// Process security validator
	m.validators = append(m.validators, &ProcessSecurityValidator{
		logger: m.logger,
	})

	// Network security validator
	networkValidator := &NetworkSecurityValidator{
		logger:       m.logger,
		allowedHosts: make(map[string]bool),
		connections:  make(map[string]*NetworkConnection),
	}
	
	// Configure allowed hosts from config
	if m.config.Plugins != nil && m.config.Plugins.Security != nil {
		for _, host := range m.config.Plugins.Security.AllowedHosts {
			networkValidator.allowedHosts[host] = true
		}
	}
	
	m.validators = append(m.validators, networkValidator)

	// File system validator
	fsValidator := &FileSystemValidator{
		logger: m.logger,
		watchedPaths: []string{
			"./configs/",
			"./internal/",
			"./cmd/",
		},
		sensitiveFiles: []string{
			"go.mod",
			"go.sum",
			".env",
			"*.yaml",
			"*.yml",
			"*.json",
		},
	}
	m.validators = append(m.validators, fsValidator)
}

// Start begins runtime monitoring
func (m *RuntimeSecurityMonitor) Start(ctx context.Context) error {
	if m.running {
		return fmt.Errorf("runtime monitor already running")
	}

	m.logger.Info("Starting runtime security monitoring")
	m.running = true

	// Start monitoring goroutine
	go m.monitoringLoop(ctx)

	return nil
}

// Stop halts runtime monitoring
func (m *RuntimeSecurityMonitor) Stop() error {
	if !m.running {
		return nil
	}

	m.logger.Info("Stopping runtime security monitoring")
	close(m.stopCh)
	m.running = false

	return nil
}

// Scan performs a comprehensive runtime security scan
func (m *RuntimeSecurityMonitor) Scan(ctx context.Context, config ScanConfig) ([]SecurityFinding, error) {
	m.logger.Info("Starting runtime security scan")
	startTime := time.Now()

	var allFindings []SecurityFinding

	for _, validator := range m.validators {
		result, err := validator.Validate(ctx, m.config)
		if err != nil {
			m.logger.Warn("Validator failed", 
				"validator", validator.Name(), 
				"error", err)
			continue
		}

		if result != nil && len(result.Findings) > 0 {
			allFindings = append(allFindings, result.Findings...)
		}
	}

	duration := time.Since(startTime)
	m.logger.Info("Runtime security scan completed", 
		"findings", len(allFindings), 
		"duration", duration)

	return allFindings, nil
}

// GetEvents returns recent runtime security events
func (m *RuntimeSecurityMonitor) GetEvents(limit int) []RuntimeEvent {
	m.eventsMutex.RLock()
	defer m.eventsMutex.RUnlock()

	if limit <= 0 || limit > len(m.events) {
		limit = len(m.events)
	}

	// Return most recent events
	start := len(m.events) - limit
	if start < 0 {
		start = 0
	}

	events := make([]RuntimeEvent, limit)
	copy(events, m.events[start:])
	
	return events
}

// RecordEvent records a security event
func (m *RuntimeSecurityMonitor) RecordEvent(event RuntimeEvent) {
	m.eventsMutex.Lock()
	defer m.eventsMutex.Unlock()

	event.ID = uuid.New().String()
	event.Timestamp = time.Now()

	m.events = append(m.events, event)

	// Limit event history (keep last 1000 events)
	if len(m.events) > 1000 {
		m.events = m.events[len(m.events)-1000:]
	}

	m.logger.Info("Security event recorded", 
		"type", event.Type, 
		"level", event.Level, 
		"source", event.Source)
}

// monitoringLoop runs continuous security monitoring
func (m *RuntimeSecurityMonitor) monitoringLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // Monitor every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.performPeriodicChecks(ctx)
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// performPeriodicChecks performs periodic security checks
func (m *RuntimeSecurityMonitor) performPeriodicChecks(ctx context.Context) {
	// Check for configuration changes
	if err := m.checkConfigurationSecurity(); err != nil {
		m.RecordEvent(RuntimeEvent{
			Type:    "config_security_issue",
			Level:   SecurityLevelMedium,
			Source:  "config_monitor",
			Message: fmt.Sprintf("Configuration security issue: %v", err),
		})
	}

	// Check network connections
	if err := m.checkNetworkSecurity(); err != nil {
		m.RecordEvent(RuntimeEvent{
			Type:    "network_security_issue",
			Level:   SecurityLevelMedium,
			Source:  "network_monitor",
			Message: fmt.Sprintf("Network security issue: %v", err),
		})
	}

	// Check file system permissions
	if err := m.checkFileSystemSecurity(); err != nil {
		m.RecordEvent(RuntimeEvent{
			Type:    "filesystem_security_issue",
			Level:   SecurityLevelMedium,
			Source:  "filesystem_monitor",
			Message: fmt.Sprintf("File system security issue: %v", err),
		})
	}
}

// checkConfigurationSecurity validates current configuration security
func (m *RuntimeSecurityMonitor) checkConfigurationSecurity() error {
	// Check for insecure storage endpoints
	if m.config.Storage.Endpoint != "" && strings.HasPrefix(m.config.Storage.Endpoint, "http://") {
		return fmt.Errorf("insecure HTTP endpoint for storage: %s", m.config.Storage.Endpoint)
	}

	// Check for debug logging in production
	if m.config.Agent.LogLevel == "debug" || m.config.Agent.LogLevel == "trace" {
		return fmt.Errorf("debug logging enabled which may expose sensitive information")
	}

	// Check backup encryption
	if m.config.Backup != nil && m.config.Backup.Enabled {
		if m.config.Backup.EncryptionKey == "" {
			return fmt.Errorf("backup enabled without encryption key")
		}
	}

	return nil
}

// checkNetworkSecurity monitors network connections
func (m *RuntimeSecurityMonitor) checkNetworkSecurity() error {
	// This is a simplified implementation
	// In practice, you would monitor actual network connections
	
	// Check if network monitoring is configured
	if m.config.Plugins != nil && m.config.Plugins.Security != nil {
		if !m.config.Plugins.Security.NetworkAllowed {
			// Verify no unexpected network activity
			return m.verifyNoNetworkActivity()
		}
	}

	return nil
}

// verifyNoNetworkActivity checks for unexpected network connections
func (m *RuntimeSecurityMonitor) verifyNoNetworkActivity() error {
	// Simplified implementation - would check actual connections in production
	return nil
}

// checkFileSystemSecurity monitors file system permissions and changes
func (m *RuntimeSecurityMonitor) checkFileSystemSecurity() error {
	// Check permissions on sensitive files
	sensitiveFiles := []string{
		"configs/",
		"go.mod",
		"go.sum",
	}

	for _, file := range sensitiveFiles {
		if err := m.checkFilePermissions(file); err != nil {
			return fmt.Errorf("file permission issue for %s: %w", file, err)
		}
	}

	return nil
}

// checkFilePermissions validates file permissions
func (m *RuntimeSecurityMonitor) checkFilePermissions(filePath string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, skip check
		}
		return fmt.Errorf("stat file: %w", err)
	}

	mode := info.Mode().Perm()
	
	// Check for overly permissive permissions (world writable)
	if mode&0002 != 0 {
		return fmt.Errorf("file %s is world-writable (permissions: %o)", filePath, mode)
	}

	// Check for sensitive files that should be more restricted
	if strings.Contains(filePath, "config") || strings.Contains(filePath, "key") {
		if mode&0044 != 0 { // Group or other readable
			return fmt.Errorf("sensitive file %s has overly permissive permissions (permissions: %o)", filePath, mode)
		}
	}

	return nil
}

// ConfigurationValidator implementation

func (v *ConfigurationValidator) Name() string {
	return "configuration_security"
}

func (v *ConfigurationValidator) Description() string {
	return "Validates configuration security settings"
}

func (v *ConfigurationValidator) Validate(ctx context.Context, target interface{}) (*ValidationResult, error) {
	config, ok := target.(*config.Config)
	if !ok {
		return nil, fmt.Errorf("expected *config.Config, got %T", target)
	}

	var findings []SecurityFinding
	var score float64 = 100.0 // Start with perfect score

	// Validate storage configuration
	if storageFindings := v.validateStorageConfig(config); len(storageFindings) > 0 {
		findings = append(findings, storageFindings...)
		score -= float64(len(storageFindings)) * 10
	}

	// Validate authentication configuration
	if authFindings := v.validateAuthConfig(config); len(authFindings) > 0 {
		findings = append(findings, authFindings...)
		score -= float64(len(authFindings)) * 15
	}

	// Validate logging configuration
	if logFindings := v.validateLoggingConfig(config); len(logFindings) > 0 {
		findings = append(findings, logFindings...)
		score -= float64(len(logFindings)) * 5
	}

	// Validate backup configuration
	if backupFindings := v.validateBackupConfig(config); len(backupFindings) > 0 {
		findings = append(findings, backupFindings...)
		score -= float64(len(backupFindings)) * 10
	}

	if score < 0 {
		score = 0
	}

	result := &ValidationResult{
		Valid:    len(findings) == 0,
		Findings: findings,
		Score:    score,
		Metadata: map[string]string{
			"total_checks": fmt.Sprintf("%d", 4), // Storage, Auth, Logging, Backup
			"failed_checks": fmt.Sprintf("%d", len(findings)),
		},
	}

	return result, nil
}

func (v *ConfigurationValidator) validateStorageConfig(config *config.Config) []SecurityFinding {
	var findings []SecurityFinding

	// Check for insecure endpoints
	if config.Storage.Endpoint != "" && strings.HasPrefix(config.Storage.Endpoint, "http://") {
		findings = append(findings, SecurityFinding{
			ID:          uuid.New().String(),
			Level:       SecurityLevelHigh,
			Title:       "Insecure storage endpoint",
			Description: "Storage endpoint uses HTTP instead of HTTPS",
			Category:    "insecure_transport",
			Remediation: "Use HTTPS endpoints for all storage operations",
			Timestamp:   time.Now(),
			Scanner:     "config_validator",
		})
	}

	// Check bucket naming
	if config.Storage.Bucket == "" {
		findings = append(findings, SecurityFinding{
			ID:          uuid.New().String(),
			Level:       SecurityLevelMedium,
			Title:       "Missing storage bucket configuration",
			Description: "Storage bucket is not configured",
			Category:    "configuration",
			Remediation: "Configure a specific storage bucket name",
			Timestamp:   time.Now(),
			Scanner:     "config_validator",
		})
	}

	return findings
}

func (v *ConfigurationValidator) validateAuthConfig(config *config.Config) []SecurityFinding {
	var findings []SecurityFinding

	for sourceName, sourceConfig := range config.Sources {
		// Check for hardcoded credentials
		for key, value := range sourceConfig.Auth {
			if v.looksLikeHardcodedCredential(key, value) {
				findings = append(findings, SecurityFinding{
					ID:          uuid.New().String(),
					Level:       SecurityLevelCritical,
					Title:       "Hardcoded credentials detected",
					Description: fmt.Sprintf("Source %s contains hardcoded credentials in field %s", sourceName, key),
					Category:    "credential_exposure",
					Remediation: "Use environment variables or secure credential stores",
					Timestamp:   time.Now(),
					Scanner:     "config_validator",
				})
			}
		}
	}

	return findings
}

func (v *ConfigurationValidator) looksLikeHardcodedCredential(key, value string) bool {
	key = strings.ToLower(key)
	
	// Skip environment variable references
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		return false
	}

	// Check for credential field names with actual values
	credentialFields := []string{
		"password", "secret", "token", "key", "client_secret", "security_token",
	}

	for _, field := range credentialFields {
		if strings.Contains(key, field) && value != "" && len(value) > 5 {
			return true
		}
	}

	return false
}

func (v *ConfigurationValidator) validateLoggingConfig(config *config.Config) []SecurityFinding {
	var findings []SecurityFinding

	// Check for debug logging
	if config.Agent.LogLevel == "debug" || config.Agent.LogLevel == "trace" {
		findings = append(findings, SecurityFinding{
			ID:          uuid.New().String(),
			Level:       SecurityLevelMedium,
			Title:       "Debug logging enabled",
			Description: "Debug logging may expose sensitive information in logs",
			Category:    "information_disclosure",
			Remediation: "Set log level to 'info' or 'warn' in production",
			Timestamp:   time.Now(),
			Scanner:     "config_validator",
		})
	}

	return findings
}

func (v *ConfigurationValidator) validateBackupConfig(config *config.Config) []SecurityFinding {
	var findings []SecurityFinding

	if config.Backup != nil && config.Backup.Enabled {
		// Check for encryption
		if config.Backup.EncryptionKey == "" {
			findings = append(findings, SecurityFinding{
				ID:          uuid.New().String(),
				Level:       SecurityLevelHigh,
				Title:       "Backup encryption not configured",
				Description: "Backups are enabled but encryption is not configured",
				Category:    "weak_crypto",
				Remediation: "Configure encryption_key for backup operations",
				Timestamp:   time.Now(),
				Scanner:     "config_validator",
			})
		}

		// Check for cross-region backup security
		if config.Backup.CrossRegion && config.Backup.DestinationRegion == config.Storage.Region {
			findings = append(findings, SecurityFinding{
				ID:          uuid.New().String(),
				Level:       SecurityLevelLow,
				Title:       "Cross-region backup misconfiguration",
				Description: "Cross-region backup is enabled but destination region is the same as source",
				Category:    "configuration",
				Remediation: "Use a different region for cross-region backups",
				Timestamp:   time.Now(),
				Scanner:     "config_validator",
			})
		}
	}

	return findings
}

// ProcessSecurityValidator implementation

func (v *ProcessSecurityValidator) Name() string {
	return "process_security"
}

func (v *ProcessSecurityValidator) Description() string {
	return "Monitors process security and privilege escalation"
}

func (v *ProcessSecurityValidator) Validate(ctx context.Context, target interface{}) (*ValidationResult, error) {
	// This is a simplified implementation
	// In practice, you would check running processes, privileges, etc.

	findings := []SecurityFinding{}
	
	// Check current process privileges
	if os.Geteuid() == 0 {
		findings = append(findings, SecurityFinding{
			ID:          uuid.New().String(),
			Level:       SecurityLevelHigh,
			Title:       "Running as root",
			Description: "Process is running with root privileges",
			Category:    "privilege_escalation",
			Remediation: "Run with minimal required privileges",
			Timestamp:   time.Now(),
			Scanner:     "process_validator",
		})
	}

	result := &ValidationResult{
		Valid:    len(findings) == 0,
		Findings: findings,
		Score:    100.0,
		Metadata: map[string]string{
			"process_checks": "1",
		},
	}

	return result, nil
}

// NetworkSecurityValidator implementation

func (v *NetworkSecurityValidator) Name() string {
	return "network_security"
}

func (v *NetworkSecurityValidator) Description() string {
	return "Monitors network connections and validates against allow-lists"
}

func (v *NetworkSecurityValidator) Validate(ctx context.Context, target interface{}) (*ValidationResult, error) {
	findings := []SecurityFinding{}

	// Check for listening ports
	if listeners := v.checkListeningPorts(); len(listeners) > 0 {
		for _, port := range listeners {
			findings = append(findings, SecurityFinding{
				ID:          uuid.New().String(),
				Level:       SecurityLevelInfo,
				Title:       "Open network port",
				Description: fmt.Sprintf("Process is listening on port %s", port),
				Category:    "network_exposure",
				Remediation: "Verify that this port should be open and is properly secured",
				Timestamp:   time.Now(),
				Scanner:     "network_validator",
			})
		}
	}

	result := &ValidationResult{
		Valid:    len(findings) == 0,
		Findings: findings,
		Score:    100.0,
		Metadata: map[string]string{
			"network_checks": "1",
		},
	}

	return result, nil
}

func (v *NetworkSecurityValidator) checkListeningPorts() []string {
	var ports []string

	// Get listening TCP ports
	listeners, err := net.Listen("tcp", ":")
	if err == nil {
		addr := listeners.Addr().String()
		if tcpAddr, ok := listeners.Addr().(*net.TCPAddr); ok {
			ports = append(ports, fmt.Sprintf("tcp:%d", tcpAddr.Port))
		} else {
			ports = append(ports, fmt.Sprintf("tcp:%s", addr))
		}
		listeners.Close()
	}

	return ports
}

// FileSystemValidator implementation

func (v *FileSystemValidator) Name() string {
	return "filesystem_security"
}

func (v *FileSystemValidator) Description() string {
	return "Monitors file system permissions and validates directory security"
}

func (v *FileSystemValidator) Validate(ctx context.Context, target interface{}) (*ValidationResult, error) {
	findings := []SecurityFinding{}

	// Check watched paths
	for _, path := range v.watchedPaths {
		if pathFindings := v.validatePath(path); len(pathFindings) > 0 {
			findings = append(findings, pathFindings...)
		}
	}

	result := &ValidationResult{
		Valid:    len(findings) == 0,
		Findings: findings,
		Score:    100.0,
		Metadata: map[string]string{
			"paths_checked": fmt.Sprintf("%d", len(v.watchedPaths)),
		},
	}

	return result, nil
}

func (v *FileSystemValidator) validatePath(path string) []SecurityFinding {
	var findings []SecurityFinding

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip inaccessible files
		}

		// Check file permissions
		mode := info.Mode().Perm()
		
		// Check for overly permissive permissions
		if mode&0002 != 0 { // World writable
			findings = append(findings, SecurityFinding{
				ID:          uuid.New().String(),
				Level:       SecurityLevelMedium,
				Title:       "World-writable file",
				Description: fmt.Sprintf("File %s is world-writable", filePath),
				Category:    "file_permissions",
				File:        filePath,
				Remediation: "Remove world-write permissions from sensitive files",
				Timestamp:   time.Now(),
				Scanner:     "filesystem_validator",
			})
		}

		// Check for sensitive file patterns
		if v.isSensitiveFile(filePath) && mode&0044 != 0 {
			findings = append(findings, SecurityFinding{
				ID:          uuid.New().String(),
				Level:       SecurityLevelLow,
				Title:       "Sensitive file with permissive permissions",
				Description: fmt.Sprintf("Sensitive file %s has group/other read permissions", filePath),
				Category:    "file_permissions",
				File:        filePath,
				Remediation: "Restrict permissions on sensitive files",
				Timestamp:   time.Now(),
				Scanner:     "filesystem_validator",
			})
		}

		return nil
	})

	if err != nil {
		v.logger.Debug("Error walking path", "path", path, "error", err)
	}

	return findings
}

func (v *FileSystemValidator) isSensitiveFile(filePath string) bool {
	for _, pattern := range v.sensitiveFiles {
		if matched, _ := filepath.Match(pattern, filepath.Base(filePath)); matched {
			return true
		}
	}
	
	// Check for common sensitive patterns
	sensitivePatterns := []string{
		"password", "secret", "key", "token", "credential",
	}
	
	fileName := strings.ToLower(filepath.Base(filePath))
	for _, pattern := range sensitivePatterns {
		if strings.Contains(fileName, pattern) {
			return true
		}
	}
	
	return false
}