package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/soar"
)

// errorEnvelope is the structured `--json` form of a failed command. Agents and
// scripts branch on the fields instead of regexing the stderr prose line: `code`
// is a canonical google.rpc-style token, `retryable` follows the transport's
// retry policy, and `request_id` is the server correlation id when present.
type errorEnvelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Status    int    `json:"status,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// newErrorEnvelope classifies err into the structured envelope. It recognizes the
// SDK error types (chronicle + SOAR) and the drift sentinel; anything else is a
// generic non-retryable ERROR.
func newErrorEnvelope(err error) errorEnvelope {
	var ec *exitCoder
	if errors.As(err, &ec) && ec.ExitCode() == 2 {
		return errorEnvelope{Code: "DRIFT", Message: err.Error()}
	}
	var ce *chronicle.APIError
	if errors.As(err, &ce) {
		return errorEnvelope{
			Code:      statusCodeName(ce.Status),
			Message:   err.Error(),
			Retryable: ce.Retryable(),
			Status:    ce.Status,
			RequestID: ce.RequestID,
		}
	}
	// soar.Error and legacy.Error are both aliases of the same transport.Error,
	// so this single check covers both SOAR planes.
	var se *soar.Error
	if errors.As(err, &se) {
		return errorEnvelope{
			Code:      statusCodeName(se.Status),
			Message:   err.Error(),
			Retryable: se.Retryable(),
			Status:    se.Status,
			RequestID: se.RequestID,
		}
	}
	return errorEnvelope{Code: "ERROR", Message: err.Error()}
}

// statusCodeName maps an HTTP status to a canonical google.rpc.Code token, the
// same vocabulary the SecOps APIs use in their error bodies.
func statusCodeName(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "INVALID_ARGUMENT"
	case http.StatusUnauthorized:
		return "UNAUTHENTICATED"
	case http.StatusForbidden:
		return "PERMISSION_DENIED"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusConflict:
		return "ABORTED"
	case http.StatusTooManyRequests:
		return "RESOURCE_EXHAUSTED"
	case http.StatusNotImplemented:
		return "UNIMPLEMENTED"
	case http.StatusInternalServerError:
		return "INTERNAL"
	case http.StatusServiceUnavailable:
		return "UNAVAILABLE"
	case http.StatusGatewayTimeout:
		return "DEADLINE_EXCEEDED"
	default:
		if status > 0 {
			return fmt.Sprintf("HTTP_%d", status)
		}
		return "ERROR"
	}
}

// renderErrorJSON writes the structured envelope for err to STDERR. Errors go to
// stderr (not stdout) so the envelope never concatenates with a command's stdout
// — a partial preview already written, or a success payload — which would leave a
// `--json` consumer with unparseable mixed output. Returns false if marshaling
// fails so the caller can fall back to the plain-text rendering.
func renderErrorJSON(err error) bool {
	b, mErr := json.MarshalIndent(newErrorEnvelope(err), "", "  ")
	if mErr != nil {
		return false
	}
	fmt.Fprintln(os.Stderr, string(b))
	return true
}
