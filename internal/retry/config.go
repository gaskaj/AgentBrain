package retry

import (
	"fmt"
	"time"
)

// Config holds the complete retry configuration for the system.
type Config struct {
	DefaultPolicy     PolicyConfig                    `yaml:"default_policy"`
	CircuitBreakers   map[string]CircuitBreakerConfig `yaml:"circuit_breakers"`
	OperationPolicies map[string]PolicyConfig         `yaml:"operation_policies"`
}

// PolicyConfig represents the configuration for a retry policy.
type PolicyConfig struct {
	MaxAttempts      int           `yaml:"max_attempts"`
	BaseDelay        time.Duration `yaml:"base_delay"`
	MaxDelay         time.Duration `yaml:"max_delay"`
	BackoffStrategy  string        `yaml:"backoff_strategy"`
	Jitter           bool          `yaml:"jitter"`
	RetryableErrors  []string      `yaml:"retryable_errors,omitempty"`
	NonRetryableErrors []string    `yaml:"non_retryable_errors,omitempty"`
}

// CircuitBreakerConfig represents the configuration for a circuit breaker.
type CircuitBreakerConfig struct {
	FailureThreshold int           `yaml:"failure_threshold"`
	ResetTimeout     time.Duration `yaml:"reset_timeout"`
	Enabled          bool          `yaml:"enabled"`
}

// DefaultConfig returns a default retry configuration.
func DefaultConfig() *Config {
	return &Config{
		DefaultPolicy: PolicyConfig{
			MaxAttempts:     3,
			BaseDelay:       time.Second,
			MaxDelay:        30 * time.Second,
			BackoffStrategy: "exponential_jitter",
			Jitter:          true,
		},
		CircuitBreakers: map[string]CircuitBreakerConfig{
			"s3_operations": {
				FailureThreshold: 5,
				ResetTimeout:     60 * time.Second,
				Enabled:          true,
			},
			"connector_auth": {
				FailureThreshold: 3,
				ResetTimeout:     5 * time.Minute,
				Enabled:          true,
			},
			"api_operations": {
				FailureThreshold: 10,
				ResetTimeout:     2 * time.Minute,
				Enabled:          true,
			},
		},
		OperationPolicies: map[string]PolicyConfig{
			"s3_upload": {
				MaxAttempts:     5,
				BaseDelay:       2 * time.Second,
				MaxDelay:        120 * time.Second,
				BackoffStrategy: "exponential",
				Jitter:          true,
			},
			"s3_download": {
				MaxAttempts:     5,
				BaseDelay:       time.Second,
				MaxDelay:        60 * time.Second,
				BackoffStrategy: "exponential",
				Jitter:          true,
			},
			"delta_commit": {
				MaxAttempts:     3,
				BaseDelay:       500 * time.Millisecond,
				MaxDelay:        10 * time.Second,
				BackoffStrategy: "linear",
				Jitter:          false,
			},
			"connector_auth": {
				MaxAttempts:     3,
				BaseDelay:       2 * time.Second,
				MaxDelay:        30 * time.Second,
				BackoffStrategy: "fixed",
				Jitter:          false,
			},
			"api_request": {
				MaxAttempts:     4,
				BaseDelay:       time.Second,
				MaxDelay:        30 * time.Second,
				BackoffStrategy: "api_rate_limit",
				Jitter:          true,
			},
			"database_query": {
				MaxAttempts:     3,
				BaseDelay:       time.Second,
				MaxDelay:        15 * time.Second,
				BackoffStrategy: "linear",
				Jitter:          false,
			},
		},
	}
}

// ToRetryPolicy converts a PolicyConfig to a RetryPolicy.
func (pc *PolicyConfig) ToRetryPolicy() (*RetryPolicy, error) {
	backoffFunc, err := parseBackoffStrategy(pc.BackoffStrategy)
	if err != nil {
		return nil, fmt.Errorf("invalid backoff strategy '%s': %w", pc.BackoffStrategy, err)
	}

	retryableFunc := DefaultRetryableFunc
	if len(pc.RetryableErrors) > 0 || len(pc.NonRetryableErrors) > 0 {
		retryableFunc = createCustomRetryableFunc(pc.RetryableErrors, pc.NonRetryableErrors)
	}

	return &RetryPolicy{
		MaxAttempts:   pc.MaxAttempts,
		BaseDelay:     pc.BaseDelay,
		MaxDelay:      pc.MaxDelay,
		BackoffFunc:   backoffFunc,
		RetryableFunc: retryableFunc,
		Jitter:        pc.Jitter,
	}, nil
}

// ToCircuitBreaker converts a CircuitBreakerConfig to a CircuitBreaker.
func (cbc *CircuitBreakerConfig) ToCircuitBreaker(name string) *CircuitBreaker {
	return NewCircuitBreaker(name, cbc.FailureThreshold, cbc.ResetTimeout)
}

// Validate validates the retry configuration.
func (c *Config) Validate() error {
	// Validate default policy
	if err := c.DefaultPolicy.Validate(); err != nil {
		return fmt.Errorf("invalid default policy: %w", err)
	}

	// Validate circuit breaker configurations
	for name, cb := range c.CircuitBreakers {
		if err := cb.Validate(); err != nil {
			return fmt.Errorf("invalid circuit breaker '%s': %w", name, err)
		}
	}

	// Validate operation policies
	for name, policy := range c.OperationPolicies {
		if err := policy.Validate(); err != nil {
			return fmt.Errorf("invalid operation policy '%s': %w", name, err)
		}
	}

	return nil
}

// Validate validates a PolicyConfig.
func (pc *PolicyConfig) Validate() error {
	if pc.MaxAttempts <= 0 {
		return fmt.Errorf("max_attempts must be positive, got %d", pc.MaxAttempts)
	}

	if pc.BaseDelay <= 0 {
		return fmt.Errorf("base_delay must be positive, got %s", pc.BaseDelay)
	}

	if pc.MaxDelay <= 0 {
		return fmt.Errorf("max_delay must be positive, got %s", pc.MaxDelay)
	}

	if pc.BaseDelay > pc.MaxDelay {
		return fmt.Errorf("base_delay (%s) must be less than or equal to max_delay (%s)", 
			pc.BaseDelay, pc.MaxDelay)
	}

	validStrategies := []string{
		"fixed", "linear", "exponential", "exponential_jitter", 
		"api_rate_limit", "network",
	}
	
	if pc.BackoffStrategy != "" {
		valid := false
		for _, strategy := range validStrategies {
			if pc.BackoffStrategy == strategy {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid backoff_strategy '%s', must be one of: %v", 
				pc.BackoffStrategy, validStrategies)
		}
	}

	return nil
}

// Validate validates a CircuitBreakerConfig.
func (cbc *CircuitBreakerConfig) Validate() error {
	if cbc.FailureThreshold <= 0 {
		return fmt.Errorf("failure_threshold must be positive, got %d", cbc.FailureThreshold)
	}

	if cbc.ResetTimeout <= 0 {
		return fmt.Errorf("reset_timeout must be positive, got %s", cbc.ResetTimeout)
	}

	return nil
}

// parseBackoffStrategy converts a string to a BackoffFunc.
func parseBackoffStrategy(strategy string) (BackoffFunc, error) {
	switch strategy {
	case "", "exponential":
		return ExponentialBackoff, nil
	case "exponential_jitter":
		return ExponentialBackoffWithJitter, nil
	case "linear":
		return LinearBackoff, nil
	case "fixed":
		return FixedBackoff, nil
	case "api_rate_limit":
		return APIRateLimitBackoff, nil
	case "network":
		return NetworkBackoff, nil
	default:
		return nil, fmt.Errorf("unknown backoff strategy: %s", strategy)
	}
}

// createCustomRetryableFunc creates a RetryableFunc based on error patterns.
func createCustomRetryableFunc(retryableErrors, nonRetryableErrors []string) RetryableFunc {
	return func(err error) bool {
		if err == nil {
			return false
		}

		errStr := toLower(err.Error())

		// Check non-retryable patterns first (higher priority)
		for _, pattern := range nonRetryableErrors {
			if contains(errStr, toLower(pattern)) {
				return false
			}
		}

		// Check retryable patterns
		for _, pattern := range retryableErrors {
			if contains(errStr, toLower(pattern)) {
				return true
			}
		}

		// Fall back to default behavior
		return DefaultRetryableFunc(err)
	}
}

// GetOperationPolicy returns the retry policy for a specific operation.
func (c *Config) GetOperationPolicy(operation string) (*RetryPolicy, error) {
	if policy, exists := c.OperationPolicies[operation]; exists {
		return policy.ToRetryPolicy()
	}
	return c.DefaultPolicy.ToRetryPolicy()
}

// GetCircuitBreakerConfig returns the circuit breaker config for a specific operation.
func (c *Config) GetCircuitBreakerConfig(name string) (CircuitBreakerConfig, bool) {
	config, exists := c.CircuitBreakers[name]
	return config, exists
}

// SetDefaults sets default values for missing configuration fields.
func (c *Config) SetDefaults() {
	// Set default policy defaults
	if c.DefaultPolicy.MaxAttempts <= 0 {
		c.DefaultPolicy.MaxAttempts = 3
	}
	if c.DefaultPolicy.BaseDelay <= 0 {
		c.DefaultPolicy.BaseDelay = time.Second
	}
	if c.DefaultPolicy.MaxDelay <= 0 {
		c.DefaultPolicy.MaxDelay = 30 * time.Second
	}
	if c.DefaultPolicy.BackoffStrategy == "" {
		c.DefaultPolicy.BackoffStrategy = "exponential_jitter"
	}

	// Initialize maps if nil
	if c.CircuitBreakers == nil {
		c.CircuitBreakers = make(map[string]CircuitBreakerConfig)
	}
	if c.OperationPolicies == nil {
		c.OperationPolicies = make(map[string]PolicyConfig)
	}

	// Set defaults for each circuit breaker
	for name, cb := range c.CircuitBreakers {
		if cb.FailureThreshold <= 0 {
			cb.FailureThreshold = 5
		}
		if cb.ResetTimeout <= 0 {
			cb.ResetTimeout = time.Minute
		}
		c.CircuitBreakers[name] = cb
	}

	// Set defaults for each operation policy
	for name, policy := range c.OperationPolicies {
		if policy.MaxAttempts <= 0 {
			policy.MaxAttempts = c.DefaultPolicy.MaxAttempts
		}
		if policy.BaseDelay <= 0 {
			policy.BaseDelay = c.DefaultPolicy.BaseDelay
		}
		if policy.MaxDelay <= 0 {
			policy.MaxDelay = c.DefaultPolicy.MaxDelay
		}
		if policy.BackoffStrategy == "" {
			policy.BackoffStrategy = c.DefaultPolicy.BackoffStrategy
		}
		c.OperationPolicies[name] = policy
	}
}