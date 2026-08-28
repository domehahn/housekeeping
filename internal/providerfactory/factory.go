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
	"context"
	"fmt"
	"log/slog"

	"github.com/domehahn/housekeeping/internal/adapters/gitlab"
	"github.com/domehahn/housekeeping/internal/config"
	"github.com/domehahn/housekeeping/internal/provider"
	"github.com/domehahn/housekeeping/internal/secrets"
)

type gitLabBuilder func(gitlab.Options) (provider.Client, error)

// New builds the configured provider's Client. Credential resolution is
// delegated to the injected generic resolver; concrete adapters receive only
// the resolved token.
func New(ctx context.Context, cfg config.Config, resolver secrets.Resolver, logger *slog.Logger) (provider.Client, error) {
	return newWithGitLabBuilder(ctx, cfg, resolver, logger, func(options gitlab.Options) (provider.Client, error) {
		return gitlab.New(options)
	})
}

func newWithGitLabBuilder(ctx context.Context, cfg config.Config, resolver secrets.Resolver, logger *slog.Logger, build gitLabBuilder) (provider.Client, error) {
	switch cfg.Provider.Type {
	case "gitlab":
		if resolver == nil {
			return nil, fmt.Errorf("build gitlab provider: secret resolver is required")
		}
		ref, err := cfg.Provider.GitLab.SecretReference()
		if err != nil {
			return nil, err
		}
		token, err := resolver.Resolve(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("resolve gitlab credential: %w", err)
		}
		if cfg.Provider.GitLab.InsecureSkipTLSVerify {
			if logger == nil {
				logger = slog.Default()
			}
			logger.Warn("TLS certificate verification is disabled (insecure_skip_tls_verify=true) - traffic to this GitLab instance is not authenticated and may be intercepted")
		}
		adapter, err := build(gitlab.Options{
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
