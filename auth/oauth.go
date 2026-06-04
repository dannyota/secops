package auth

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// CloudPlatformScope is the OAuth2 scope the Chronicle SIEM API expects.
const CloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// oauthCreds signs requests with a Google OAuth2 bearer token.
//
// Resolution order (evaluated once, lazily, on first use):
//
//  1. SECOPS_ACCESS_TOKEN  — a static bearer token (CI / break-glass).
//  2. Application Default Credentials — honors GOOGLE_APPLICATION_CREDENTIALS
//     and `gcloud auth application-default login`, via FindDefaultCredentials.
//
// DEVIATION (vs the official Python wrapper): auth is never resolved at
// construction; nothing here shells out to `gcloud` until a request is signed.
type oauthCreds struct {
	scopes []string

	once sync.Once
	src  oauth2.TokenSource
	err  error
}

// OAuth returns OAuth2/ADC credentials for the given scopes (defaults to
// CloudPlatformScope). Resolution is deferred to the first request.
func OAuth(scopes ...string) Credentials {
	if len(scopes) == 0 {
		scopes = []string{CloudPlatformScope}
	}
	return &oauthCreds{scopes: scopes}
}

func (c *oauthCreds) resolve() (oauth2.TokenSource, error) {
	c.once.Do(func() {
		if tok := os.Getenv("SECOPS_ACCESS_TOKEN"); tok != "" {
			c.src = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: tok})
			return
		}
		creds, err := google.FindDefaultCredentials(context.Background(), c.scopes...)
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
