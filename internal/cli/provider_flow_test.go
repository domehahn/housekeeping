package cli

import (
	"strings"
	"testing"

	"github.com/domehahn/housekeeping/internal/config"
	"github.com/domehahn/housekeeping/internal/output"
	"github.com/domehahn/housekeeping/internal/provider"
)

func TestProviderList_NeedsNoClient(t *testing.T) {
	// No client set at all - provider list must never call requireClient().
	e := &env{cfg: config.Default(), format: output.FormatTable}
	stdout, _, err := runCmd(newProviderListCmd(e), nil)
	if err != nil {
		t.Fatalf("provider list must work without any client/config, got: %v", err)
	}
	if !strings.Contains(stdout, "gitlab") {
		t.Errorf("expected gitlab to be listed, got:\n%s", stdout)
	}
}

func TestProviderInfo_HappyPath(t *testing.T) {
	c := newFakeClient()
	c.info = fakeInfo()
	e := testEnv(c)

	stdout, _, err := runCmd(newProviderInfoCmd(e), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "bot") {
		t.Errorf("expected the authenticated identity in output, got:\n%s", stdout)
	}
}

func TestProviderCapabilities_HappyPath(t *testing.T) {
	c := newFakeClient()
	c.capabilities = provider.Capabilities{ListProjects: provider.SupportSupported}
	e := testEnv(c)

	stdout, _, err := runCmd(newProviderCapabilitiesCmd(e), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "supported") {
		t.Errorf("expected capability output, got:\n%s", stdout)
	}
}
