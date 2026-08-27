// Package config defines the on-disk/CLI configuration schema and its
// validation. Configuration values are expressed in days (not raw
// durations) throughout the struct so YAML stays simple (`days: 90`); CLI
// flags that accept human durations like "30d" are converted to days at
// the CLI boundary via ParseDuration.
package config

import (
	"fmt"
	"regexp"
)

// Config is the root configuration schema, matching examples/config.yaml.
type Config struct {
	Version int `yaml:"version"`

	Provider ProviderConfig `yaml:"provider"`
	Scope    ScopeConfig    `yaml:"scope"`

	Projects ProjectsConfig `yaml:"projects"`
	Users    UsersConfig    `yaml:"users"`

	Safety      SafetyConfig      `yaml:"safety"`
	Execution   ExecutionConfig   `yaml:"execution"`
	Performance PerformanceConfig `yaml:"performance"`

	// Output is the default rendering format (table|json|yaml) when
	// --output is not given on the command line.
	Output string `yaml:"output"`
}

type ProviderConfig struct {
	Type   string       `yaml:"type"`
	GitLab GitLabConfig `yaml:"gitlab"`
}

type GitLabConfig struct {
	BaseURL string `yaml:"base_url"`
	// TokenEnv names the environment variable holding the access token.
	// The token itself is never stored in configuration.
	TokenEnv string `yaml:"token_env"`
	// InsecureSkipTLSVerify disables TLS certificate verification. Must be
	// explicitly opted into; the CLI prints a loud warning when enabled.
	InsecureSkipTLSVerify bool `yaml:"insecure_skip_tls_verify"`
}

type ScopeConfig struct {
	Group     string `yaml:"group"`
	Recursive bool   `yaml:"recursive"`
}

type ProjectsConfig struct {
	Inactive   ProjectInactiveConfig `yaml:"inactive"`
	Archived   ArchivedConfig        `yaml:"archived"`
	Include    []string              `yaml:"include"`
	Exclude    []string              `yaml:"exclude"`
	Protection ProjectProtection     `yaml:"protection"`
}

type ProjectInactiveConfig struct {
	Enabled bool `yaml:"enabled"`
	Days    int  `yaml:"days"`
}

type ArchivedConfig struct {
	Enabled bool `yaml:"enabled"`
}

type ProjectProtection struct {
	Paths []string `yaml:"paths"`
	Regex []string `yaml:"regex"`
}

type UsersConfig struct {
	Inactive           UserInactiveConfig `yaml:"inactive"`
	UnknownActivity    string             `yaml:"unknown_activity"` // skip|warn|match
	Protection         UserProtection     `yaml:"protection"`
	ExcludeCurrentUser bool               `yaml:"exclude_current_user"`
}

type UserInactiveConfig struct {
	Enabled          bool   `yaml:"enabled"`
	LastLoginDays    int    `yaml:"last_login_days"`
	LastActivityDays int    `yaml:"last_activity_days"`
	Match            string `yaml:"match"` // all|any

	// IgnoreGlobalActivityIfNonBillableElsewhere opts into the
	// billable-seat override: a user who is only protected by recent
	// activity elsewhere on the instance is still matched if they are
	// billable in the target group but hold no privileged membership
	// (see BillableAccessLevelThreshold) anywhere else. Disabled by
	// default - this weakens the "active elsewhere protects you"
	// guarantee, so it must be turned on deliberately. See
	// docs/adr/0004-billable-seat-override.md.
	IgnoreGlobalActivityIfNonBillableElsewhere bool `yaml:"ignore_global_activity_if_non_billable_elsewhere"`

	// BillableAccessLevelThreshold is the access level (guest|reporter|
	// developer|maintainer|owner) at or above which a membership in
	// another group counts as "binds a seat" for the override above.
	// Defaults to "developer" when the override is enabled and this is
	// left empty. This is an approximation - see the override's
	// documentation for why it cannot be GitLab's exact, tier-dependent
	// billing rule for groups this token does not own.
	BillableAccessLevelThreshold string `yaml:"billable_access_level_threshold"`
}

type UserProtection struct {
	Usernames    []string `yaml:"usernames"`
	Regex        []string `yaml:"regex"`
	AccessLevels []string `yaml:"access_levels"`
}

type SafetyConfig struct {
	MaxActions    MaxActionsConfig    `yaml:"max_actions"`
	MaxPercentage MaxPercentageConfig `yaml:"max_percentage"`
}

type MaxActionsConfig struct {
	Projects     int `yaml:"projects"`
	Users        int `yaml:"users"`
	PipelineTags int `yaml:"pipeline_tags"`
	RunnerTags   int `yaml:"runner_tags"`
}

type MaxPercentageConfig struct {
	Projects     int `yaml:"projects"`
	Users        int `yaml:"users"`
	PipelineTags int `yaml:"pipeline_tags"`
	RunnerTags   int `yaml:"runner_tags"`
}

type ExecutionConfig struct {
	Revalidate bool `yaml:"revalidate"`
	FailFast   bool `yaml:"fail_fast"`
}

type PerformanceConfig struct {
	Workers int `yaml:"workers"`
}

// Default returns a Config populated with safe defaults. Loading merges a
// config file, then CLI flags, on top of this - see
// docs/architecture.md "Configuration hierarchy".
func Default() Config {
	return Config{
		Version: 1,
		Provider: ProviderConfig{
			Type: "gitlab",
		},
		Scope: ScopeConfig{Recursive: false},
		Users: UsersConfig{
			UnknownActivity: "skip",
			Inactive:        UserInactiveConfig{Match: "all"},
		},
		Safety: SafetyConfig{
			MaxActions:    MaxActionsConfig{Projects: 10, Users: 20, PipelineTags: 10, RunnerTags: 5},
			MaxPercentage: MaxPercentageConfig{Projects: 0, Users: 0, PipelineTags: 0, RunnerTags: 0}, // 0 = disabled
		},
		Execution: ExecutionConfig{
			Revalidate: true,
			FailFast:   false,
		},
		Performance: PerformanceConfig{Workers: 5},
		Output:      "table",
	}
}

var validAccessLevels = map[string]bool{
	"guest": true, "reporter": true, "developer": true, "maintainer": true, "owner": true,
}

// Validate checks internal consistency of the configuration: known
// provider types, valid regexes, non-negative limits, and valid enum
// values. It never performs network I/O.
func (c Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("config: unsupported version %d (expected 1)", c.Version)
	}

	switch c.Provider.Type {
	case "gitlab":
		if c.Provider.GitLab.BaseURL == "" {
			return fmt.Errorf("config: provider.gitlab.base_url is required to connect to a GitLab instance " +
				"(set it in the config file, via --gitlab-url, or run a command that doesn't need a live " +
				"connection, e.g. `scm-cleaner provider list`)")
		}
		if c.Provider.GitLab.TokenEnv == "" {
			return fmt.Errorf("config: provider.gitlab.token_env is required and must name the environment " +
				"variable holding your GitLab access token (set it in the config file or via --token-env, " +
				"e.g. token_env: GITLAB_TOKEN, then `export GITLAB_TOKEN=...`)")
		}
	case "":
		return fmt.Errorf("config: provider.type is required (currently supported: gitlab)")
	default:
		return fmt.Errorf("config: unknown provider.type %q (currently supported: gitlab; run "+
			"`scm-cleaner provider list` to see what this build supports)", c.Provider.Type)
	}

	if c.Projects.Inactive.Enabled && c.Projects.Inactive.Days < 0 {
		return fmt.Errorf("config: projects.inactive.days must not be negative")
	}
	for _, p := range append(append([]string{}, c.Projects.Include...), c.Projects.Exclude...) {
		if err := validateRegex(p); err != nil {
			return fmt.Errorf("config: projects include/exclude: %w", err)
		}
	}
	for _, p := range c.Projects.Protection.Regex {
		if err := validateRegex(p); err != nil {
			return fmt.Errorf("config: projects.protection.regex: %w", err)
		}
	}

	if err := validateMatch(c.Users.Inactive.Match); err != nil {
		return fmt.Errorf("config: users.inactive.match: %w", err)
	}
	if c.Users.Inactive.Enabled {
		if c.Users.Inactive.LastLoginDays < 0 || c.Users.Inactive.LastActivityDays < 0 {
			return fmt.Errorf("config: users.inactive day thresholds must not be negative")
		}
	}
	switch c.Users.UnknownActivity {
	case "skip", "warn", "match", "":
	default:
		return fmt.Errorf("config: users.unknown_activity must be one of skip|warn|match, got %q", c.Users.UnknownActivity)
	}
	for _, r := range c.Users.Protection.Regex {
		if err := validateRegex(r); err != nil {
			return fmt.Errorf("config: users.protection.regex: %w", err)
		}
	}
	for _, lvl := range c.Users.Protection.AccessLevels {
		if !validAccessLevels[lvl] {
			return fmt.Errorf("config: users.protection.access_levels contains unknown level %q", lvl)
		}
	}
	if c.Users.Inactive.BillableAccessLevelThreshold != "" && !validAccessLevels[c.Users.Inactive.BillableAccessLevelThreshold] {
		return fmt.Errorf("config: users.inactive.billable_access_level_threshold contains unknown level %q", c.Users.Inactive.BillableAccessLevelThreshold)
	}

	if c.Safety.MaxActions.Projects < 0 || c.Safety.MaxActions.Users < 0 ||
		c.Safety.MaxActions.PipelineTags < 0 || c.Safety.MaxActions.RunnerTags < 0 {
		return fmt.Errorf("config: safety.max_actions values must not be negative")
	}
	for name, pct := range map[string]int{
		"projects": c.Safety.MaxPercentage.Projects, "users": c.Safety.MaxPercentage.Users,
		"pipeline_tags": c.Safety.MaxPercentage.PipelineTags, "runner_tags": c.Safety.MaxPercentage.RunnerTags,
	} {
		if pct < 0 || pct > 100 {
			return fmt.Errorf("config: safety.max_percentage.%s must be between 0 and 100", name)
		}
	}

	if c.Performance.Workers < 0 {
		return fmt.Errorf("config: performance.workers must not be negative")
	}

	switch c.Output {
	case "table", "json", "yaml", "":
	default:
		return fmt.Errorf("config: output must be one of table|json|yaml, got %q", c.Output)
	}

	return nil
}

func validateMatch(m string) error {
	switch m {
	case "all", "any", "":
		return nil
	default:
		return fmt.Errorf("must be one of all|any, got %q", m)
	}
}

func validateRegex(pattern string) error {
	if pattern == "" {
		return nil
	}
	if len(pattern) > 512 {
		return fmt.Errorf("pattern too long (%d chars): %q", len(pattern), pattern)
	}
	_, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid regex %q: %w", pattern, err)
	}
	return nil
}
