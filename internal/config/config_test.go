package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	yaml := `
agent:
  log_level: debug
  log_format: text
  health_addr: ":9090"
  schedule: "@every 30m"

storage:
  bucket: test-bucket
  region: us-west-2

sources:
  test_sf:
    type: salesforce
    enabled: true
    concurrency: 2
    batch_size: 5000
    objects:
      - Account
      - Contact
    auth:
      client_id: "test-id"
      client_secret: "test-secret"
`

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0644))

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "debug", cfg.Agent.LogLevel)
	assert.Equal(t, "text", cfg.Agent.LogFormat)
	assert.Equal(t, ":9090", cfg.Agent.HealthAddr)
	assert.Equal(t, "test-bucket", cfg.Storage.Bucket)
	assert.Equal(t, "us-west-2", cfg.Storage.Region)

	src := cfg.Sources["test_sf"]
	require.NotNil(t, src)
	assert.Equal(t, "salesforce", src.Type)
	assert.True(t, src.Enabled)
	assert.Equal(t, 2, src.Concurrency)
	assert.Equal(t, 5000, src.BatchSize)
	assert.Equal(t, []string{"Account", "Contact"}, src.Objects)
}

func TestLoad_EnvVarExpansion(t *testing.T) {
	t.Setenv("TEST_BUCKET", "my-bucket")
	t.Setenv("TEST_REGION", "eu-west-1")

	yaml := `
storage:
  bucket: "${TEST_BUCKET}"
  region: "${TEST_REGION}"

sources:
  src:
    type: test
`

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0644))

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "my-bucket", cfg.Storage.Bucket)
	assert.Equal(t, "eu-west-1", cfg.Storage.Region)
}

func TestLoad_EnvVarDefaults(t *testing.T) {
	os.Unsetenv("NONEXISTENT_VAR")

	yaml := `
storage:
  bucket: "${NONEXISTENT_VAR:-fallback-bucket}"
  region: us-east-1

sources:
  src:
    type: test
`

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0644))

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "fallback-bucket", cfg.Storage.Bucket)
}

func TestLoad_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "missing bucket",
			yaml: `
storage:
  region: us-east-1
sources:
  s:
    type: test
`,
		},
		{
			name: "missing region",
			yaml: `
storage:
  bucket: b
sources:
  s:
    type: test
`,
		},
		{
			name: "no sources",
			yaml: `
storage:
  bucket: b
  region: r
`,
		},
		{
			name: "source missing type",
			yaml: `
storage:
  bucket: b
  region: r
sources:
  s:
    enabled: true
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0644))

			_, err := Load(path)
			assert.Error(t, err)
		})
	}
}
