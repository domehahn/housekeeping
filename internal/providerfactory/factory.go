// Package providerfactory constructs a provider.Client from Config. It is
// the only place outside cmd/ that is allowed to import a concrete adapter
// package - the rest of the CLI and all of internal/app depend solely on
// internal/provider's interfaces.
//
// Adding a new provider (e.g. GitHub) means adding one case here that
// constructs and returns that adapter; internal/app, internal/policy, and
// the CLI command bodies do not change. See README "Adding another
// Provider" for a worked example.
package providerfactory

import (
	"fmt"
	"log/slog"

	"github.com/domehahn/housekeeping/internal/adapters/gitlab"
	"github.com/domehahn/housekeeping/internal/config"
	"github.com/domehahn/housekeeping/internal/provider"
)

// New builds the configured provider's Client. The access token is
// resolved from the environment variable named by the provider's
// token_env setting - never read from configuration or a CLI flag.
func New(cfg config.Config, logger *slog.Logger) (provider.Client, error) {
	switch cfg.Provider.Type {
	case "gitlab":
		token, err := config.ResolveToken(cfg.Provider.GitLab.TokenEnv)
		if err != nil {
			return nil, err
		}
		if cfg.Provider.GitLab.InsecureSkipTLSVerify {
			logger.Warn("TLS certificate verification is disabled (insecure_skip_tls_verify=true) - traffic to this GitLab instance is not authenticated and may be intercepted")
		}
		adapter, err := gitlab.New(gitlab.Options{
			BaseURL:               cfg.Provider.GitLab.BaseURL,
			Token:                 token,
			InsecureSkipTLSVerify: cfg.Provider.GitLab.InsecureSkipTLSVerify,
			Workers:               cfg.Performance.Workers,
		})
		if err != nil {
			return nil, fmt.Errorf("build gitlab provider: %w", err)
		}
		return adapter, nil
	default:
		return nil, fmt.Errorf("unsupported provider type %q (supported: gitlab)", cfg.Provider.Type)
	}
}
