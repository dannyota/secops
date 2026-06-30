package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

func TestIoCValueType(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		override string
		want     chronicle.IoCValueType
		wantErr  bool
	}{
		{"ipv4", "8.8.8.8", "", chronicle.IoCValueIP, false},
		{"ipv6", "2001:4860:4860::8888", "", chronicle.IoCValueIP, false},
		{"md5", "44d88612fea8a8f36de82e1278abb02f", "", chronicle.IoCValueMD5, false},
		{"sha1", "da39a3ee5e6b4b0d3255bfef95601890afd80709", "", chronicle.IoCValueSHA1, false},
		{"sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "", chronicle.IoCValueSHA256, false},
		{"domain", "example.com", "", chronicle.IoCValueDomain, false},
		{"override wins over shape", "example.com", "sha256", chronicle.IoCValueSHA256, false},
		{"override domain", "anything", "domain", chronicle.IoCValueDomain, false},
		{"bad override", "example.com", "bogus", "", true},
		{"uninferable", "not-a-known-shape", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := iocValueType(c.value, c.override)
			if c.wantErr {
				if err == nil {
					t.Fatalf("iocValueType(%q,%q) = %q, want error", c.value, c.override, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("iocValueType(%q,%q) unexpected error: %v", c.value, c.override, err)
			}
			if got != c.want {
				t.Errorf("iocValueType(%q,%q) = %q, want %q", c.value, c.override, got, c.want)
			}
		})
	}
}

func TestRelatedThreatCollectionTypes(t *testing.T) {
	cases := []struct {
		in      string
		want    []chronicle.RelatedThreatCollectionType
		wantErr bool
	}{
		{"", []chronicle.RelatedThreatCollectionType{chronicle.RelatedThreatCollectionCampaign, chronicle.RelatedThreatCollectionReport}, false},
		{"all", []chronicle.RelatedThreatCollectionType{chronicle.RelatedThreatCollectionCampaign, chronicle.RelatedThreatCollectionReport}, false},
		{"campaign", []chronicle.RelatedThreatCollectionType{chronicle.RelatedThreatCollectionCampaign}, false},
		{"reports", []chronicle.RelatedThreatCollectionType{chronicle.RelatedThreatCollectionReport}, false},
		{"bogus", nil, true},
	}
	for _, c := range cases {
		got, err := relatedThreatCollectionTypes(c.in)
		if c.wantErr {
			if err == nil {
				t.Fatalf("relatedThreatCollectionTypes(%q) = %v, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("relatedThreatCollectionTypes(%q): %v", c.in, err)
		}
		if len(got) != len(c.want) {
			t.Fatalf("relatedThreatCollectionTypes(%q) len = %d, want %d", c.in, len(got), len(c.want))
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("relatedThreatCollectionTypes(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

// TestReadIndicatorList: file input skips blanks and # comments, trims whitespace.
func TestReadIndicatorList(t *testing.T) {
	p := filepath.Join(t.TempDir(), "iocs.txt")
	if err := os.WriteFile(p, []byte("a.com\n\n# a comment\n  1.2.3.4  \nevil.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readIndicatorList(&cobra.Command{}, p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.com", "1.2.3.4", "evil.example"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("readIndicatorList = %v, want %v", got, want)
	}
}

// TestReadIndicatorListStdin: "-" reads from the command's stdin.
func TestReadIndicatorListStdin(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("x.com\n# c\nhash123\n"))
	got, err := readIndicatorList(cmd, "-")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"x.com", "hash123"}) {
		t.Errorf("stdin read = %v", got)
	}
}

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
