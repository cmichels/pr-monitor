package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level pr-monitor configuration.
type Config struct {
	GitHub        GitHubConfig       `yaml:"github"`
	Jira          JiraConfig         `yaml:"jira"`
	WorkspaceDirs []string           `yaml:"workspace_dirs"`
	RepoOverrides map[string]string  `yaml:"repo_overrides"`
	Clone         CloneConfig        `yaml:"clone"`
	ShameTimer    ShameTimerConfig   `yaml:"shame_timer"`
	Notifications NotificationConfig `yaml:"notifications"`
}

// GitHubConfig holds GitHub-related settings.
type GitHubConfig struct {
	ReviewTeams  []string      `yaml:"review_teams"`
	Org          string        `yaml:"org"`
	PollInterval time.Duration `yaml:"poll_interval"`
}

// rawGitHubConfig is used for custom duration unmarshaling.
type rawGitHubConfig struct {
	ReviewTeams  []string `yaml:"review_teams"`
	Org          string   `yaml:"org"`
	PollInterval string   `yaml:"poll_interval"`
}

// UnmarshalYAML implements yaml.Unmarshaler to parse poll_interval as a duration string.
func (g *GitHubConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw rawGitHubConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}

	g.ReviewTeams = raw.ReviewTeams
	g.Org = raw.Org

	if raw.PollInterval != "" {
		d, err := time.ParseDuration(raw.PollInterval)
		if err != nil {
			return fmt.Errorf("invalid poll_interval %q: %w", raw.PollInterval, err)
		}
		g.PollInterval = d
	}

	return nil
}

// CloneConfig controls behavior when a repo isn't found locally.
type CloneConfig struct {
	DefaultDir      string `yaml:"default_dir"`
	PromptOnMissing bool   `yaml:"prompt_on_missing"`
}

// ShameTimerConfig defines hour thresholds for review age coloring.
type ShameTimerConfig struct {
	Green  int `yaml:"green"`
	Yellow int `yaml:"yellow"`
	Red    int `yaml:"red"`
}

// NotificationConfig controls toast and status output.
type NotificationConfig struct {
	ToastEnabled   bool   `yaml:"toast_enabled"`
	StatusJSONPath string `yaml:"status_json_path"`
}

// JiraConfig holds Jira integration settings.
type JiraConfig struct {
	BaseURL      string        `yaml:"base_url"`
	Project      string        `yaml:"project"`
	Filters      []JiraFilter  `yaml:"filters"`
	MyTasksJQL   string        `yaml:"my_tasks_jql"`
	PollInterval time.Duration `yaml:"poll_interval"`
}

// JiraFilter identifies a saved Jira filter by ID and display name.
type JiraFilter struct {
	ID   int    `yaml:"id"`
	Name string `yaml:"name"`
}

// rawJiraConfig is used for custom duration unmarshaling.
type rawJiraConfig struct {
	BaseURL      string       `yaml:"base_url"`
	Project      string       `yaml:"project"`
	Filters      []JiraFilter `yaml:"filters"`
	MyTasksJQL   string       `yaml:"my_tasks_jql"`
	PollInterval string       `yaml:"poll_interval"`
}

// UnmarshalYAML implements yaml.Unmarshaler to parse poll_interval as a duration string.
func (j *JiraConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw rawJiraConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}

	j.BaseURL = raw.BaseURL
	j.Project = raw.Project
	j.Filters = raw.Filters
	j.MyTasksJQL = raw.MyTasksJQL

	if raw.PollInterval != "" {
		d, err := time.ParseDuration(raw.PollInterval)
		if err != nil {
			return fmt.Errorf("invalid jira poll_interval %q: %w", raw.PollInterval, err)
		}
		j.PollInterval = d
	}

	return nil
}

// DefaultConfig returns a Config with all default values applied.
func DefaultConfig() *Config {
	return &Config{
		GitHub: GitHubConfig{
			PollInterval: 3 * time.Minute,
		},
		Jira: JiraConfig{
			PollInterval: 5 * time.Minute,
			Project:      "OP",
			MyTasksJQL:   `project = OP AND assignee = currentUser() AND status IN ("To Do", "In Progress") ORDER BY updated DESC`,
		},
		Clone: CloneConfig{
			PromptOnMissing: true,
		},
		ShameTimer: ShameTimerConfig{
			Green:  4,
			Yellow: 24,
			Red:    48,
		},
		Notifications: NotificationConfig{
			ToastEnabled:   true,
			StatusJSONPath: "~/.config/pr-monitor/status.json",
		},
	}
}

// LoadDefault resolves the default config path (respecting XDG_CONFIG_HOME)
// and loads the config. If the file doesn't exist, returns DefaultConfig.
func LoadDefault() (*Config, error) {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolving home directory: %w", err)
		}
		configDir = filepath.Join(home, ".config")
	}

	path := filepath.Join(configDir, "pr-monitor", "config.yaml")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := DefaultConfig()
		expandPaths(cfg)
		return cfg, nil
	}

	return Load(path)
}

// Load reads a YAML config file, applies defaults for missing fields, validates,
// and returns the resulting Config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	applyDefaults(cfg)
	expandPaths(cfg)

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// applyDefaults fills in zero-value fields with defaults.
func applyDefaults(cfg *Config) {
	defaults := DefaultConfig()

	if cfg.GitHub.PollInterval == 0 {
		cfg.GitHub.PollInterval = defaults.GitHub.PollInterval
	}
	if cfg.ShameTimer.Green == 0 {
		cfg.ShameTimer.Green = defaults.ShameTimer.Green
	}
	if cfg.ShameTimer.Yellow == 0 {
		cfg.ShameTimer.Yellow = defaults.ShameTimer.Yellow
	}
	if cfg.ShameTimer.Red == 0 {
		cfg.ShameTimer.Red = defaults.ShameTimer.Red
	}
	if cfg.Notifications.StatusJSONPath == "" {
		cfg.Notifications.StatusJSONPath = defaults.Notifications.StatusJSONPath
	}
	if cfg.Jira.PollInterval == 0 {
		cfg.Jira.PollInterval = defaults.Jira.PollInterval
	}
	if cfg.Jira.Project == "" {
		cfg.Jira.Project = defaults.Jira.Project
	}
	if cfg.Jira.MyTasksJQL == "" {
		cfg.Jira.MyTasksJQL = defaults.Jira.MyTasksJQL
	}
}

// validate checks config invariants.
func validate(cfg *Config) error {
	if len(cfg.GitHub.ReviewTeams) > 0 && cfg.GitHub.Org == "" {
		return fmt.Errorf("github.org is required when review_teams are configured")
	}
	if len(cfg.WorkspaceDirs) == 0 {
		return fmt.Errorf("at least one workspace_dirs entry is required")
	}
	return nil
}

// expandPaths replaces leading ~ with the user's home directory in all path fields.
func expandPaths(cfg *Config) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	expandTilde := func(p string) string {
		if strings.HasPrefix(p, "~/") {
			return filepath.Join(home, p[2:])
		}
		if p == "~" {
			return home
		}
		return p
	}

	for i, dir := range cfg.WorkspaceDirs {
		cfg.WorkspaceDirs[i] = expandTilde(dir)
	}

	for repo, path := range cfg.RepoOverrides {
		cfg.RepoOverrides[repo] = expandTilde(path)
	}

	cfg.Clone.DefaultDir = expandTilde(cfg.Clone.DefaultDir)
	cfg.Notifications.StatusJSONPath = expandTilde(cfg.Notifications.StatusJSONPath)
}
