package httpretry

import "net/http"

// IdempotentMethod reports whether retrying method is side-effect-safe. Only
// these may be retried on a 5xx or a transport error; POST/PATCH are NOT — a
// 5xx on a mutation may have already applied server-side
// (create-despite-error), so retrying it duplicates the side effect.
func IdempotentMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

// retryStatuses5xx are the server errors safe to retry ONLY for idempotent
// methods; 429 (rejected before processing) is retried for any method.
var retryStatuses5xx = map[int]bool{500: true, 502: true, 503: true, 504: true}

// Retryable decides whether a response/error for method warrants another
// attempt: a transport error (no status) or a retryable 5xx is retried only
// for idempotent methods; 429 is retried for any method.
func Retryable(method string, status int, transportErr bool) bool {
	if transportErr {
		return IdempotentMethod(method)
	}
	if status == 429 {
		return true
	}
	if retryStatuses5xx[status] {
		return IdempotentMethod(method)
	}
	return false
}

// RequestIDHeaders are the response headers that may carry a server request
// id, in priority order. Surfaced on transport errors so a failed call is
// traceable in a support escalation.
var RequestIDHeaders = []string{"X-Goog-Request-Id", "X-Request-Id", "X-Cloud-Trace-Context"}

// RequestIDFromHeader returns the first request-id header present, or "".
func RequestIDFromHeader(h http.Header) string {
	for _, k := range RequestIDHeaders {
		if v := h.Get(k); v != "" {
			return v
		}
	}
	return ""
}
