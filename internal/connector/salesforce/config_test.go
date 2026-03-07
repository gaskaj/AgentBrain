package salesforce

import (
	"strings"
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &Config{
				ClientID:      "test_client_id",
				ClientSecret:  "test_client_secret",
				Username:      "test@example.com",
				Password:      "test_password",
				SecurityToken: "test_token",
				LoginURL:      "https://login.salesforce.com",
				APIVersion:    "v59.0",
			},
			wantErr: false,
		},
		{
			name: "missing required field - client_id",
			config: &Config{
				ClientSecret:  "test_client_secret",
				Username:      "test@example.com",
				Password:      "test_password",
				SecurityToken: "test_token",
			},
			wantErr: true,
			errMsg:  "client_id is required",
		},
		{
			name: "missing required field - client_secret",
			config: &Config{
				ClientID:      "test_client_id",
				Username:      "test@example.com",
				Password:      "test_password",
				SecurityToken: "test_token",
			},
			wantErr: true,
			errMsg:  "client_secret is required",
		},
		{
			name: "invalid email",
			config: &Config{
				ClientID:      "test_client_id",
				ClientSecret:  "test_client_secret",
				Username:      "invalid-email",
				Password:      "test_password",
				SecurityToken: "test_token",
			},
			wantErr: true,
			errMsg:  "username must be a valid email address",
		},
		{
			name: "invalid URL",
			config: &Config{
				ClientID:      "test_client_id",
				ClientSecret:  "test_client_secret",
				Username:      "test@example.com",
				Password:      "test_password",
				SecurityToken: "test_token",
				LoginURL:      "://invalid-url",
			},
			wantErr: true,
			errMsg:  "login_url must be a valid URL",
		},
		{
			name: "invalid API version",
			config: &Config{
				ClientID:      "test_client_id",
				ClientSecret:  "test_client_secret",
				Username:      "test@example.com",
				Password:      "test_password",
				SecurityToken: "test_token",
				APIVersion:    "invalid-version",
			},
			wantErr: true,
			errMsg:  "api_version must be in format vXX.X (e.g., v59.0)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, should contain %v", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestConfig_SetDefaults(t *testing.T) {
	config := &Config{
		ClientID:      "test_client_id",
		ClientSecret:  "test_client_secret",
		Username:      "test@example.com",
		Password:      "test_password",
		SecurityToken: "test_token",
	}

	config.SetDefaults()

	if config.LoginURL != "https://login.salesforce.com" {
		t.Errorf("SetDefaults() LoginURL = %v, want %v", config.LoginURL, "https://login.salesforce.com")
	}

	if config.APIVersion != "v59.0" {
		t.Errorf("SetDefaults() APIVersion = %v, want %v", config.APIVersion, "v59.0")
	}
}

func TestFromMap(t *testing.T) {
	tests := []struct {
		name    string
		auth    map[string]interface{}
		options map[string]interface{}
		wantErr bool
	}{
		{
			name: "valid config from map",
			auth: map[string]interface{}{
				"client_id":      "test_client_id",
				"client_secret":  "test_client_secret",
				"username":       "test@example.com",
				"password":       "test_password",
				"security_token": "test_token",
				"login_url":      "https://login.salesforce.com",
			},
			options: map[string]interface{}{
				"api_version": "v59.0",
			},
			wantErr: false,
		},
		{
			name: "missing required field",
			auth: map[string]interface{}{
				"client_id":     "test_client_id",
				"client_secret": "test_client_secret",
				// missing username, password, security_token
			},
			options: map[string]interface{}{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := FromMap(tt.auth, tt.options)
			if tt.wantErr {
				if err == nil {
					t.Errorf("FromMap() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("FromMap() unexpected error = %v", err)
				}
				if config == nil {
					t.Errorf("FromMap() expected config but got nil")
				}
			}
		})
	}
}

func TestConfig_Schema(t *testing.T) {
	config := &Config{}
	schema := config.Schema()

	if schema == nil {
		t.Errorf("Schema() returned nil")
		return
	}

	// Check that the schema has the expected structure
	if schema["type"] != "object" {
		t.Errorf("Schema() type = %v, want %v", schema["type"], "object")
	}

	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Errorf("Schema() properties is not a map")
		return
	}

	// Check that required fields are present
	requiredFields := []string{"client_id", "client_secret", "username", "password", "security_token"}
	for _, field := range requiredFields {
		if _, exists := properties[field]; !exists {
			t.Errorf("Schema() missing required field: %s", field)
		}
	}

	// Check that required array is present
	required, ok := schema["required"].([]string)
	if !ok {
		t.Errorf("Schema() required is not a string slice")
		return
	}

	if len(required) != 5 {
		t.Errorf("Schema() required length = %d, want 5", len(required))
	}
}