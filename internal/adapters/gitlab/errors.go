package gitlab

import (
	"errors"
	"net/http"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/domehahn/housekeeping/internal/provider"
)

// classify maps a GitLab SDK error (typically *gitlab.ErrorResponse, but
// also plain network errors) into the generic provider.Error taxonomy so
// that nothing outside this package needs to know about HTTP status codes
// or GitLab's error shapes.
func classify(op string, err error) error {
	if err == nil {
		return nil
	}

	// The SDK special-cases 404 as the plain sentinel gitlab.ErrNotFound
	// rather than an *ErrorResponse (see CheckResponse in the SDK), so it
	// must be checked before the *ErrorResponse case below.
	if errors.Is(err, gitlab.ErrNotFound) {
		return provider.NewError(provider.KindNotFound, op, "resource not found", err)
	}

	var errResp *gitlab.ErrorResponse
	if errors.As(err, &errResp) && errResp.Response != nil {
		kind := kindForStatus(errResp.Response.StatusCode)
		return provider.NewError(kind, op, safeMessage(errResp), err)
	}

	// No structured HTTP response available - treat as a temporary/network
	// error so callers can decide whether to retry rather than assuming a
	// hard failure.
	return provider.NewError(provider.KindTemporary, op, "network or transport error", err)
}

func kindForStatus(status int) provider.Kind {
	switch status {
	case http.StatusUnauthorized:
		return provider.KindAuthentication
	case http.StatusForbidden:
		return provider.KindAuthorization
	case http.StatusNotFound:
		return provider.KindNotFound
	case http.StatusTooManyRequests:
		return provider.KindRateLimit
	case http.StatusConflict:
		return provider.KindConflict
	case http.StatusUnprocessableEntity, http.StatusBadRequest:
		return provider.KindValidation
	default:
		if status >= 500 {
			return provider.KindTemporary
		}
		return provider.KindUnknown
	}
}

// safeMessage extracts a human-readable message without ever including
// request/response headers (which could carry the Authorization/PRIVATE-TOKEN
// header) or raw body bytes that might echo back request data.
func safeMessage(errResp *gitlab.ErrorResponse) string {
	if errResp.Message != "" {
		return errResp.Message
	}
	if errResp.Response != nil {
		return errResp.Response.Status
	}
	return "GitLab API error"
}
