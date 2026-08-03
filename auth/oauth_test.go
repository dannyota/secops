package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOAuthApplyHonorsTokenContextDuringRefresh(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	credentialsPath := filepath.Join(t.TempDir(), "adc.json")
	credentialsJSON := fmt.Sprintf(`{
  "type": "authorized_user",
  "client_id": "test-client",
  "client_secret": "test-secret",
  "refresh_token": "test-refresh",
  "token_uri": %q
}`, server.URL)
	if err := os.WriteFile(credentialsPath, []byte(credentialsJSON), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialsPath)
	t.Setenv("SECOPS_ACCESS_TOKEN", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	result := make(chan error, 1)
	go func() { result <- OAuth(WithTokenContext(ctx)).Apply(req) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("token refresh request never started")
	}
	cancel()
	select {
	case err = <-result:
	case <-time.After(time.Second):
		t.Fatal("OAuth.Apply did not stop when its token context was canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("OAuth.Apply error = %v, want context canceled", err)
	}
}

func TestOAuthDefaultLifetimeOutlivesRequestContext(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		// Shorter than oauth2's expiryDelta, so the next Apply must refresh and
		// proves the token source did not capture the first request's context.
		_, _ = fmt.Fprintf(w, `{"access_token":"token-%d","token_type":"Bearer","expires_in":1}`, requests)
	}))
	t.Cleanup(server.Close)

	credentialsPath := filepath.Join(t.TempDir(), "adc.json")
	credentialsJSON := fmt.Sprintf(`{
  "type": "authorized_user",
  "client_id": "test-client",
  "client_secret": "test-secret",
  "refresh_token": "test-refresh",
  "token_uri": %q
}`, server.URL)
	if err := os.WriteFile(credentialsPath, []byte(credentialsJSON), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialsPath)
	t.Setenv("SECOPS_ACCESS_TOKEN", "")

	creds := OAuth()
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	first, err := http.NewRequestWithContext(firstCtx, http.MethodGet, "https://example.com/first", nil)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	if err := creds.Apply(first); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	cancelFirst()

	second, err := http.NewRequest(http.MethodGet, "https://example.com/second", nil)
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	if err := creds.Apply(second); err != nil {
		t.Fatalf("second Apply after first request cancellation: %v", err)
	}
	if requests < 2 {
		t.Fatalf("token endpoint received %d request(s), want a refresh after cancellation", requests)
	}
}
