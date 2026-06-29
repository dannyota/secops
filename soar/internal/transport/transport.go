// Package transport is the shared, durable HTTP plumbing for the Google SecOps
// SOAR API.
//
// Both the modern v1alpha client (danny.vn/secops/soar) and the external-API
// client (danny.vn/secops/soar/legacy) build on it. SOAR authenticates with the
// AppKey header — never ADC.
//
// Two request styles share one transport:
//
//   - V1Alpha: /v1alpha/projects/<num>/locations/<reg>/instances/<id>/<resource>,
//     with ?format=camel, the x-goog-api-version header, optional updateMask, and
//     Google-style {items,nextPageToken} pagination.
//   - External: /api/external/v1/<path> (the legacy Siemplify surface), with
//     offset-style {requestedPage,pageSize} pagination.
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"danny.vn/secops/auth"
	"danny.vn/secops/internal/httpretry"
)

// APIVersion is the SOAR API version the v1alpha surface targets.
const APIVersion = "v1alpha"

// ExternalPrefix is the path prefix of the legacy Siemplify external API.
const ExternalPrefix = "/api/external/v1"

// Settings identifies a tenant's SOAR instance.
type Settings struct {
	BaseURL       string // SOAR host, e.g. https://<tenant>.siemplify-soar.com
	ProjectNumber string // GCP project number (v1alpha resource path)
	Region        string // e.g. "us"
	CustomerID    string // Chronicle instance UUID
	ForceIPv4     bool   // pin the dialer to IPv4 (also via SECOPS_FORCE_IPV4)
}

// Error is a non-2xx response from the SOAR API. URL is retained for callers
// that need structured diagnostics; Error redacts it from the rendered message.
type Error struct {
	Method    string
	URL       string
	Status    int
	Body      string
	RequestID string // server request id (for support escalation), when present
}

func (e *Error) Error() string {
	const max = 2000
	body := e.Body
	if len(body) > max {
		body = body[:max] + "…(truncated)"
	}
	rid := ""
	if e.RequestID != "" {
		rid = " [request-id: " + e.RequestID + "]"
	}
	return fmt.Sprintf("soar: %s request failed with HTTP %d%s: %s", e.Method, e.Status, rid, body)
}

// Retryable reports whether the failed request is safe to retry under the
// transport's policy: a 429 (any method) or a 5xx on an idempotent method.
// Surfaced so a structured error can tell a caller whether a retry is sound.
func (e *Error) Retryable() bool { return retryable(e.Method, e.Status, false) }

// requestIDHeaders are the response headers that may carry a server request id.
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

// Transport executes authenticated SOAR requests. Safe for concurrent use.
type Transport struct {
	settings Settings
	base     string
	http     *http.Client
	// limiter paces outgoing requests so a bursty multi-call operation can't trip
	// the API quota. nil means no client-side pacing.
	limiter *rate.Limiter
}

// New builds a Transport. creds must be an AppKey credential (auth.SOARAppKey);
// auth resolves lazily on the first request. A nil httpClient uses a sensible
// default.
func New(s Settings, creds auth.Credentials, httpClient *http.Client) *Transport {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout:   5 * time.Minute,
			Transport: auth.RoundTripper(creds, auth.HTTPTransport(s.ForceIPv4)),
		}
	}
	// No client-side pacing by default (opt in via SetLimiter); the bounded
	// Retry-After-honoring retry recovers from a 429 without slowing bulk reads.
	return &Transport{
		settings: s,
		base:     strings.TrimRight(s.BaseURL, "/"),
		http:     httpClient,
	}
}

// SetLimiter sets a request-pacing limiter (nil disables pacing). Off by default.
func (t *Transport) SetLimiter(l *rate.Limiter) { t.limiter = l }

// retryPolicy is the shared default policy with the package knobs overlaid (so a
// test that zeros baseBackoff gets instant retries). Budget bounds the TOTAL
// backoff across the loop, not just a single wait.
func retryPolicy() httpretry.Policy {
	p := httpretry.DefaultPolicy()
	p.MaxAttempts = maxRetries + 1
	p.Base = baseBackoff
	return p
}

// Settings returns a copy of the instance settings.
func (t *Transport) SettingsCopy() Settings { return t.settings }

// instancePath is the tenant resource prefix at the given API version.
func (t *Transport) instancePath(version string) string {
	return fmt.Sprintf("/%s/projects/%s/locations/%s/instances/%s",
		version, t.settings.ProjectNumber, t.settings.Region, t.settings.CustomerID)
}

type spec struct {
	query      url.Values
	updateMask []string
	version    string
}

// Option customizes a single request.
type Option func(*spec)

// Query adds URL query parameters.
func Query(q url.Values) Option { return func(s *spec) { s.query = q } }

// UpdateMask sets the updateMask query parameter (v1alpha PATCH sparse updates).
func UpdateMask(fields ...string) Option { return func(s *spec) { s.updateMask = fields } }

// Version overrides the API version for this request. The SOAR host serves
// v1alpha ONLY (v1/v1beta 404), so this is rarely needed here — it exists for
// symmetry with the transport's request options. The v1 > v1beta > v1alpha
// version ladder is a chronicle-host concern, not a SOAR-host one.
func Version(v string) Option { return func(s *spec) { s.version = v } }

// V1Alpha executes a SOAR v1alpha-style request (default version v1alpha; override
// with Version()). resource is appended to the instance path (leading slash
// optional). format=camel and the x-goog-api-version header are always set;
// updateMask is added when provided. body (if non-nil) is JSON marshaled; out (if
// non-nil) is JSON decoded.
func (t *Transport) V1Alpha(ctx context.Context, method, resource string, body, out any, opts ...Option) error {
	sp := apply(opts)
	version := sp.version
	if version == "" {
		version = APIVersion
	}
	q := cloneOrNew(sp.query)
	q.Set("format", "camel")
	if len(sp.updateMask) > 0 {
		q.Set("updateMask", strings.Join(sp.updateMask, ","))
	}
	base := t.base + t.instancePath(version) + "/" + strings.TrimLeft(resource, "/")
	full := base + "?" + q.Encode()
	if method == http.MethodGet && len(full) > maxGetURLLen {
		// A long filter expression can push the URL past intermediary limits.
		// The server accepts the equivalent the web UI sends in that case:
		// POST with X-HTTP-Method-Override: GET and the query parameters as a
		// form body (format stays on the URL). Semantically still a read, but
		// sent as POST it is conservatively excluded from 5xx retries.
		form := q
		q = url.Values{"format": {"camel"}}
		form.Del("format")
		return t.do(ctx, http.MethodPost, base+"?"+q.Encode(), []byte(form.Encode()), out, map[string]string{
			"x-goog-api-version":     version,
			"X-HTTP-Method-Override": "GET",
			"Content-Type":           "application/x-www-form-urlencoded",
		})
	}
	return t.do(ctx, method, full, body, out, map[string]string{"x-goog-api-version": version})
}

// maxGetURLLen is the URL length beyond which a GET is downgraded to the
// method-override POST form. 2000 stays under the common 2 KB intermediary
// caps with headroom for the host and instance path.
const maxGetURLLen = 2000

// External executes a legacy Siemplify external-API request. path is appended to
// /api/external/v1 (leading slash optional).
func (t *Transport) External(ctx context.Context, method, path string, body, out any, opts ...Option) error {
	sp := apply(opts)
	full := t.base + ExternalPrefix + "/" + strings.TrimLeft(path, "/")
	if len(sp.query) > 0 {
		full += "?" + sp.query.Encode()
	}
	return t.do(ctx, method, full, body, out, nil)
}

// ExternalBytes is like External but returns the raw response bytes without JSON
// decoding — for endpoints that return binary content (reports, exports).
func (t *Transport) ExternalBytes(ctx context.Context, method, path string, body any) ([]byte, error) {
	full := t.base + ExternalPrefix + "/" + strings.TrimLeft(path, "/")
	var raw []byte
	if err := t.do(ctx, method, full, body, &raw, nil); err != nil {
		return nil, err
	}
	return raw, nil
}

// retryStatuses5xx are server errors safe to retry ONLY for idempotent methods —
// a 5xx on a mutating POST may have already taken effect server-side (the SOAR
// external API in particular returns a post-creation 500 on CreateManualCase
// while still creating the case), so blindly retrying it duplicates the side
// effect. 429 (rate-limited → rejected before processing) is safe for any method.
var retryStatuses5xx = map[int]bool{500: true, 502: true, 503: true, 504: true}

const maxRetries = 4

// baseBackoff is the first retry delay; subsequent attempts back off
// exponentially (×2 each). A package var so tests can zero it.
var baseBackoff = 300 * time.Millisecond

// idempotentMethod reports whether retrying method is side-effect-safe. Only
// these may be retried on a 5xx or a transport error; POST/PATCH are not.
func idempotentMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

// retryable decides whether a response/error for method warrants another attempt.
// A transport error (no status) or a 5xx is retried only for idempotent methods;
// 429 is retried for any method (the request was rejected, not processed).
func retryable(method string, status int, transportErr bool) bool {
	if transportErr {
		return idempotentMethod(method)
	}
	if status == 429 {
		return true
	}
	if retryStatuses5xx[status] {
		return idempotentMethod(method)
	}
	return false
}

func (t *Transport) do(ctx context.Context, method, full string, body, out any, extraHeaders map[string]string) error {
	var bodyBytes []byte
	switch b := body.(type) {
	case nil:
	case []byte:
		// Pre-encoded payload (e.g. the method-override form body) — sent
		// verbatim; the caller supplies the Content-Type via extraHeaders.
		bodyBytes = b
	default:
		j, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("soar: marshal request body: %w", err)
		}
		bodyBytes = j
	}

	policy := retryPolicy()
	var lastErr error
	var wait, spent time.Duration
	// nextWait computes the pre-retry delay and reports whether to retry: false once
	// attempts are exhausted OR the accumulated backoff would exceed the total
	// Budget, so an honored 429 hint can't hang a command for minutes.
	nextWait := func(attempt int, hint time.Duration) (time.Duration, bool) {
		if attempt >= policy.MaxAttempts-1 {
			return 0, false
		}
		w := policy.Backoff(attempt+1, hint, httpretry.Jitter())
		if policy.Budget > 0 && spent+w > policy.Budget {
			return 0, false
		}
		spent += w
		return w, true
	}
	for attempt := range policy.MaxAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		// Optional client-side pacing (off by default). A ctx-driven Wait failure
		// must surface as ctx.Err() so the CLI's timeout/quota hints apply.
		if t.limiter != nil {
			if err := t.limiter.Wait(ctx); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return err
			}
		}

		var reader io.Reader
		if bodyBytes != nil {
			reader = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, full, reader)
		if err != nil {
			return fmt.Errorf("soar: build request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}

		resp, err := t.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("soar: %s request failed: %w", method, err)
			if retryable(method, 0, true) {
				if w, ok := nextWait(attempt, 0); ok {
					wait = w
					continue
				}
			}
			return lastErr
		}
		data, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if readErr != nil {
				return fmt.Errorf("soar: read response: %w", readErr)
			}
			if out != nil && len(data) > 0 {
				if bp, ok := out.(*[]byte); ok {
					*bp = data
				} else if err := json.Unmarshal(data, out); err != nil {
					return fmt.Errorf("soar: decode response: %w", err)
				}
			}
			return nil
		}

		apiErr := &Error{Method: method, URL: full, Status: resp.StatusCode, Body: string(data), RequestID: requestIDFromHeader(resp.Header)}
		if retryable(method, resp.StatusCode, false) {
			// Honor the server's Retry-After / RetryInfo ONLY for a 429 (quota); a
			// 5xx uses the short backoff so a transient error fails fast.
			var hint time.Duration
			if resp.StatusCode == http.StatusTooManyRequests {
				hint = httpretry.ParseHint(resp.Header, data)
			}
			if w, ok := nextWait(attempt, hint); ok {
				wait = w
				lastErr = apiErr
				continue
			}
		}
		return apiErr
	}
	return lastErr
}

func apply(opts []Option) *spec {
	s := &spec{}
	for _, o := range opts {
		o(s)
	}
	return s
}

func cloneOrNew(q url.Values) url.Values {
	out := url.Values{}
	for k, vs := range q {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// PaginateV1Alpha drives Google-style {items,nextPageToken} pagination: fetch is
// called with the current page token ("" first) and returns the nextPageToken.
//
// maxPages is a safety backstop, not a "first N pages" feature: callers pass a
// generous cap meaning "list everything". If the cap is hit while a non-empty
// token remains, the listing is truncated, so an error is returned rather than
// silently presenting a partial result as complete.
func PaginateV1Alpha(maxPages int, fetch func(pageToken string) (next string, err error)) error {
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
	return fmt.Errorf("transport: pagination stopped at the %d-page cap with more results remaining; narrow the query", maxPages)
}
