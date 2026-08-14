package audit

import (
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
	mu sync.Mutex
	w  io.WriteCloser
}

// NewLogger opens (creating if necessary, always appending) the audit log
// file at path. Permissions are restricted to the owner since an audit log
// can reveal which users/projects were targeted.
func NewLogger(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: open log %s: %w", path, err)
	}
	return &Logger{w: f}, nil
}

// Write appends one record as a single JSON line.
func (l *Logger) Write(r Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("audit: marshal record: %w", err)
	}
	data = append(data, '\n')
	if _, err := l.w.Write(data); err != nil {
		return fmt.Errorf("audit: write record: %w", err)
	}
	return nil
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
