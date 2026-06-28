package auth

import (
	"fmt"
	"net/http"
	"os"
)

// apiKeyCreds signs requests with a static API key / SOAR AppKey carried in a
// header. No OAuth, no gcloud, no ADC — for SecOps features that don't need it.
type apiKeyCreds struct {
	header string
	key    string
}

// APIKey returns credentials that set `header: key`. An empty header defaults
// to "x-goog-api-key" (the Google API key header).
func APIKey(header, key string) Credentials {
	if header == "" {
		header = "x-goog-api-key"
	}
	return &apiKeyCreds{header: header, key: key}
}

// SOARAppKey returns credentials for the SecOps SOAR REST API, which expects
// the key in the "AppKey" header.
func SOARAppKey(key string) Credentials {
	return &apiKeyCreds{header: "AppKey", key: key}
}

func (c *apiKeyCreds) Apply(req *http.Request) error {
	if c.key == "" {
		return fmt.Errorf("auth: API key for header %q is empty", c.header)
	}
	req.Header.Set(c.header, c.key)
	return nil
}

// SOARBearerToken returns credentials that set a Bearer Authorization header
// for the SOAR host — the JWT minted by GenerateSoarAuthJwt on the chronicle
// host. Unlike SOARAppKey (which sets the "AppKey" header), this uses standard
// Bearer auth for the modern v1alpha SOAR paths.
func SOARBearerToken(token string) Credentials {
	return &bearerCreds{token: token}
}

type bearerCreds struct{ token string }

func (c *bearerCreds) Apply(req *http.Request) error {
	if c.token == "" {
		return fmt.Errorf("auth: SOAR Bearer token is empty")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	return nil
}

// FromEnv returns the first non-empty value among the named environment
// variables (e.g. FromEnv("SECOPS_API_KEY", "SECOPS_SOAR_APP_KEY")).
func FromEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}
