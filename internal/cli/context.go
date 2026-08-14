package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/domehahn/housekeeping/internal/audit"
	"github.com/domehahn/housekeeping/internal/config"
	"github.com/domehahn/housekeeping/internal/domain"
	"github.com/domehahn/housekeeping/internal/output"
	"github.com/domehahn/housekeeping/internal/provider"
	"github.com/domehahn/housekeeping/internal/providerfactory"
)

// globalFlags holds every persistent (root-level) CLI flag. Values here
// take precedence over the config file and environment, per the
// documented configuration hierarchy: Default < Config File < Environment
// < CLI Flags.
type globalFlags struct {
	configPath string
	output     string

	group     string
	recursive bool

	gitlabURL string
	tokenEnv  string
	insecure  bool
	workers   int

	auditLogPath string
}

// env is the fully resolved runtime state shared by every command. It is
// built once, in the root command's PersistentPreRunE, and passed by
// pointer to leaf commands via closures - there is no package-level mutable
// state.
type env struct {
	flags  globalFlags
	cfg    config.Config
	log    *slog.Logger
	format output.Format
	clock  domain.Clock

	// client is created lazily by requireClient() since commands like
	// `config validate` and `version` must work without network access or
	// even a resolvable token.
	client provider.Client
}

// loadConfig merges the config file (if any) with global CLI flags. Flags
// that were explicitly set win over the file; unset flags leave the file's
// (or the built-in default's) value untouched.
func (e *env) loadConfig() error {
	cfg := config.Default()
	if e.flags.configPath != "" {
		if _, err := os.Stat(e.flags.configPath); err == nil {
			loaded, err := config.Load(e.flags.configPath)
			if err != nil {
				return exitErr(ExitInvalidConfiguration, err)
			}
			cfg = loaded
		} else if !os.IsNotExist(err) {
			return exitErr(ExitInvalidConfiguration, fmt.Errorf("stat config file %s: %w", e.flags.configPath, err))
		}
		// A missing default-path config file is not an error: all
		// settings can come from flags/environment instead.
	}

	if e.flags.output != "" {
		cfg.Output = e.flags.output
	}
	if e.flags.group != "" {
		cfg.Scope.Group = e.flags.group
	}
	if e.flags.recursive {
		cfg.Scope.Recursive = true
	}
	if e.flags.gitlabURL != "" {
		cfg.Provider.GitLab.BaseURL = e.flags.gitlabURL
	}
	if e.flags.tokenEnv != "" {
		cfg.Provider.GitLab.TokenEnv = e.flags.tokenEnv
	}
	if e.flags.insecure {
		cfg.Provider.GitLab.InsecureSkipTLSVerify = true
	}
	if e.flags.workers > 0 {
		cfg.Performance.Workers = e.flags.workers
	}
	if cfg.Provider.Type == "" {
		cfg.Provider.Type = "gitlab"
	}

	format, err := output.ParseFormat(cfg.Output)
	if err != nil {
		return exitErr(ExitInvalidConfiguration, err)
	}

	e.cfg = cfg
	e.format = format
	e.clock = domain.RealClock{}
	e.log = newLogger()
	return nil
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if os.Getenv("SCM_CLEANER_DEBUG") != "" {
		level = slog.LevelDebug
	}
	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}

// requireClient validates the provider configuration and builds the
// provider client, resolving the access token from the environment. This
// is only called by commands that actually need network access.
func (e *env) requireClient() (provider.Client, error) {
	if e.client != nil {
		return e.client, nil
	}
	if err := e.cfg.Validate(); err != nil {
		return nil, exitErr(ExitInvalidConfiguration, err)
	}
	client, err := providerfactory.New(e.cfg, e.log)
	if err != nil {
		return nil, exitErr(ExitAuthenticationError, err)
	}
	e.client = client
	return client, nil
}

func (e *env) auditWriter() (audit.Writer, error) {
	if e.flags.auditLogPath == "" {
		return audit.NoopLogger{}, nil
	}
	l, err := audit.NewLogger(e.flags.auditLogPath)
	if err != nil {
		return nil, exitErr(ExitGeneralError, err)
	}
	return l, nil
}
