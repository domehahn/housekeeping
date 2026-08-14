// Package gitlab is the only place in this codebase that is allowed to
// know about the GitLab API. It implements the ports declared in
// internal/provider by mapping GitLab SDK types to internal/domain types
// (see mapper.go) and GitLab HTTP errors to provider.Error (see errors.go).
//
// Client library: gitlab.com/gitlab-org/api/client-go (the official
// successor to xanzy/go-gitlab). It already implements exponential backoff
// retries for 429/5xx responses via hashicorp/go-retryablehttp and does not
// retry permanent 4xx errors, satisfying the resilience requirements
// without bespoke retry code here.
package gitlab

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// Options configures a new Adapter.
type Options struct {
	// BaseURL is the GitLab instance URL, e.g. "https://gitlab.example.com"
	// or "https://gitlab.example.com/api/v4". Normalized by normalizeBaseURL.
	BaseURL string
	// Token is the private/personal access token. Never logged.
	Token string
	// InsecureSkipTLSVerify disables certificate verification. Must be
	// explicitly requested by the caller; defaults to false everywhere in
	// this codebase.
	InsecureSkipTLSVerify bool
	// Workers bounds concurrency for read operations that fan out across
	// many resources (e.g. fetching per-user admin details).
	Workers int
	// RetryMax overrides the maximum number of retries for
	// retryable (429/5xx) responses. Defaults to 4. Tests use a lower
	// value so exercising a 500 response doesn't require waiting through
	// the full production backoff schedule.
	RetryMax int
}

// Adapter implements the provider.Client port set against a real GitLab
// instance. It holds no domain-level policy or planning logic - it only
// reads/writes GitLab resources and maps them to/from domain types.
type Adapter struct {
	gl       *gitlab.Client
	instance string // normalized instance URL, used for plan integrity checks
	workers  int
}

// New constructs a GitLab Adapter. It performs no network I/O itself.
func New(opts Options) (*Adapter, error) {
	if opts.Token == "" {
		return nil, fmt.Errorf("gitlab: token must not be empty")
	}
	baseURL, instance, err := normalizeBaseURL(opts.BaseURL)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: 60 * time.Second}
	if opts.InsecureSkipTLSVerify {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // explicit opt-in, warned at CLI layer
		}
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = 5
	}

	retryMax := opts.RetryMax
	if retryMax <= 0 {
		retryMax = 4
	}

	gl, err := gitlab.NewClient(opts.Token,
		gitlab.WithBaseURL(baseURL),
		gitlab.WithHTTPClient(httpClient),
		gitlab.WithCustomRetryMax(retryMax),
		gitlab.WithCustomRetryWaitMinMax(1*time.Second, 30*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("gitlab: build client: %w", err)
	}

	return &Adapter{gl: gl, instance: instance, workers: workers}, nil
}

// normalizeBaseURL accepts either an instance root ("https://gitlab.example.com")
// or an explicit API root ("https://gitlab.example.com/api/v4") and returns
// the API base URL the SDK expects plus the canonical instance URL used for
// plan-integrity comparisons (always the bare instance root, no trailing
// slash, no /api/v4 suffix).
func normalizeBaseURL(raw string) (apiBaseURL, instance string, err error) {
	if raw == "" {
		return "", "", fmt.Errorf("gitlab: base_url must not be empty")
	}
	trimmed := strings.TrimRight(raw, "/")
	instance = strings.TrimSuffix(trimmed, "/api/v4")
	instance = strings.TrimRight(instance, "/")
	if !strings.HasPrefix(instance, "http://") && !strings.HasPrefix(instance, "https://") {
		return "", "", fmt.Errorf("gitlab: base_url must include a scheme (http:// or https://): %q", raw)
	}
	return instance + "/api/v4", instance, nil
}

// Instance returns the canonical instance URL this adapter is connected to.
func (a *Adapter) Instance() string { return a.instance }
