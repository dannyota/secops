package chronicle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"danny.vn/secops/internal/httpretry"
)

// --- retry plumbing ----------------------------------------------------------

// retryStatuses are the HTTP statuses worth retrying with backoff. A 5xx is only
// retried for idempotent methods (see retryable); 429 is retried for any method.
// DEVIATION: explicit and bounded, rather than buried in transport defaults.
var retryStatuses = map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true}

const maxRetries = 4

// baseBackoff is the first retry delay; subsequent attempts back off ×2 (with
// jitter, in httpretry). A package var so tests can zero it for instant retries.
var baseBackoff = 300 * time.Millisecond

// retryPolicy is the shared default policy with the package knobs overlaid, so a
// test that zeros baseBackoff still gets instant retries. Budget bounds the TOTAL
// backoff across the loop (so an honored 429 hint can't hang a command for
// minutes), not just a single wait.
func retryPolicy() httpretry.Policy {
	p := httpretry.DefaultPolicy()
	p.MaxAttempts = maxRetries + 1
	p.Base = baseBackoff
	return p
}

// idempotentMethod reports whether retrying method is side-effect-safe. Only these
// may be retried on a 5xx or a transport error; POST/PATCH are NOT — a 5xx on a
// mutation may have already applied server-side (create-despite-error), so
// retrying it duplicates the side effect (CLAUDE.md). Mirrors the SOAR transport.
func idempotentMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

// retryable decides whether a response/error for method warrants another attempt:
// a transport error (no status) or a 5xx is retried only for idempotent methods;
// 429 (rejected before processing) is retried for any method.
func retryable(method string, status int, transportErr bool) bool {
	if transportErr {
		return idempotentMethod(method)
	}
	if status == 429 {
		return true
	}
	if retryStatuses[status] { // 5xx (429 already returned above)
		return idempotentMethod(method)
	}
	return false
}

// --- streaming & pagination --------------------------------------------------

// streamArray POSTs body to path and, on a 2xx, decodes the JSON-array response
// body element-by-element AS IT ARRIVES (json.Decoder reads incrementally off the
// wire), invoking onElem per element. A non-2xx becomes *APIError. There is NO
// retry: this serves non-idempotent streaming POSTs, where a 5xx may already have
// applied server-side, and the value here is the progressive stream. onElem
// returning a non-nil error stops decoding and is returned (the body is closed).
func (c *Client) streamArray(ctx context.Context, path string, body any, onElem func(json.RawMessage) error, opts ...requestOption) error {
	spec := &requestSpec{}
	for _, o := range opts {
		o(spec)
	}
	base := c.baseURL
	if spec.version != "" {
		if def := "/" + DefaultAPIVersion; strings.HasSuffix(base, def) {
			base = base[:len(base)-len(def)] + "/" + spec.version
		}
	}
	full := base + "/" + strings.TrimLeft(path, "/")
	if len(spec.query) > 0 {
		full += "?" + spec.query.Encode()
	}

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("chronicle: marshal request body: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, full, reader)
	if err != nil {
		return fmt.Errorf("chronicle: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("chronicle: POST request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return &APIError{Method: http.MethodPost, URL: full, Status: resp.StatusCode, Body: string(data), RequestID: requestIDFromHeader(resp.Header)}
	}

	dec := json.NewDecoder(resp.Body)
	// The streamed body is a JSON array of chunk objects: consume the opening '['
	// then decode each element as it streams in. An empty body is a no-op.
	tok, err := dec.Token()
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("chronicle: read stream: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return fmt.Errorf("chronicle: streamed response is not a JSON array (got %v)", tok)
	}
	for dec.More() {
		var elem json.RawMessage
		if err := dec.Decode(&elem); err != nil {
			return fmt.Errorf("chronicle: decode stream element: %w", err)
		}
		if err := onElem(elem); err != nil {
			return err
		}
	}
	return nil
}

// paginate repeatedly invokes fetch with the current page token until fetch
// returns an empty next-token or maxPages is reached. fetch receives "" on the
// first call and returns the nextPageToken from that page.
//
// DEVIATION: one generic paginator, vs the wrapper's per-method token loops.
func paginate(maxPages int, fetch func(pageToken string) (next string, err error)) error {
	token := ""
	for range maxPages {
		next, err := fetch(token)
		if err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		token = next
	}
	return nil
}
