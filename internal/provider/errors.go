// Package provider defines the ports (interfaces) through which the
// application layer talks to an SCM/VCS platform, plus the
// provider-independent error taxonomy adapters must translate their
// platform's errors into. Nothing in this package may import a
// provider-specific SDK.
package provider

import (
	"errors"
	"fmt"
)

// Kind classifies a provider error into a small, closed set the
// application layer can branch on without knowing which concrete provider
// produced it (e.g. to pick an exit code).
type Kind string

const (
	KindAuthentication Kind = "authentication" // invalid/missing credentials
	KindAuthorization  Kind = "authorization"  // valid credentials, insufficient rights
	KindNotFound       Kind = "not_found"
	KindRateLimit      Kind = "rate_limit"
	KindTemporary      Kind = "temporary" // retryable server-side/network error
	KindValidation     Kind = "validation"
	KindConflict       Kind = "conflict"
	KindUnknown        Kind = "unknown"
)

// Error is the generic error type every adapter must return for I/O
// failures. The application layer inspects Kind, never HTTP status codes
// or SDK-specific error types.
type Error struct {
	Kind    Kind
	Op      string // short description of the failed operation
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Op, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// Is allows errors.Is(err, provider.ErrAuthentication) style checks by
// comparing Kind against the sentinel errors below.
func (e *Error) Is(target error) bool {
	var t *Error
	if errors.As(target, &t) {
		return e.Kind == t.Kind
	}
	return false
}

// Sentinel errors usable with errors.Is(err, provider.ErrX) to classify a
// wrapped *Error without inspecting its fields directly.
var (
	ErrAuthentication = &Error{Kind: KindAuthentication}
	ErrAuthorization  = &Error{Kind: KindAuthorization}
	ErrNotFound       = &Error{Kind: KindNotFound}
	ErrRateLimit      = &Error{Kind: KindRateLimit}
	ErrTemporary      = &Error{Kind: KindTemporary}
	ErrValidation     = &Error{Kind: KindValidation}
	ErrConflict       = &Error{Kind: KindConflict}
)

// NewError constructs a classified provider Error.
func NewError(kind Kind, op, message string, err error) *Error {
	return &Error{Kind: kind, Op: op, Message: message, Err: err}
}
