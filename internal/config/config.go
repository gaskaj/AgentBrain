package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/agentbrain/agentbrain/internal/monitoring"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Agent      AgentConfig                      `yaml:"agent"`
	Storage    StorageConfig                    `yaml:"storage"`
	Sources    map[string]*SourceConfig         `yaml:"sources"`
	Monitoring monitoring.MonitoringConfig     `yaml:"monitoring"`
	Backup     *BackupConfig                    `yaml:"backup,omitempty"`
	Profiler   *ProfilerConfig                  `yaml:"profiler,omitempty"`
	Plugins    *PluginConfig                    `yaml:"plugins,omitempty"`
	Retry      *RetryConfig                     `yaml:"retry,omitempty"`
}

type AgentConfig struct {
	LogLevel   string        `yaml:"log_level"`
	LogFormat  string        `yaml:"log_format"`
	HealthAddr string        `yaml:"health_addr"`
	Schedule   string        `yaml:"schedule"`
	Timeout    time.Duration `yaml:"timeout"`
}

type StorageConfig struct {
	Bucket   string `yaml:"bucket"`
	Region   string `yaml:"region"`
	Endpoint string `yaml:"endpoint"`
	Prefix   string `yaml:"prefix"`
}

type SourceConfig struct {
	Type         string                 `yaml:"type"`
	Enabled      bool                   `yaml:"enabled"`
	Schedule     string                 `yaml:"schedule"`
	Concurrency  int                   `yaml:"concurrency"`
	BatchSize    int                   `yaml:"batch_size"`
	Objects      []string               `yaml:"objects"`
	Auth         map[string]string      `yaml:"auth"`
	Options      map[string]string      `yaml:"options"`
	Checkpoint   *CheckpointConfig      `yaml:"checkpoint,omitempty"`
	Consistency  *ConsistencyConfig     `yaml:"consistency,omitempty"`
	Validation   *ValidationConfig      `yaml:"validation,omitempty"`
	ErrorHandling *ErrorHandlingConfig  `yaml:"error_handling,omitempty"`
}

type ConsistencyConfig struct {
	Enabled         bool                    `yaml:"enabled"`
	Relationships   map[string][]string     `yaml:"relationships"`
	Windows         map[string]string       `yaml:"staleness_windows"`
	MaxStaleness    string                  `yaml:"max_staleness"`
	RequiredObjects []string                `yaml:"required_objects"`
	FailOnViolation bool                    `yaml:"fail_on_violation"`
}

type CheckpointConfig struct {
	Frequency         int    `yaml:"frequency"`
	RetentionDays     int    `yaml:"retention_days"`
	MaxCheckpoints    int    `yaml:"max_checkpoints"`
	ValidationEnabled bool   `yaml:"validation_enabled"`
	CompactionEnabled bool   `yaml:"compaction_enabled"`
	SizeThresholdMB   int64  `yaml:"size_threshold_mb"`
	AdaptiveMode      bool   `yaml:"adaptive_mode"`
}

type ValidationConfig struct {
	Enabled         bool                    `yaml:"enabled"`
	ErrorThreshold  float64                 `yaml:"error_threshold"`
	DriftThreshold  float64                 `yaml:"drift_threshold"`
	StrictMode      bool                    `yaml:"strict_mode"`
	CustomRules     map[string][]CustomRule `yaml:"custom_rules"`
}

type CustomRule struct {
	Field    string    `yaml:"field"`
	Type     string    `yaml:"type"`
	Min      *float64  `yaml:"min,omitempty"`
	Max      *float64  `yaml:"max,omitempty"`
	Pattern  string    `yaml:"pattern,omitempty"`
	Values   []string  `yaml:"values,omitempty"`
	Required bool      `yaml:"required"`
}

type ErrorHandlingConfig struct {
	MaxRetries             int           `yaml:"max_retries"`
	BaseDelay              time.Duration `yaml:"base_delay"`
	MaxDelay               time.Duration `yaml:"max_delay"`
	CircuitBreakerThreshold int           `yaml:"circuit_breaker_threshold"`
	CircuitBreakerTimeout   time.Duration `yaml:"circuit_breaker_timeout"`
	PartialRecovery         bool          `yaml:"partial_recovery"`
	SkipFailedObjects       bool          `yaml:"skip_failed_objects"`
}

type BackupConfig struct {
	Enabled           bool   `yaml:"enabled"`
	DestinationBucket string `yaml:"destination_bucket"`
	DestinationRegion string `yaml:"destination_region"`
	Schedule          string `yaml:"schedule"`
	RetentionDays     int    `yaml:"retention_days"`
	CrossRegion       bool   `yaml:"cross_region"`
	EncryptionKey     string `yaml:"encryption_key"`
	ValidationMode    string `yaml:"validation_mode"` // "checksum", "full", "none"
	ConcurrentUploads int    `yaml:"concurrent_uploads"`
	ChunkSizeMB       int    `yaml:"chunk_size_mb"`
}

type ProfilerConfig struct {
	Enabled               bool          `yaml:"enabled"`
	SampleRate            float64       `yaml:"sample_rate"`
	OutputDir             string        `yaml:"output_dir"`
	CPUProfileDuration    time.Duration `yaml:"cpu_profile_duration"`
	MemoryProfileInterval time.Duration `yaml:"memory_profile_interval"`
	GoroutineThreshold    int           `yaml:"goroutine_threshold"`
}

type PluginConfig struct {
	Enabled     bool                    `yaml:"enabled"`
	Directory   string                  `yaml:"directory"`
	AutoReload  bool                    `yaml:"auto_reload"`
	WatchPaths  []string                `yaml:"watch_paths"`
	Security    *PluginSecurityConfig   `yaml:"security,omitempty"`
}

type PluginSecurityConfig struct {
	MaxMemoryMB      int               `yaml:"max_memory_mb"`
	MaxCPUPercent    float64           `yaml:"max_cpu_percent"`
	NetworkAllowed   bool              `yaml:"network_allowed"`
	AllowedHosts     []string          `yaml:"allowed_hosts"`
	AllowedEnvVars   map[string]string `yaml:"allowed_env_vars"`
	SandboxEnabled   bool              `yaml:"sandbox_enabled"`
}

// RetryConfig holds the retry framework configuration.
type RetryConfig struct {
	DefaultPolicy     RetryPolicyConfig                    `yaml:"default_policy"`
	CircuitBreakers   map[string]CircuitBreakerConfig      `yaml:"circuit_breakers"`
	OperationPolicies map[string]RetryPolicyConfig         `yaml:"operation_policies"`
}

// RetryPolicyConfig represents the configuration for a retry policy.
type RetryPolicyConfig struct {
	MaxAttempts        int           `yaml:"max_attempts"`
	BaseDelay          time.Duration `yaml:"base_delay"`
	MaxDelay           time.Duration `yaml:"max_delay"`
	BackoffStrategy    string        `yaml:"backoff_strategy"`
	Jitter             bool          `yaml:"jitter"`
	RetryableErrors    []string      `yaml:"retryable_errors,omitempty"`
	NonRetryableErrors []string      `yaml:"non_retryable_errors,omitempty"`
}

// CircuitBreakerConfig represents the configuration for a circuit breaker.
type CircuitBreakerConfig struct {
	FailureThreshold int           `yaml:"failure_threshold"`
	ResetTimeout     time.Duration `yaml:"reset_timeout"`
	Enabled          bool          `yaml:"enabled"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	expanded := expandEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	setDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

// ValidateWithRegistry validates the configuration using the provided connector registry.
func (c *Config) ValidateWithRegistry(registry ConnectorRegistry) error {
	// First validate basic config structure
	if err := validate(c); err != nil {
		return err
	}

	// Then validate each connector's configuration
	for sourceName, sourceConfig := range c.Sources {
		if err := registry.ValidateSourceConfig(sourceConfig); err != nil {
			return fmt.Errorf("invalid config for source %s: %w", sourceName, err)
		}
	}
	return nil
}

// ConnectorRegistry interface for configuration validation
type ConnectorRegistry interface {
	ValidateSourceConfig(sourceConfig *SourceConfig) error
}

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-1]

		parts := strings.SplitN(key, ":-", 2)
		varName := strings.TrimSpace(parts[0])

		if val, ok := os.LookupEnv(varName); ok {
			return val
		}
		if len(parts) == 2 {
			return parts[1]
		}
		return match
	})
}

func setDefaults(cfg *Config) {
	if cfg.Agent.LogLevel == "" {
		cfg.Agent.LogLevel = "info"
	}
	if cfg.Agent.LogFormat == "" {
		cfg.Agent.LogFormat = "json"
	}
	if cfg.Agent.HealthAddr == "" {
		cfg.Agent.HealthAddr = ":8080"
	}
	if cfg.Agent.Schedule == "" {
		cfg.Agent.Schedule = "@every 1h"
	}
	if cfg.Agent.Timeout == 0 {
		cfg.Agent.Timeout = 30 * time.Minute
	}

	// Set monitoring defaults
	if cfg.Monitoring.CheckInterval == 0 {
		cfg.Monitoring.CheckInterval = 5 * time.Minute
	}
	if cfg.Monitoring.AlertCooldown == 0 {
		cfg.Monitoring.AlertCooldown = 30 * time.Minute
	}

	// Set default rule configurations
	setRuleDefaults(&cfg.Monitoring.Rules)

	for _, src := range cfg.Sources {
		if src.Concurrency <= 0 {
			src.Concurrency = 4
		}
		if src.BatchSize <= 0 {
			src.BatchSize = 10000
		}
		
		// Set checkpoint defaults if checkpoint config is provided
		if src.Checkpoint != nil {
			setCheckpointDefaults(src.Checkpoint)
		}
		
		// Set error handling defaults if error handling config is provided
		if src.ErrorHandling != nil {
			setErrorHandlingDefaults(src.ErrorHandling)
		}
	}

	// Set backup defaults if backup config is provided
	if cfg.Backup != nil {
		setBackupDefaults(cfg.Backup)
	}

	// Set profiler defaults if profiler config is provided
	if cfg.Profiler != nil {
		setProfilerDefaults(cfg.Profiler)
	}

	// Set plugin defaults if plugin config is provided
	if cfg.Plugins != nil {
		setPluginDefaults(cfg.Plugins)
	}

	// Set retry defaults if retry config is provided
	if cfg.Retry != nil {
		setRetryDefaults(cfg.Retry)
	}
}

func setCheckpointDefaults(cp *CheckpointConfig) {
	if cp.Frequency <= 0 {
		cp.Frequency = 10
	}
	if cp.RetentionDays <= 0 {
		cp.RetentionDays = 30
	}
	if cp.MaxCheckpoints <= 0 {
		cp.MaxCheckpoints = 50
	}
	if cp.SizeThresholdMB <= 0 {
		cp.SizeThresholdMB = 128
	}
}

func setBackupDefaults(bp *BackupConfig) {
	if bp.Schedule == "" {
		bp.Schedule = "@daily" // Daily backups by default
	}
	if bp.RetentionDays <= 0 {
		bp.RetentionDays = 30 // 30 days retention
	}
	if bp.ValidationMode == "" {
		bp.ValidationMode = "checksum" // Default to checksum validation
	}
	if bp.ConcurrentUploads <= 0 {
		bp.ConcurrentUploads = 4 // Default concurrency
	}
	if bp.ChunkSizeMB <= 0 {
		bp.ChunkSizeMB = 64 // 64MB chunks
	}
}

func setProfilerDefaults(pf *ProfilerConfig) {
	if pf.SampleRate <= 0 {
		pf.SampleRate = 0.1 // Sample 10% of operations by default
	}
	if pf.OutputDir == "" {
		pf.OutputDir = "./profiles"
	}
	if pf.CPUProfileDuration <= 0 {
		pf.CPUProfileDuration = 30 * time.Second
	}
	if pf.MemoryProfileInterval <= 0 {
		pf.MemoryProfileInterval = 5 * time.Minute
	}
	if pf.GoroutineThreshold <= 0 {
		pf.GoroutineThreshold = 1000
	}
}

func setPluginDefaults(pg *PluginConfig) {
	if pg.Directory == "" {
		pg.Directory = "/etc/agentbrain/plugins"
	}
	if len(pg.WatchPaths) == 0 {
		pg.WatchPaths = []string{pg.Directory}
	}
	
	// Set security defaults if security config is provided
	if pg.Security != nil {
		if pg.Security.MaxMemoryMB <= 0 {
			pg.Security.MaxMemoryMB = 512
		}
		if pg.Security.MaxCPUPercent <= 0 {
			pg.Security.MaxCPUPercent = 25.0
		}
		if pg.Security.AllowedEnvVars == nil {
			pg.Security.AllowedEnvVars = make(map[string]string)
		}
	}
}

func setRetryDefaults(rt *RetryConfig) {
	// Set default policy defaults
	if rt.DefaultPolicy.MaxAttempts <= 0 {
		rt.DefaultPolicy.MaxAttempts = 3
	}
	if rt.DefaultPolicy.BaseDelay <= 0 {
		rt.DefaultPolicy.BaseDelay = time.Second
	}
	if rt.DefaultPolicy.MaxDelay <= 0 {
		rt.DefaultPolicy.MaxDelay = 30 * time.Second
	}
	if rt.DefaultPolicy.BackoffStrategy == "" {
		rt.DefaultPolicy.BackoffStrategy = "exponential_jitter"
	}

	// Initialize maps if nil
	if rt.CircuitBreakers == nil {
		rt.CircuitBreakers = make(map[string]CircuitBreakerConfig)
	}
	if rt.OperationPolicies == nil {
		rt.OperationPolicies = make(map[string]RetryPolicyConfig)
	}

	// Set default circuit breakers if none configured
	if len(rt.CircuitBreakers) == 0 {
		rt.CircuitBreakers["s3_operations"] = CircuitBreakerConfig{
			FailureThreshold: 5,
			ResetTimeout:     60 * time.Second,
			Enabled:          true,
		}
		rt.CircuitBreakers["connector_auth"] = CircuitBreakerConfig{
			FailureThreshold: 3,
			ResetTimeout:     5 * time.Minute,
			Enabled:          true,
		}
		rt.CircuitBreakers["api_operations"] = CircuitBreakerConfig{
			FailureThreshold: 10,
			ResetTimeout:     2 * time.Minute,
			Enabled:          true,
		}
	}

	// Set default operation policies if none configured
	if len(rt.OperationPolicies) == 0 {
		rt.OperationPolicies["s3_upload"] = RetryPolicyConfig{
			MaxAttempts:     5,
			BaseDelay:       2 * time.Second,
			MaxDelay:        120 * time.Second,
			BackoffStrategy: "exponential",
			Jitter:          true,
		}
		rt.OperationPolicies["s3_download"] = RetryPolicyConfig{
			MaxAttempts:     5,
			BaseDelay:       time.Second,
			MaxDelay:        60 * time.Second,
			BackoffStrategy: "exponential",
			Jitter:          true,
		}
		rt.OperationPolicies["delta_commit"] = RetryPolicyConfig{
			MaxAttempts:     3,
			BaseDelay:       500 * time.Millisecond,
			MaxDelay:        10 * time.Second,
			BackoffStrategy: "linear",
			Jitter:          false,
		}
		rt.OperationPolicies["connector_auth"] = RetryPolicyConfig{
			MaxAttempts:     3,
			BaseDelay:       2 * time.Second,
			MaxDelay:        30 * time.Second,
			BackoffStrategy: "fixed",
			Jitter:          false,
		}
		rt.OperationPolicies["api_request"] = RetryPolicyConfig{
			MaxAttempts:     4,
			BaseDelay:       time.Second,
			MaxDelay:        30 * time.Second,
			BackoffStrategy: "api_rate_limit",
			Jitter:          true,
		}
	}

	// Set defaults for each circuit breaker
	for name, cb := range rt.CircuitBreakers {
		if cb.FailureThreshold <= 0 {
			cb.FailureThreshold = 5
		}
		if cb.ResetTimeout <= 0 {
			cb.ResetTimeout = time.Minute
		}
		rt.CircuitBreakers[name] = cb
	}

	// Set defaults for each operation policy
	for name, policy := range rt.OperationPolicies {
		if policy.MaxAttempts <= 0 {
			policy.MaxAttempts = rt.DefaultPolicy.MaxAttempts
		}
		if policy.BaseDelay <= 0 {
			policy.BaseDelay = rt.DefaultPolicy.BaseDelay
		}
		if policy.MaxDelay <= 0 {
			policy.MaxDelay = rt.DefaultPolicy.MaxDelay
		}
		if policy.BackoffStrategy == "" {
			policy.BackoffStrategy = rt.DefaultPolicy.BackoffStrategy
		}
		rt.OperationPolicies[name] = policy
	}
}

func setErrorHandlingDefaults(eh *ErrorHandlingConfig) {
	if eh.MaxRetries <= 0 {
		eh.MaxRetries = 3
	}
	if eh.BaseDelay <= 0 {
		eh.BaseDelay = 1 * time.Second
	}
	if eh.MaxDelay <= 0 {
		eh.MaxDelay = 60 * time.Second
	}
	if eh.CircuitBreakerThreshold <= 0 {
		eh.CircuitBreakerThreshold = 5
	}
	if eh.CircuitBreakerTimeout <= 0 {
		eh.CircuitBreakerTimeout = 2 * time.Minute
	}
}

func setRuleDefaults(rules *monitoring.RulesConfig) {
	// Agent failure rate rule defaults
	if rules.AgentFailureRate.Threshold == 0 {
		rules.AgentFailureRate.Threshold = 0.10 // 10%
	}
	if rules.AgentFailureRate.Window == 0 {
		rules.AgentFailureRate.Window = 1 * time.Hour
	}
	if rules.AgentFailureRate.Severity == "" {
		rules.AgentFailureRate.Severity = "warning"
	}

	// Workflow completion rule defaults
	if rules.WorkflowCompletion.MinSuccessRate == 0 {
		rules.WorkflowCompletion.MinSuccessRate = 0.80 // 80%
	}
	if rules.WorkflowCompletion.Window == 0 {
		rules.WorkflowCompletion.Window = 6 * time.Hour
	}
	if rules.WorkflowCompletion.Severity == "" {
		rules.WorkflowCompletion.Severity = "critical"
	}

	// Disk usage rule defaults
	if rules.DiskUsage.WarningThreshold == 0 {
		rules.DiskUsage.WarningThreshold = 80.0 // 80%
	}
	if rules.DiskUsage.CriticalThreshold == 0 {
		rules.DiskUsage.CriticalThreshold = 90.0 // 90%
	}
	if rules.DiskUsage.Severity == "" {
		rules.DiskUsage.Severity = "warning"
	}

	// Memory usage rule defaults
	if rules.MemoryUsage.WarningThreshold == 0 {
		rules.MemoryUsage.WarningThreshold = 80.0 // 80%
	}
	if rules.MemoryUsage.CriticalThreshold == 0 {
		rules.MemoryUsage.CriticalThreshold = 90.0 // 90%
	}
	if rules.MemoryUsage.Severity == "" {
		rules.MemoryUsage.Severity = "warning"
	}

	// API response time rule defaults
	if rules.APIResponseTime.WarningThreshold == 0 {
		rules.APIResponseTime.WarningThreshold = 5 * time.Second
	}
	if rules.APIResponseTime.CriticalThreshold == 0 {
		rules.APIResponseTime.CriticalThreshold = 10 * time.Second
	}
	if rules.APIResponseTime.Severity == "" {
		rules.APIResponseTime.Severity = "warning"
	}
}

func validate(cfg *Config) error {
	if cfg.Storage.Bucket == "" {
		return fmt.Errorf("storage.bucket is required")
	}
	if cfg.Storage.Region == "" {
		return fmt.Errorf("storage.region is required")
	}
	if len(cfg.Sources) == 0 {
		return fmt.Errorf("at least one source is required")
	}
	for name, src := range cfg.Sources {
		if src.Type == "" {
			return fmt.Errorf("source %q: type is required", name)
		}
	}
	return nil
}


