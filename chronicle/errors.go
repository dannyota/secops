package chronicle

import (
	"errors"
	"fmt"
	"net/http"
)

// APIError is a non-2xx response from the Chronicle API.
//
// DEVIATION (vs the official Python wrapper): it surfaces the HTTP method,
// URL, status, and (truncated) body instead of collapsing everything into a
// generic message. Callers can branch on Status (see IsNotFound).
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
	return fmt.Sprintf("chronicle: %s %s -> HTTP %d%s: %s", e.Method, e.URL, e.Status, rid, body)
}

// requestIDHeaders are the response headers that may carry a server request id,
// in priority order. Surfaced on APIError so a failed call is traceable in a
// support escalation.
var requestIDHeaders = []string{"X-Goog-Request-Id", "X-Request-Id", "X-Cloud-Trace-Context"}

// requestIDFromHeader returns the first request-id header present, or "".
func requestIDFromHeader(h http.Header) string {
	for _, k := range requestIDHeaders {
		if v := h.Get(k); v != "" {
			return v
		}
	}
	return ""
}

// IsNotFound reports whether err is an APIError with a 404 status. Useful for
// the numeric-vs-string project-form fallback.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}
