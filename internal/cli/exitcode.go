package cli

import (
	"errors"

	"github.com/domehahn/housekeeping/internal/provider"
)

// Documented exit codes. main.go maps a returned error to one of these via
// errors.As against *ExitError; anything else falls back to 1.
const (
	ExitSuccess              = 0
	ExitGeneralError         = 1
	ExitInvalidConfiguration = 2
	ExitAuthenticationError  = 3
	ExitAuthorizationError   = 4
	ExitSafetyGuardTriggered = 5
	ExitPartialExecution     = 6
	ExitPlanValidationFailed = 7
)

// ExitError carries an explicit exit code alongside its message so command
// bodies can signal exactly which failure category occurred without the
// top-level runner having to re-inspect error internals.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

func exitErr(code int, err error) error {
	if err == nil {
		return nil
	}
	return &ExitError{Code: code, Err: err}
}

// wrapProviderErr classifies a provider.Error (typically the result of a
// network call) into the matching documented exit code. Errors that are
// not a *provider.Error fall back to ExitGeneralError.
func wrapProviderErr(err error) error {
	if err == nil {
		return nil
	}
	var pErr *provider.Error
	if errors.As(err, &pErr) {
		switch pErr.Kind {
		case provider.KindAuthentication:
			return exitErr(ExitAuthenticationError, err)
		case provider.KindAuthorization:
			return exitErr(ExitAuthorizationError, err)
		}
	}
	return exitErr(ExitGeneralError, err)
}
