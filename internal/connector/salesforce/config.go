package salesforce

import (
	"fmt"
	"net/url"
	"strings"
)

// Config represents the structured configuration for the Salesforce connector.
type Config struct {
	ClientID      string `yaml:"client_id" validate:"required"`
	ClientSecret  string `yaml:"client_secret" validate:"required"`
	Username      string `yaml:"username" validate:"required,email"`
	Password      string `yaml:"password" validate:"required"`
	SecurityToken string `yaml:"security_token" validate:"required"`
	LoginURL      string `yaml:"login_url" validate:"url"`
	APIVersion    string `yaml:"api_version" validate:"semver"`
}

// SetDefaults sets default values for optional fields.
func (c *Config) SetDefaults() {
	if c.LoginURL == "" {
		c.LoginURL = "https://login.salesforce.com"
	}
	if c.APIVersion == "" {
		c.APIVersion = "v59.0"
	}
}

// Validate performs validation on the configuration.
func (c *Config) Validate() error {
	// Required field validation
	if c.ClientID == "" {
		return fmt.Errorf("client_id is required")
	}
	if c.ClientSecret == "" {
		return fmt.Errorf("client_secret is required")
	}
	if c.Username == "" {
		return fmt.Errorf("username is required")
	}
	if c.Password == "" {
		return fmt.Errorf("password is required")
	}
	if c.SecurityToken == "" {
		return fmt.Errorf("security_token is required")
	}

	// Email validation for username
	if c.Username != "" && !isValidEmail(c.Username) {
		return fmt.Errorf("username must be a valid email address")
	}

	// URL validation for login_url
	if c.LoginURL != "" {
		if _, err := url.Parse(c.LoginURL); err != nil {
			return fmt.Errorf("login_url must be a valid URL: %w", err)
		}
	}

	// API version validation
	if c.APIVersion != "" && !isValidAPIVersion(c.APIVersion) {
		return fmt.Errorf("api_version must be in format vXX.X (e.g., v59.0)")
	}

	return nil
}

// ToAuthConfig converts the structured config to the legacy AuthConfig format.
func (c *Config) ToAuthConfig() AuthConfig {
	return AuthConfig{
		ClientID:      c.ClientID,
		ClientSecret:  c.ClientSecret,
		Username:      c.Username,
		Password:      c.Password,
		SecurityToken: c.SecurityToken,
		LoginURL:      c.LoginURL,
	}
}

// Schema returns a simple schema representation for the Salesforce connector configuration.
func (c *Config) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"client_id": map[string]interface{}{
				"type":        "string",
				"description": "Salesforce OAuth2 client ID",
				"required":    true,
			},
			"client_secret": map[string]interface{}{
				"type":        "string",
				"description": "Salesforce OAuth2 client secret",
				"required":    true,
			},
			"username": map[string]interface{}{
				"type":        "string",
				"description": "Salesforce username (must be a valid email)",
				"format":      "email",
				"required":    true,
			},
			"password": map[string]interface{}{
				"type":        "string",
				"description": "Salesforce password",
				"required":    true,
			},
			"security_token": map[string]interface{}{
				"type":        "string",
				"description": "Salesforce security token",
				"required":    true,
			},
			"login_url": map[string]interface{}{
				"type":        "string",
				"description": "Salesforce login URL",
				"format":      "url",
				"default":     "https://login.salesforce.com",
				"required":    false,
			},
			"api_version": map[string]interface{}{
				"type":        "string",
				"description": "Salesforce API version (e.g., v59.0)",
				"pattern":     "^v\\d+\\.\\d+$",
				"default":     "v59.0",
				"required":    false,
			},
		},
		"required": []string{"client_id", "client_secret", "username", "password", "security_token"},
	}
}

// isValidEmail performs basic email validation.
func isValidEmail(email string) bool {
	// Simple email validation - contains @ and at least one dot after @
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	if len(parts[0]) == 0 || len(parts[1]) == 0 {
		return false
	}
	return strings.Contains(parts[1], ".")
}

// isValidAPIVersion validates the API version format (vXX.X).
func isValidAPIVersion(version string) bool {
	if !strings.HasPrefix(version, "v") {
		return false
	}
	version = version[1:] // Remove the 'v' prefix
	parts := strings.Split(version, ".")
	if len(parts) != 2 {
		return false
	}
	// Basic check that both parts contain digits
	for _, part := range parts {
		if len(part) == 0 {
			return false
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}

// FromMap creates a Config from a map[string]interface{} (for backward compatibility).
func FromMap(auth map[string]interface{}, options map[string]interface{}) (*Config, error) {
	config := &Config{}

	// Extract from auth map
	if v, ok := auth["client_id"]; ok {
		if s, ok := v.(string); ok {
			config.ClientID = s
		}
	}
	if v, ok := auth["client_secret"]; ok {
		if s, ok := v.(string); ok {
			config.ClientSecret = s
		}
	}
	if v, ok := auth["username"]; ok {
		if s, ok := v.(string); ok {
			config.Username = s
		}
	}
	if v, ok := auth["password"]; ok {
		if s, ok := v.(string); ok {
			config.Password = s
		}
	}
	if v, ok := auth["security_token"]; ok {
		if s, ok := v.(string); ok {
			config.SecurityToken = s
		}
	}
	if v, ok := auth["login_url"]; ok {
		if s, ok := v.(string); ok {
			config.LoginURL = s
		}
	}

	// Extract from options map
	if v, ok := options["api_version"]; ok {
		if s, ok := v.(string); ok {
			config.APIVersion = s
		}
	}

	// Set defaults and validate
	config.SetDefaults()
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("salesforce config validation failed: %w", err)
	}

	return config, nil
}