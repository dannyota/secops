package cli

import (
	"errors"
	"net/http"
	"reflect"
	"testing"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/soar"
)

// TestErrorEnvelopeClassifies asserts the structured --json error envelope maps
// each SDK error type to the right canonical code, request id, and retryable flag.
func TestErrorEnvelopeClassifies(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		wantCode  string
		wantRetry bool
		wantRID   string
	}{
		{
			name:     "chronicle 404 GET not retryable",
			err:      &chronicle.APIError{Method: http.MethodGet, Status: 404, RequestID: "abc"},
			wantCode: "NOT_FOUND", wantRetry: false, wantRID: "abc",
		},
		{
			name:     "chronicle 500 GET retryable (idempotent)",
			err:      &chronicle.APIError{Method: http.MethodGet, Status: 500},
			wantCode: "INTERNAL", wantRetry: true,
		},
		{
			name:     "chronicle 500 POST not retryable (mutation)",
			err:      &chronicle.APIError{Method: http.MethodPost, Status: 500},
			wantCode: "INTERNAL", wantRetry: false,
		},
		{
			name:     "soar 429 any method retryable",
			err:      &soar.Error{Method: http.MethodPost, Status: 429, RequestID: "r1"},
			wantCode: "RESOURCE_EXHAUSTED", wantRetry: true, wantRID: "r1",
		},
		{
			name:     "drift sentinel",
			err:      divergence("surface x drifted"),
			wantCode: "DRIFT", wantRetry: false,
		},
		{
			name:     "generic error",
			err:      errors.New("boom"),
			wantCode: "ERROR", wantRetry: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newErrorEnvelope(tc.err)
			if env.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", env.Code, tc.wantCode)
			}
			if env.Retryable != tc.wantRetry {
				t.Errorf("retryable = %v, want %v", env.Retryable, tc.wantRetry)
			}
			if env.RequestID != tc.wantRID {
				t.Errorf("request_id = %q, want %q", env.RequestID, tc.wantRID)
			}
		})
	}
}

// TestEnumFromUsage pins the help-text enum extractor used by `commands --json`.
func TestEnumFromUsage(t *testing.T) {
	cases := []struct {
		usage string
		want  []string
	}{
		{"curated precision (precise|broad)", []string{"precise", "broad"}},
		{"reason: malicious | not-malicious | maintenance", []string{"malicious", "not-malicious", "maintenance"}},
		{"a plain description with no enum", nil},
		{"just one|", nil}, // single token → not an enum
		{"replace a base step with a mold: <step-name|id>=<step.json>", nil}, // placeholder grammar, not an enum
		{"indicator type: md5|sha1|sha256 (default: auto-detect)", []string{"md5", "sha1", "sha256"}},
	}
	for _, tc := range cases {
		if got := enumFromUsage(tc.usage); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("enumFromUsage(%q) = %v, want %v", tc.usage, got, tc.want)
		}
	}
}

// TestCommandsCatalogRichFlags asserts the catalog now carries per-flag type and
// the parsed enum for a representative guarded verb.
func TestCommandsCatalogRichFlags(t *testing.T) {
	byPath := map[string]commandRow{}
	for _, r := range collectCommands(rootCmd, "") {
		byPath[r.Path] = r
	}
	r, ok := byPath["curated set"]
	if !ok {
		t.Fatal("`curated set` missing from catalog")
	}
	var precision *flagInfo
	for i := range r.Flags {
		if r.Flags[i].Name == "precision" {
			precision = &r.Flags[i]
		}
	}
	if precision == nil {
		t.Fatal("curated set has no --precision flag in catalog")
	}
	if precision.Type != "string" {
		t.Errorf("--precision type = %q, want string", precision.Type)
	}
	if !reflect.DeepEqual(precision.Enum, []string{"precise", "broad"}) {
		t.Errorf("--precision enum = %v, want [precise broad]", precision.Enum)
	}
}
