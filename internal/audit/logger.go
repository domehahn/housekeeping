package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// Logger appends Records to a JSON Lines (one JSON object per line) file,
// suitable for `--audit-log cleanup-audit.jsonl`. It is safe for
// concurrent use, though execution itself is currently sequential.
type Logger struct {
	mu       sync.Mutex
	w        io.WriteCloser
	lastHash string
}

// NewLogger opens (creating if necessary, always appending) the audit log
// file at path. Permissions are restricted to the owner since an audit log
// can reveal which users/projects were targeted.
func NewLogger(path string) (*Logger, error) {
	var lastHash string
	if fRead, err := os.Open(path); err == nil {
		scanner := bufio.NewScanner(fRead)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var rec Record
			if err := json.Unmarshal(line, &rec); err == nil && rec.Hash != "" {
				lastHash = rec.Hash
			}
		}
		_ = fRead.Close()
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // path is the operator's own --audit-log CLI argument, not attacker-controlled input
	if err != nil {
		return nil, fmt.Errorf("audit: open log %s: %w", path, err)
	}
	return &Logger{w: f, lastHash: lastHash}, nil
}

// Write appends one record as a single JSON line with hash chain integrity.
func (l *Logger) Write(r Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	r.PreviousHash = l.lastHash
	r.Hash = ComputeRecordHash(r)
	l.lastHash = r.Hash

	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("audit: marshal record: %w", err)
	}
	data = append(data, '\n')
	if _, err := l.w.Write(data); err != nil {
		return fmt.Errorf("audit: write record: %w", err)
	}
	if f, ok := l.w.(*os.File); ok {
		_ = f.Sync()
	}
	return nil
}

// ComputeRecordHash computes the SHA-256 fingerprint for an audit record with its Hash cleared.
func ComputeRecordHash(r Record) string {
	r.Hash = ""
	data, err := json.Marshal(r)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyAuditChain validates every record in an audit log reader to confirm
// hash chain continuity and record content integrity.
func VerifyAuditChain(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	var expectedPrevious string
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			return fmt.Errorf("line %d: invalid JSON: %w", lineNo, err)
		}
		if rec.PreviousHash != expectedPrevious {
			return fmt.Errorf("line %d: hash chain broken (previousHash %q != expected %q)", lineNo, rec.PreviousHash, expectedPrevious)
		}
		claimedHash := rec.Hash
		if claimedHash == "" {
			return fmt.Errorf("line %d: record missing hash", lineNo)
		}
		computed := ComputeRecordHash(rec)
		if computed != claimedHash {
			return fmt.Errorf("line %d: record content modified (hash %q != computed %q)", lineNo, claimedHash, computed)
		}
		expectedPrevious = claimedHash
	}
	return scanner.Err()
}

// Close closes the underlying file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Close()
}

// NoopLogger discards records; used when --audit-log is not set.
type NoopLogger struct{}

func (NoopLogger) Write(Record) error { return nil }
func (NoopLogger) Close() error       { return nil }

// Writer is the interface commands depend on so a NoopLogger can stand in
// for a real Logger without a nil check at every call site.
type Writer interface {
	Write(Record) error
	Close() error
}

var (
	_ Writer = (*Logger)(nil)
	_ Writer = NoopLogger{}
)
