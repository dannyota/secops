package chronicle

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

// DefaultAPIVersion is the Chronicle API version this SDK targets.
const DefaultAPIVersion = "v1alpha"

// Settings identifies a single Chronicle (Google SecOps) instance.
//
// Both ProjectID and ProjectNumber are kept because Chronicle endpoints are
// inconsistent about which form they accept in a resource name (see
// resource.go) — this SDK encodes the required form explicitly per endpoint.
type Settings struct {
	ProjectID     string // GCP project ID (string form), e.g. "my-project"
	ProjectNumber string // GCP project number (numeric, as a string)
	Region        string // e.g. "us", "europe", "asia-southeast1"
	CustomerID    string // Chronicle instance UUID
	BaseURL       string // optional; defaults from Region + DefaultAPIVersion
	ForceIPv4     bool   // pin the dialer to IPv4 (also via SECOPS_FORCE_IPV4)
}

// Client is a Chronicle SIEM API client. It is safe for concurrent use.
type Client struct {
	settings Settings
	baseURL  string
	http     *http.Client
	// limiter paces outgoing requests so a bursty multi-call operation can't fire
	// everything at once and trip the API quota. nil means no client-side pacing.
	limiter *rate.Limiter
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient overrides the underlying *http.Client (e.g. for tests).
// The provided client is responsible for authentication if set this way.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithLimiter overrides the client-side request-pacing limiter. Pass nil to
// disable pacing (e.g. in tests, or when an outer limiter already governs rate).
func WithLimiter(l *rate.Limiter) Option { return func(c *Client) { c.limiter = l } }

// NewClient builds a Chronicle client for the SIEM API.
//
// creds should be OAuth credentials (auth.OAuth) — the SIEM API requires an
// OAuth2 token. Auth is resolved lazily on the first request, so constructing a
// client never touches the network or gcloud.
func NewClient(s Settings, creds auth.Credentials, opts ...Option) (*Client, error) {
	if s.Region == "" {
		return nil, fmt.Errorf("chronicle: Settings.Region is required")
	}
	if s.CustomerID == "" {
		return nil, fmt.Errorf("chronicle: Settings.CustomerID is required")
	}
	base := s.BaseURL
	if base == "" {
		base = fmt.Sprintf("https://%s-chronicle.googleapis.com/%s", s.Region, DefaultAPIVersion)
	}
	// No client-side pacing by default — proactive throttling can't reliably honor
	// a per-minute quota and would slow legitimate bulk reads; the bounded
	// Retry-After-honoring retry recovers from a 429 instead. Opt in via WithLimiter.
	c := &Client{settings: s, baseURL: strings.TrimRight(base, "/")}

	c.http = &http.Client{
		Timeout:   5 * time.Minute,
		Transport: auth.RoundTripper(creds, auth.HTTPTransport(s.ForceIPv4)),
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// Settings returns a copy of the instance settings.
func (c *Client) Settings() Settings { return c.settings }

// --- request plumbing -------------------------------------------------------

type requestSpec struct {
	query   url.Values
	version string
}

type requestOption func(*requestSpec)

// withQuery attaches URL query parameters to a request.
func withQuery(q url.Values) requestOption {
	return func(s *requestSpec) { s.query = q }
}

// withVersion pins this request to a specific API version (e.g. "v1", "v1beta"),
// overriding the client's default (DefaultAPIVersion). Project policy is to prefer
// the newest version that works per surface: v1 > v1beta > v1alpha. Each surface
// passes its validated version; DefaultAPIVersion is the fallback for the rest.
//
// It rewrites the trailing "/<DefaultAPIVersion>" segment of the base URL, so it is
// a no-op when a caller supplies a custom Settings.BaseURL that doesn't end in
// "/<DefaultAPIVersion>" (the request then uses that base's version). All normal
// callers use the default base; the version probes set a per-version BaseURL on
// purpose instead.
func withVersion(v string) requestOption {
	return func(s *requestSpec) { s.version = v }
}

// do executes method against path (relative to baseURL; leading slash optional),
// JSON-marshaling body (if non-nil) and JSON-decoding the response into out (if
// non-nil). Non-2xx responses become *APIError. Transient failures (429/5xx and
// network errors) are retried with capped exponential backoff.
func (c *Client) do(ctx context.Context, method, path string, body, out any, opts ...requestOption) error {
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
	return c.doRequest(ctx, method, full, body, out)
}

// doRequest issues method against the already-built absolute URL full, marshaling
// body and decoding into out, with the shared capped-backoff retry loop. Retries
// are gated by retryable: a 5xx or transport error is retried only for idempotent
// methods, 429 for any. do() and caseDo() (case.go) both delegate here.
func (c *Client) doRequest(ctx context.Context, method, full string, body, out any) error {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("chronicle: marshal request body: %w", err)
		}
		bodyBytes = b
	}

	policy := retryPolicy()
	var lastErr error
	var wait, spent time.Duration
	// nextWait computes the pre-retry delay and reports whether to retry at all:
	// false once attempts are exhausted OR the accumulated backoff would exceed the
	// total Budget — so an honored 429 hint can't hang a command for minutes.
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
		// Optional client-side pacing (opt-in via WithLimiter). A ctx-driven Wait
		// failure must surface as ctx.Err() so the CLI's timeout/quota hints apply.
		if c.limiter != nil {
			if err := c.limiter.Wait(ctx); err != nil {
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
			return fmt.Errorf("chronicle: build request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("chronicle: %s request failed: %w", method, err)
			if retryable(method, 0, true) {
				if w, ok := nextWait(attempt, 0); ok {
					wait = w
					continue // transport error, idempotent method: retry
				}
			}
			return lastErr
		}

		data, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if readErr != nil {
				return fmt.Errorf("chronicle: read response: %w", readErr)
			}
			if out != nil && len(data) > 0 {
				if err := json.Unmarshal(data, out); err != nil {
					return fmt.Errorf("chronicle: decode response: %w", err)
				}
			}
			return nil
		}

		apiErr := &APIError{Method: method, URL: full, Status: resp.StatusCode, Body: string(data), RequestID: requestIDFromHeader(resp.Header)}
		if retryable(method, resp.StatusCode, false) {
			// Honor the server's Retry-After / RetryInfo ONLY for a 429 (quota — it's
			// authoritative there); a 5xx uses the short backoff so a transient error
			// doesn't block for a proxy-set Retry-After.
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

// get/post/patch are thin verb wrappers around do.
func (c *Client) get(ctx context.Context, path string, out any, opts ...requestOption) error {
	return c.do(ctx, http.MethodGet, path, nil, out, opts...)
}

func (c *Client) post(ctx context.Context, path string, body, out any, opts ...requestOption) error {
	return c.do(ctx, http.MethodPost, path, body, out, opts...)
}

func (c *Client) patch(ctx context.Context, path string, body, out any, opts ...requestOption) error {
	return c.do(ctx, http.MethodPatch, path, body, out, opts...)
}
