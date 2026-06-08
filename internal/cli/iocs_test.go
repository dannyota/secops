package cli

import (
	"testing"

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
