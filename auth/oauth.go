package auth

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// CloudPlatformScope is the OAuth2 scope the Chronicle SIEM API expects.
const CloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// oauthCreds signs requests with a Google OAuth2 bearer token.
//
// Resolution order (evaluated once, lazily, on first use):
//
//  1. SECOPS_ACCESS_TOKEN  — a static bearer token (CI / break-glass); no network.
//  2. Application Default Credentials — honors GOOGLE_APPLICATION_CREDENTIALS
//     and `gcloud auth application-default login`, via FindDefaultCredentials.
//
// DEVIATION (vs the official Python wrapper): auth is never resolved at
// construction; nothing here shells out to `gcloud` until a request is signed.
type oauthCreds struct {
	scopes    []string
	forceIPv4 bool
	// tokenCtx owns ADC discovery and token refreshes for this credential. The
	// default is background so normal long-lived clients remain reusable; bounded
	// workflows such as doctor may provide their own lifetime context.
	tokenCtx context.Context

	once sync.Once
	src  oauth2.TokenSource
	err  error
}

// OAuthOption configures the OAuth credential provider.
type OAuthOption func(*oauthCreds)

// WithScopes overrides the OAuth2 scopes (default: CloudPlatformScope).
func WithScopes(scopes ...string) OAuthOption {
	return func(c *oauthCreds) {
		if len(scopes) > 0 {
			c.scopes = scopes
		}
	}
}

// WithForceIPv4 pins the in-process token-minting/refresh HTTP calls to IPv4
// (also honored via SECOPS_FORCE_IPV4). Needed on corporate VPNs whose IPv6
// routing to oauth2.googleapis.com is broken — the same workaround the SIEM/SOAR
// API transports use. No effect on the static-token path (it makes no network
// call).
func WithForceIPv4(force bool) OAuthOption {
	return func(c *oauthCreds) { c.forceIPv4 = force }
}

// WithTokenContext bounds ADC discovery and token mint/refresh operations to
// ctx. The context must remain valid for the entire lifetime of the returned
// credentials. Most callers should omit this option; it exists for bounded
// workflows such as doctor that discard the credentials when ctx ends.
func WithTokenContext(ctx context.Context) OAuthOption {
	return func(c *oauthCreds) {
		if ctx != nil {
			c.tokenCtx = ctx
		}
	}
}

// OAuth returns OAuth2/ADC credentials. The access token is minted in-process by
// the Google auth library (FindDefaultCredentials) and auto-refreshed — no
// `gcloud` shell-out and no token persisted to disk. Resolution is deferred to
// the first request.
func OAuth(opts ...OAuthOption) Credentials {
	c := &oauthCreds{
		scopes:   []string{CloudPlatformScope},
		tokenCtx: context.Background(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *oauthCreds) resolve() (oauth2.TokenSource, error) {
	c.once.Do(func() {
		if tok := os.Getenv("SECOPS_ACCESS_TOKEN"); tok != "" {
			c.src = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: tok})
			return
		}
		// Mint/refresh tokens in-process. When IPv4 is forced, hand the oauth2
		// library an IPv4-pinned HTTP client via the context so the token endpoint
		// calls honor it too (not just the API transports). tokenCtx is normally
		// background for reusable clients; bounded workflows can opt into a shorter
		// lifetime with WithTokenContext.
		ctx := c.tokenCtx
		if ctx == nil {
			ctx = context.Background()
		}
		if c.forceIPv4 || ForceIPv4Env() {
			ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{
				Timeout:   60 * time.Second,
				Transport: HTTPTransport(c.forceIPv4),
			})
		}
		creds, err := google.FindDefaultCredentials(ctx, c.scopes...)
		if err != nil {
			c.err = fmt.Errorf("resolve Google credentials: %w "+
				"(set SECOPS_ACCESS_TOKEN, point GOOGLE_APPLICATION_CREDENTIALS at a "+
				"service-account key, or run `gcloud auth application-default login`)", err)
			return
		}
		c.src = creds.TokenSource
	})
	return c.src, c.err
}

func (c *oauthCreds) Apply(req *http.Request) error {
	src, err := c.resolve()
	if err != nil {
		return err
	}
	tok, err := src.Token()
	if err != nil {
		return fmt.Errorf("obtain access token: %w", err)
	}
	tok.SetAuthHeader(req)
	return nil
}
