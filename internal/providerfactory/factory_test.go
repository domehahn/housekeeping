package providerfactory

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/domehahn/housekeeping/internal/adapters/gitlab"
	"github.com/domehahn/housekeeping/internal/config"
	"github.com/domehahn/housekeeping/internal/provider"
	"github.com/domehahn/housekeeping/internal/secrets"
)

type recordingResolver struct {
	ref   secrets.Reference
	value string
	err   error
}

func (r *recordingResolver) Resolve(_ context.Context, ref secrets.Reference) (string, error) {
	r.ref = ref
	return r.value, r.err
}

func TestFactoryResolvesReferenceAndPassesOnlyTokenToAdapter(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.GitLab.BaseURL = "https://gitlab.example.com"
	cfg.Provider.GitLab.Token = config.TokenConfig{Source: secrets.SourceKeychain, Service: "scm-cleaner", Account: "alice"}
	resolver := &recordingResolver{value: "resolved-secret"}
	var options gitlab.Options

	client, err := newWithGitLabBuilder(context.Background(), cfg, resolver, slog.Default(), func(got gitlab.Options) (provider.Client, error) {
		options = got
		return nil, nil
	})
	if err != nil || client != nil {
		t.Fatalf("newWithGitLabBuilder() = %v, %v", client, err)
	}
	if resolver.ref != (secrets.Reference{Source: secrets.SourceKeychain, Service: "scm-cleaner", Account: "alice"}) {
		t.Fatalf("resolver reference = %+v", resolver.ref)
	}
	if options.Token != "resolved-secret" || options.BaseURL != cfg.Provider.GitLab.BaseURL {
		t.Fatalf("adapter options = %+v", options)
	}
}

func TestFactoryDoesNotLeakSecretInConstructionError(t *testing.T) {
	cfg := config.Default()
	cfg.Provider.GitLab.BaseURL = "not a valid URL"
	cfg.Provider.GitLab.TokenEnv = "GITLAB_TOKEN"
	resolver := &recordingResolver{value: "do-not-leak-this-token"}
	_, err := New(context.Background(), cfg, resolver, slog.Default())
	if err == nil {
		t.Fatal("expected adapter construction error")
	}
	if strings.Contains(err.Error(), resolver.value) {
		t.Fatalf("error leaked resolved token: %v", err)
	}
}
