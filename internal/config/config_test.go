package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
github:
  org: "my-org"
  review_teams:
    - "team-a"
    - "team-b"
  poll_interval: "5m"

workspace_dirs:
  - "/projects"
  - "/other"

repo_overrides:
  "org/special-repo": "/custom/path"

clone:
  default_dir: "/projects/clones"
  prompt_on_missing: false

shame_timer:
  green: 2
  yellow: 12
  red: 36

notifications:
  toast_enabled: false
  status_json_path: "/tmp/status.json"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0644))

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, "my-org", cfg.GitHub.Org)
	assert.Equal(t, []string{"team-a", "team-b"}, cfg.GitHub.ReviewTeams)
	assert.Equal(t, 5*time.Minute, cfg.GitHub.PollInterval)
	assert.Equal(t, []string{"/projects", "/other"}, cfg.WorkspaceDirs)
	assert.Equal(t, "/custom/path", cfg.RepoOverrides["org/special-repo"])
	assert.Equal(t, "/projects/clones", cfg.Clone.DefaultDir)
	assert.False(t, cfg.Clone.PromptOnMissing)
	assert.Equal(t, 2, cfg.ShameTimer.Green)
	assert.Equal(t, 12, cfg.ShameTimer.Yellow)
	assert.Equal(t, 36, cfg.ShameTimer.Red)
	assert.False(t, cfg.Notifications.ToastEnabled)
	assert.Equal(t, "/tmp/status.json", cfg.Notifications.StatusJSONPath)
}

func TestLoad_DefaultsApplied(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// Minimal config — only required fields
	yaml := `
github:
  org: "my-org"

workspace_dirs:
  - "/projects"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0644))

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, 3*time.Minute, cfg.GitHub.PollInterval)
	assert.Equal(t, 4, cfg.ShameTimer.Green)
	assert.Equal(t, 24, cfg.ShameTimer.Yellow)
	assert.Equal(t, 48, cfg.ShameTimer.Red)
	assert.True(t, cfg.Notifications.ToastEnabled)
	assert.True(t, cfg.Clone.PromptOnMissing)

	// StatusJSONPath default should be expanded (no ~)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".config/pr-monitor/status.json"), cfg.Notifications.StatusJSONPath)
}

func TestLoadDefault_NoConfigFile(t *testing.T) {
	// Point XDG_CONFIG_HOME to an empty temp dir so the file won't exist
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := LoadDefault()
	require.NoError(t, err)

	assert.Equal(t, 3*time.Minute, cfg.GitHub.PollInterval)
	assert.Equal(t, 4, cfg.ShameTimer.Green)
	assert.Equal(t, 24, cfg.ShameTimer.Yellow)
	assert.Equal(t, 48, cfg.ShameTimer.Red)
	assert.True(t, cfg.Notifications.ToastEnabled)
	assert.True(t, cfg.Clone.PromptOnMissing)

	// Path should be expanded
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".config/pr-monitor/status.json"), cfg.Notifications.StatusJSONPath)
}

func TestLoad_ValidationError_MissingOrg(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
github:
  review_teams:
    - "some-team"

workspace_dirs:
  - "/projects"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0644))

	_, err := Load(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github.org is required")
}

func TestLoad_ValidationError_NoWorkspaceDirs(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
github:
  org: "my-org"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0644))

	_, err := Load(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workspace_dirs")
}

func TestLoad_TildeExpansion(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
github:
  org: "my-org"

workspace_dirs:
  - "~/projects"
  - "/absolute/path"

repo_overrides:
  "org/repo": "~/overrides/repo"

clone:
  default_dir: "~/clones"

notifications:
  status_json_path: "~/status.json"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0644))

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(home, "projects"), cfg.WorkspaceDirs[0])
	assert.Equal(t, "/absolute/path", cfg.WorkspaceDirs[1])
	assert.Equal(t, filepath.Join(home, "overrides/repo"), cfg.RepoOverrides["org/repo"])
	assert.Equal(t, filepath.Join(home, "clones"), cfg.Clone.DefaultDir)
	assert.Equal(t, filepath.Join(home, "status.json"), cfg.Notifications.StatusJSONPath)
}

func TestLoad_PollIntervalParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{"minutes", "5m", 5 * time.Minute},
		{"seconds", "30s", 30 * time.Second},
		{"mixed", "1m30s", 90 * time.Second},
		{"hours", "1h", time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")

			yaml := `
github:
  org: "my-org"
  poll_interval: "` + tt.input + `"

workspace_dirs:
  - "/projects"
`
			require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0644))

			cfg, err := Load(cfgPath)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, cfg.GitHub.PollInterval)
		})
	}
}

func TestLoad_InvalidPollInterval(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
github:
  poll_interval: "not-a-duration"

workspace_dirs:
  - "/projects"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0644))

	_, err := Load(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid poll_interval")
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading config file")
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 3*time.Minute, cfg.GitHub.PollInterval)
	assert.Equal(t, 4, cfg.ShameTimer.Green)
	assert.Equal(t, 24, cfg.ShameTimer.Yellow)
	assert.Equal(t, 48, cfg.ShameTimer.Red)
	assert.True(t, cfg.Notifications.ToastEnabled)
	assert.Equal(t, "~/.config/pr-monitor/status.json", cfg.Notifications.StatusJSONPath)
	assert.True(t, cfg.Clone.PromptOnMissing)
	assert.Empty(t, cfg.GitHub.Org)
	assert.Nil(t, cfg.GitHub.ReviewTeams)
	assert.Nil(t, cfg.WorkspaceDirs)
	assert.Nil(t, cfg.RepoOverrides)
}

func TestLoadDefault_XDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Create a config file in the XDG location
	cfgDir := filepath.Join(dir, "pr-monitor")
	require.NoError(t, os.MkdirAll(cfgDir, 0755))

	yaml := `
github:
  org: "xdg-org"

workspace_dirs:
  - "/xdg-projects"
`
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(yaml), 0644))

	cfg, err := LoadDefault()
	require.NoError(t, err)

	assert.Equal(t, "xdg-org", cfg.GitHub.Org)
	assert.Equal(t, []string{"/xdg-projects"}, cfg.WorkspaceDirs)
}

func TestLoad_OrgWithoutTeams_Valid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
github:
  org: "my-org"

workspace_dirs:
  - "/projects"
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0644))

	cfg, err := Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "my-org", cfg.GitHub.Org)
	assert.Empty(t, cfg.GitHub.ReviewTeams)
}
