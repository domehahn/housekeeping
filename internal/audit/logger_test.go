package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/domehahn/housekeeping/internal/domain"
)

func TestLoggerWritesOwnerOnlyJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	logger, err := NewLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	record := Record{Timestamp: time.Now(), Provider: "gitlab", ResourceType: domain.ResourceTypeProject, ResourceID: "1", Action: domain.ActionDeleteProject, Result: domain.ResultSuccess}
	if err := logger.Write(record); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("audit permissions = %o, want 600", info.Mode().Perm())
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("expected one audit line")
	}
	var got Record
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ResourceID != "1" || got.Result != domain.ResultSuccess {
		t.Fatalf("unexpected audit record: %+v", got)
	}
}
