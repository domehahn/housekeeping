package cli

import (
	"strings"
	"testing"
)

func TestVersion_TableOutput(t *testing.T) {
	stdout, _, err := runCmd(newVersionCmd(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "scm-cleaner") {
		t.Errorf("expected version output to name the binary, got:\n%s", stdout)
	}
}

func TestVersion_JSONOutput(t *testing.T) {
	stdout, _, err := runCmd(newVersionCmd(), []string{"--output", "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, `"version"`) {
		t.Errorf("expected JSON output with a version field, got:\n%s", stdout)
	}
}

func TestVersion_RejectsInvalidFormat(t *testing.T) {
	_, _, err := runCmd(newVersionCmd(), []string{"--output", "xml"})
	if err == nil {
		t.Fatal("expected an error for an unsupported output format")
	}
}
