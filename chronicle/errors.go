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
	Method string
	URL    string
	Status int
	Body   string
}

func (e *APIError) Error() string {
	const max = 2000
	body := e.Body
	if len(body) > max {
		body = body[:max] + "…(truncated)"
	}
	return fmt.Sprintf("chronicle: %s %s -> HTTP %d: %s", e.Method, e.URL, e.Status, body)
}

// IsNotFound reports whether err is an APIError with a 404 status. Useful for
// the numeric-vs-string project-form fallback.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}
