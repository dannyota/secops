// Package transport is the shared, durable HTTP plumbing for the Google SecOps
// SOAR API.
//
// Both the modern v1alpha client (danny.vn/secops/soar) and the quarantined
// legacy client (danny.vn/secops/soar/legacy) build on it, so the legacy
// subpackage can be deleted wholesale without touching the modern code. SOAR
// authenticates with the AppKey header — never ADC.
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

	"danny.vn/secops/auth"
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

// Error is a non-2xx response from the SOAR API.
type Error struct {
	Method string
	URL    string
	Status int
	Body   string
}

func (e *Error) Error() string {
	const max = 2000
	body := e.Body
	if len(body) > max {
		body = body[:max] + "…(truncated)"
	}
	return fmt.Sprintf("soar: %s %s -> HTTP %d: %s", e.Method, e.URL, e.Status, body)
}

// Transport executes authenticated SOAR requests. Safe for concurrent use.
type Transport struct {
	settings Settings
	base     string
	http     *http.Client
}

// New builds a Transport. creds must be an AppKey credential (auth.SOARAppKey);
// auth resolves lazily on the first request. A nil httpClient uses a sensible
// default.
func New(s Settings, creds auth.Credentials, httpClient *http.Client) *Transport {
	if httpClient == nil {
		ht := &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		}
		if dc := auth.IPv4DialContext(s.ForceIPv4); dc != nil {
			ht.DialContext = dc
		}
		httpClient = &http.Client{
			Timeout:   5 * time.Minute,
			Transport: auth.RoundTripper(creds, ht),
		}
	}
	return &Transport{
		settings: s,
		base:     strings.TrimRight(s.BaseURL, "/"),
		http:     httpClient,
	}
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

// Version overrides the API version for this request (e.g. "v1", "v1beta").
// Default is APIVersion (v1alpha). Project policy is to prefer v1 > v1beta >
// v1alpha per surface — pass the version validated for that surface.
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
	full := t.base + t.instancePath(version) + "/" + strings.TrimLeft(resource, "/") + "?" + q.Encode()
	return t.do(ctx, method, full, body, out, map[string]string{"x-goog-api-version": version})
}

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
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("soar: marshal request body: %w", err)
		}
		bodyBytes = b
	}

	var lastErr error
	for attempt := range maxRetries + 1 {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * baseBackoff
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
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
			lastErr = fmt.Errorf("soar: %s %s: %w", method, full, err)
			if attempt < maxRetries && retryable(method, 0, true) {
				continue
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
				if err := json.Unmarshal(data, out); err != nil {
					return fmt.Errorf("soar: decode response: %w", err)
				}
			}
			return nil
		}

		apiErr := &Error{Method: method, URL: full, Status: resp.StatusCode, Body: string(data)}
		if attempt < maxRetries && retryable(method, resp.StatusCode, false) {
			lastErr = apiErr
			continue
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
	return nil
}
