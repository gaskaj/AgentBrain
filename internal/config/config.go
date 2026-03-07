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
	Type        string            `yaml:"type"`
	Enabled     bool              `yaml:"enabled"`
	Schedule    string            `yaml:"schedule"`
	Concurrency int              `yaml:"concurrency"`
	BatchSize   int              `yaml:"batch_size"`
	Objects     []string          `yaml:"objects"`
	Auth        map[string]string `yaml:"auth"`
	Options     map[string]string `yaml:"options"`
	Checkpoint  *CheckpointConfig `yaml:"checkpoint,omitempty"`
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


