package auth

import (
	"net/http"
	"testing"
)

func mustReq(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestAPIKeyDefaultsHeader(t *testing.T) {
	req := mustReq(t)
	if err := APIKey("", "k123").Apply(req); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("x-goog-api-key"); got != "k123" {
		t.Errorf("x-goog-api-key = %q", got)
	}
}

func TestSOARAppKeyHeader(t *testing.T) {
	req := mustReq(t)
	if err := SOARAppKey("app-k").Apply(req); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("AppKey"); got != "app-k" {
		t.Errorf("AppKey = %q", got)
	}
}

func TestSOARBearerTokenHeader(t *testing.T) {
	req := mustReq(t)
	if err := SOARBearerToken("jwt").Apply(req); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer jwt" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestEmptyCredentialsError(t *testing.T) {
	if err := APIKey("", "").Apply(mustReq(t)); err == nil {
		t.Error("empty API key must error")
	}
	if err := SOARBearerToken("").Apply(mustReq(t)); err == nil {
		t.Error("empty bearer token must error")
	}
}

type recordingRT struct{ got *http.Request }

func (r *recordingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.got = req
	return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
}

func TestRoundTripperClonesRequest(t *testing.T) {
	base := &recordingRT{}
	rt := RoundTripper(SOARAppKey("k"), base)
	orig := mustReq(t)
	resp, err := rt.RoundTrip(orig)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if base.got.Header.Get("AppKey") != "k" {
		t.Error("outbound request missing the auth header")
	}
	if orig.Header.Get("AppKey") != "" {
		t.Error("RoundTrip mutated the caller's request")
	}
}
