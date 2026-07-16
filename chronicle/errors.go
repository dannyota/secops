package chronicle

import (
	"errors"
	"fmt"
	"net/http"

	"danny.vn/secops/internal/httpretry"
)

// APIError is a non-2xx response from the Chronicle API.
//
// DEVIATION (vs the official Python wrapper): it surfaces the HTTP method,
// status, and (truncated) body instead of collapsing everything into a generic
// message. URL is retained for callers that need structured diagnostics; Error
// redacts it from the rendered message. Callers can branch on Status (see
// IsNotFound).
type APIError struct {
	Method    string
	URL       string
	Status    int
	Body      string
	RequestID string // server request id (for support escalation), when present
}

func (e *APIError) Error() string {
	const max = 2000
	body := e.Body
	if len(body) > max {
		body = body[:max] + "…(truncated)"
	}
	rid := ""
	if e.RequestID != "" {
		rid = " [request-id: " + e.RequestID + "]"
	}
	return fmt.Sprintf("chronicle: %s request failed with HTTP %d%s: %s", e.Method, e.Status, rid, body)
}

// Retryable reports whether the failed request is safe to retry under the
// transport's policy: a 429 (any method) or a 5xx on an idempotent method.
// Surfaced so a structured error can tell a caller whether a retry is sound.
func (e *APIError) Retryable() bool { return httpretry.Retryable(e.Method, e.Status, false) }

// IsNotFound reports whether err is an APIError with a 404 status. Useful for
// the numeric-vs-string project-form fallback.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}
