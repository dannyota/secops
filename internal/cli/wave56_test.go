package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/secops/soar"
)

func TestBuildWatchlistEntity(t *testing.T) {
	e, label, err := buildWatchlistEntity("", "", "host-1", "", "", "ns")
	if err != nil || e.Asset == nil || e.Namespace != "ns" || !strings.Contains(label, "host-1") {
		t.Errorf("hostname entity = %+v, %q, %v", e, label, err)
	}
	if _, _, err := buildWatchlistEntity("", "", "", "", "", ""); err == nil {
		t.Error("no selector must error")
	}
	if _, _, err := buildWatchlistEntity("1.2.3.4", "", "h", "", "", ""); err == nil {
		t.Error("two selectors must error")
	}
	// Pin the exact wire shapes (per the documented entity oneof: ip/mac/email
	// are single-element arrays, hostname/userid scalars) so a shape change is a
	// deliberate edit, not drift — the add itself stays gated until a live smoke.
	for _, tc := range []struct {
		ip, mac, hostname, user, email string
		wantJSON                       string
	}{
		{ip: "10.0.0.5", wantJSON: `{"asset":{"ip":["10.0.0.5"]}}`},
		{mac: "00:11:22:33:44:55", wantJSON: `{"asset":{"mac":["00:11:22:33:44:55"]}}`},
		{hostname: "h-1", wantJSON: `{"asset":{"hostname":"h-1"}}`},
		{user: "u-1", wantJSON: `{"user":{"userid":"u-1"}}`},
		{email: "user@example.com", wantJSON: `{"user":{"email_addresses":["user@example.com"]}}`},
	} {
		e, _, err := buildWatchlistEntity(tc.ip, tc.mac, tc.hostname, tc.user, tc.email, "")
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		got, _ := json.Marshal(e)
		if string(got) != tc.wantJSON {
			t.Errorf("entity wire shape = %s, want %s", got, tc.wantJSON)
		}
	}
}

func TestRefuseAIGenerationIfReadOnly(t *testing.T) {
	t.Setenv("SECOPSCTL_HOME", t.TempDir())
	t.Setenv("SECOPS_READONLY", "")
	if err := refuseAIGenerationIfReadOnly("x"); err != nil {
		t.Errorf("must allow when not read-only: %v", err)
	}
	t.Setenv("SECOPS_READONLY", "1")
	if err := refuseAIGenerationIfReadOnly("x"); err == nil {
		t.Error("must refuse AI generation in read-only mode")
	}
}

func TestCaseSummarySettled(t *testing.T) {
	var s soar.CaseSummary
	if err := json.Unmarshal([]byte(`{"summary":"x","state":"IN_PROGRESS"}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.Settled() {
		t.Error("IN_PROGRESS must not be settled")
	}
	if err := json.Unmarshal([]byte(`{"summary":"x","state":"SUCCESSFUL","reasons":["r"]}`), &s); err != nil {
		t.Fatal(err)
	}
	if !s.Settled() || len(s.Reasons) != 1 {
		t.Errorf("settled decode = %+v", s)
	}
}

func TestHTMLToText(t *testing.T) {
	in := "<p>First line.</p><ul><li>one</li><li>two &amp; three</li></ul>"
	got := htmlToText(in)
	for _, want := range []string{"First line.", "- one", "- two & three"} {
		if !strings.Contains(got, want) {
			t.Errorf("htmlToText missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "<") {
		t.Errorf("tags leaked: %q", got)
	}
}

func TestCaseAlertDecode(t *testing.T) {
	var a soar.CaseAlert
	body := `{"id": 24506, "identifier": "ALERT-X", "alertGroupIdentifier": "GRP-1", "displayName": "Alert"}`
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatal(err)
	}
	if a.ID.String() != "24506" || a.Identifier != "ALERT-X" {
		t.Errorf("decode = %+v", a)
	}
}
