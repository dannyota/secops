package chronicle

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"danny.vn/secops/auth"
)

type rejectingCredentials struct {
	called bool
}

var _ auth.Credentials = (*rejectingCredentials)(nil)

func (c *rejectingCredentials) Apply(*http.Request) error {
	c.called = true
	return errors.New("credentials must not be resolved in this test")
}

func TestGetInstanceUsesV1Endpoint(t *testing.T) {
	const wantName = "projects/test-project/locations/us/instances/test-customer"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/v1/"+wantName {
			t.Errorf("path = %q, want %q", r.URL.Path, "/v1/"+wantName)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want none", r.URL.RawQuery)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want none from overridden test client", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		} else if len(body) != 0 {
			t.Errorf("body = %q, want empty", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"`+wantName+`"}`)
	}))
	defer server.Close()

	creds := &rejectingCredentials{}
	client, err := NewClient(
		Settings{
			ProjectID:  "test-project",
			Region:     "us",
			CustomerID: "test-customer",
			BaseURL:    server.URL + "/v1alpha",
		},
		creds,
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	instance, err := client.GetInstance(context.Background())
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if creds.called {
		t.Fatal("GetInstance resolved credentials despite the HTTP client override")
	}
	if instance.Name != wantName {
		t.Errorf("instance name = %q, want %q", instance.Name, wantName)
	}
}
