package auth

import (
	"net/http"
	"testing"
)

func TestBearerTokenApply(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := BearerToken("abc.def.ghi").Apply(req); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, want := req.Header.Get("Authorization"), "Bearer abc.def.ghi"; got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}

func TestBearerTokenEmpty(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err := BearerToken("").Apply(req); err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization should be unset on error, got %q", got)
	}
}
