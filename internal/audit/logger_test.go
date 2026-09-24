package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	defer func() { _ = f.Close() }()
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

func TestVerifyAuditChain_ValidChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	logger, err := NewLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	r1 := Record{Timestamp: time.Now(), Provider: "gitlab", ResourceType: domain.ResourceTypeProject, ResourceID: "1", Action: domain.ActionDeleteProject, Result: domain.ResultSuccess}
	r2 := Record{Timestamp: time.Now(), Provider: "gitlab", ResourceType: domain.ResourceTypeUser, ResourceID: "2", Action: domain.ActionBlockUser, Result: domain.ResultSuccess}
	_ = logger.Write(r1)
	_ = logger.Write(r2)
	_ = logger.Close()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := VerifyAuditChain(f); err != nil {
		t.Fatalf("VerifyAuditChain failed on valid chain: %v", err)
	}
}

func TestVerifyAuditChain_DetectsTampering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	logger, err := NewLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = logger.Write(Record{Timestamp: time.Now(), Provider: "gitlab", ResourceType: domain.ResourceTypeProject, ResourceID: "1", Action: domain.ActionDeleteProject, Result: domain.ResultSuccess})
	_ = logger.Write(Record{Timestamp: time.Now(), Provider: "gitlab", ResourceType: domain.ResourceTypeUser, ResourceID: "2", Action: domain.ActionBlockUser, Result: domain.ResultSuccess})
	_ = logger.Close()

	content, _ := os.ReadFile(path)
	tampered := strings.Replace(string(content), `"resourceId":"1"`, `"resourceId":"999"`, 1)
	_ = os.WriteFile(path, []byte(tampered), 0o600)

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := VerifyAuditChain(f); err == nil {
		t.Error("expected VerifyAuditChain to detect tampered record content")
	}
}
