package auth

import (
	"fmt"
	"net/http"
)

// bearerCreds signs requests with a verbatim bearer token in the Authorization
// header. Unlike oauthCreds it mints and refreshes nothing — the token is supplied
// as-is by the caller. It is the third credential type alongside OAuth/ADC and the
// API key/AppKey: a pre-obtained bearer the caller already holds.
//
// The motivating case is the SOAR host: the web console authenticates SOAR calls
// with a session JWT carried as `Authorization: Bearer <token>`, and some SOAR-host
// surfaces accept only that bearer identity (not the AppKey header). Supplying such
// a token here reproduces the console's auth for a CLI request.
type bearerCreds struct {
	token string
}

// BearerToken returns credentials that set "Authorization: Bearer <token>". The
// token is used verbatim and never refreshed, so supply a currently-valid one;
// short-lived session tokens should be passed per-invocation rather than persisted.
func BearerToken(token string) Credentials {
	return &bearerCreds{token: token}
}

func (c *bearerCreds) Apply(req *http.Request) error {
	if c.token == "" {
		return fmt.Errorf("auth: bearer token is empty")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	return nil
}
