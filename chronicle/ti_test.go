package chronicle

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIocAssociationDecode(t *testing.T) {
	raw := `{
		"name":"projects/000000000000/locations/us/instances/00000000-0000-0000-0000-000000000000/iocAssociations/malware--aabbccdd-1234-5678-9abc-def012345678",
		"type":"MALWARE",
		"threatDisplayName":"Test Malware",
		"description":"A test description.",
		"firstReferenceTime":"2026-07-01T00:00:00Z",
		"roles":["Downloader"],
		"operatingSystems":["Windows"]
	}`
	var a IocAssociation
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatal(err)
	}
	if a.ID != "malware--aabbccdd-1234-5678-9abc-def012345678" {
		t.Errorf("ID = %q, want derived from name", a.ID)
	}
	if a.AssociationType != "MALWARE" {
		t.Errorf("AssociationType = %q", a.AssociationType)
	}
	if a.ThreatDisplayName != "Test Malware" {
		t.Errorf("ThreatDisplayName = %q", a.ThreatDisplayName)
	}
	if len(a.Roles) != 1 || a.Roles[0] != "Downloader" {
		t.Errorf("Roles = %v, want [Downloader]", a.Roles)
	}
	if len(a.Raw) == 0 {
		t.Error("Raw is empty")
	}
}

func TestCoverageDetailDecode(t *testing.T) {
	raw := `{
		"name":"projects/000/locations/us/instances/00000000-0000-0000-0000-000000000000/coverageDetails/campaign--uuid_ur_rule1",
		"threatCollection":"projects/000/locations/us/instances/00000000-0000-0000-0000-000000000000/threatCollections/campaign--uuid",
		"rule":"projects/000/locations/us/instances/00000000-0000-0000-0000-000000000000/rules/ur_rule1"
	}`
	var d CoverageDetail
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	if d.ThreatCollection == "" {
		t.Error("ThreatCollection is empty")
	}
	if d.Rule == "" {
		t.Error("Rule is empty")
	}
	if len(d.Raw) == 0 {
		t.Error("Raw is empty")
	}
}

func TestCoverageFilter(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want string
	}{
		{
			name: "empty",
			ids:  nil,
			want: "",
		},
		{
			name: "single",
			ids:  []string{"campaign--abc"},
			want: `threat_collection:"campaign--abc"`,
		},
		{
			name: "multiple OR-joined",
			ids:  []string{"campaign--abc", "report--26-123"},
			want: `threat_collection:"campaign--abc" OR threat_collection:"report--26-123"`,
		},
		{
			name: "full resource name extracts last segment",
			ids:  []string{"projects/123/locations/us/instances/uuid/threatCollections/campaign--abc"},
			want: `threat_collection:"campaign--abc"`,
		},
		{
			name: "blanks filtered",
			ids:  []string{"", "  ", "campaign--abc"},
			want: `threat_collection:"campaign--abc"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CoverageFilter(tt.ids)
			if got != tt.want {
				t.Errorf("CoverageFilter(%v) =\n  %q\nwant\n  %q", tt.ids, got, tt.want)
			}
		})
	}
}

func TestCoverageFilterChunking(t *testing.T) {
	// Generate 40 ids (at the chunk limit) — should produce one clause.
	ids := make([]string, 40)
	for i := range ids {
		ids[i] = "campaign--" + strings.Repeat("a", i+1)
	}
	f := CoverageFilter(ids)
	count := strings.Count(f, "threat_collection:")
	if count != 40 {
		t.Errorf("40 ids produced %d filter terms, want 40", count)
	}
}

func TestBatchGetChunking(t *testing.T) {
	// Verify the chunk size constant.
	if batchGetIocAssociationsChunkSize != 80 {
		t.Errorf("chunk size = %d, want 80", batchGetIocAssociationsChunkSize)
	}
	if coverageFilterChunkSize != 40 {
		t.Errorf("coverage chunk size = %d, want 40", coverageFilterChunkSize)
	}
}
