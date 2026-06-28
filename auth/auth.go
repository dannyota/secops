// Package auth provides credential providers for Google SecOps.
//
// SecOps has two independent authentication surfaces, and this package keeps
// them deliberately split:
//
//   - OAuth (ADC / service-account / static bearer) — for the Chronicle SIEM
//     API (chronicle.googleapis.com), which requires a Google OAuth2 token.
//   - API key / SOAR AppKey — for SecOps features that authenticate with a
//     long-lived key header and do NOT need ADC (SOAR REST, webhooks, …).
//
// Both implement Credentials, so each product client takes only the credential
// type it needs. Credentials resolve lazily (on the first signed request), so
// constructing a client never blocks on ADC, the network, or gcloud — `--help`,
// config parsing, offline tests, and API-key-only flows stay credential-free.
package auth

import (
	"fmt"
	"net/http"
)

// Credentials applies authentication to an outbound request.
//
// Implementations MUST resolve their underlying secret lazily (on first Apply)
// rather than at construction, and MUST be safe for concurrent use.
type Credentials interface {
	// Apply sets the appropriate auth header on req.
	Apply(req *http.Request) error
}

// RoundTripper wraps base so every request carries creds. A nil base uses
// http.DefaultTransport.
func RoundTripper(creds Credentials, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &authTransport{creds: creds, base: base}
}

type authTransport struct {
	creds Credentials
	base  http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// The RoundTripper contract forbids mutating the input request, so clone.
	r2 := req.Clone(req.Context())
	if err := t.creds.Apply(r2); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(r2)
}

// MintToken resolves creds and extracts the Bearer token value. Returns an error
// if creds don't produce a Bearer-style Authorization header (e.g. an API key).
func MintToken(creds Credentials) (string, error) {
	req, _ := http.NewRequest(http.MethodGet, "https://localhost/token-probe", nil)
	if err := creds.Apply(req); err != nil {
		return "", err
	}
	h := req.Header.Get("Authorization")
	if h == "" {
		return "", fmt.Errorf("auth: credentials did not set Authorization header")
	}
	const prefix = "Bearer "
	if len(h) > len(prefix) && h[:len(prefix)] == prefix {
		return h[len(prefix):], nil
	}
	return "", fmt.Errorf("auth: credentials set non-Bearer Authorization")
}
