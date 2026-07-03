package mirror

import (
	"bytes"
	"encoding/base64"
	"testing"

	"danny.vn/secops/chronicle"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// sampleParser is a live active parser (CBN base64-encoded as the API returns it).
func sampleParser(name, cbn string) chronicle.Parser {
	return chronicle.Parser{
		Name:         name,
		State:        "ACTIVE",
		Type:         "CUSTOM",
		ReleaseStage: "RELEASED",
		CreateTime:   "2026-01-01T00:00:00Z",
		CBN:          b64(cbn),
		Creator:      map[string]any{"source": "CUSTOMER"},
		VersionInfo:  map[string]any{"version": "1", "rollbackAvailable": true},
	}
}

// TestParserRoundTrip: a live active parser written to disk and re-loaded
// canonicalizes identically — a pulled parser pushes back in sync.
func TestParserRoundTrip(t *testing.T) {
	const lt = "OKTA"
	cbn := "filter {\n  mutate { add_field => { \"x\" => \"y\" } }\n}\n"
	p := sampleParser("projects/p/locations/r/instances/c/logTypes/OKTA/parsers/pa_111", cbn)

	live, err := parserLiveObject(lt, p)
	if err != nil {
		t.Fatalf("live object: %v", err)
	}
	dir := t.TempDir()
	if err := writeParserObject(dir, live); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := loadParsers(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d, want 1", len(loaded))
	}
	if loaded[0].ServerID != p.Name {
		t.Errorf("ServerID = %q, want %q", loaded[0].ServerID, p.Name)
	}
	if loaded[0].Slug != lt {
		t.Errorf("Slug = %q, want %q", loaded[0].Slug, lt)
	}
	if !bytes.Equal(loaded[0].Canonical, live.Canonical) {
		t.Errorf("round-trip canonical mismatch:\n live: %s\n disk: %s", live.Canonical, loaded[0].Canonical)
	}
}

// TestParserCanonicalExcludesID: the volatile parser id must NOT be in the
// canonical, or every create-new-version edit would phantom-diff. Two parsers
// with different ids/versions but the same CBN canonicalize identically.
func TestParserCanonicalExcludesID(t *testing.T) {
	const lt = "WINDOWS_AD"
	cbn := "filter { }\n"
	a, _ := parserLiveObject(lt, sampleParser("x/logTypes/WINDOWS_AD/parsers/pa_aaa", cbn))
	b := sampleParser("x/logTypes/WINDOWS_AD/parsers/pa_bbb", cbn)
	b.VersionInfo = map[string]any{"version": "9", "rollbackAvailable": false}
	b.CreateTime = "2027-09-09T00:00:00Z"
	bo, _ := parserLiveObject(lt, b)
	if !bytes.Equal(a.Canonical, bo.Canonical) {
		t.Errorf("canonical depends on volatile fields (id/version/time):\n a: %s\n b: %s", a.Canonical, bo.Canonical)
	}
	if a.ServerID == bo.ServerID {
		t.Error("test setup: the two parsers should have different ids")
	}
}

// TestActiveParserPrefersCustom verifies active-parser selection: the custom
// version is preferred over prebuilt when both are active.
func TestActiveParserPrefersCustom(t *testing.T) {
	mk := func(name, typ, state string) chronicle.Parser {
		return chronicle.Parser{Name: name, State: state, Type: typ}
	}
	tests := []struct {
		name  string
		input []chronicle.Parser
		want  string // expected Name, or "" for nil
	}{
		{
			"prebuilt+custom active, prebuilt first",
			[]chronicle.Parser{
				mk("prebuilt-1", "PREBUILT", "ACTIVE"),
				mk("custom-1", "CUSTOM", "ACTIVE"),
			},
			"custom-1",
		},
		{
			"prebuilt+custom active, custom first",
			[]chronicle.Parser{
				mk("custom-1", "CUSTOM", "ACTIVE"),
				mk("prebuilt-1", "PREBUILT", "ACTIVE"),
			},
			"custom-1",
		},
		{
			"only prebuilt active",
			[]chronicle.Parser{
				mk("prebuilt-1", "PREBUILT", "ACTIVE"),
			},
			"prebuilt-1",
		},
		{
			"prebuilt inactive, custom active",
			[]chronicle.Parser{
				mk("prebuilt-1", "PREBUILT", "INACTIVE"),
				mk("custom-1", "CUSTOM", "ACTIVE"),
			},
			"custom-1",
		},
		{
			"empty list",
			nil,
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := activeParser(tt.input)
			if tt.want == "" {
				if got != nil {
					t.Errorf("activeParser() = %q, want nil", got.Name)
				}
				return
			}
			if got == nil {
				t.Fatalf("activeParser() = nil, want %q", tt.want)
			}
			if got.Name != tt.want {
				t.Errorf("activeParser() = %q, want %q", got.Name, tt.want)
			}
		})
	}
}

// TestParserCBNRoundTripExact: CBN bytes survive the write→read cycle exactly
// (the .conf is the human diff surface; any munging would phantom-diff).
func TestParserCBNRoundTripExact(t *testing.T) {
	const lt = "GCP_CLOUDAUDIT"
	for _, cbn := range []string{
		"no trailing newline",
		"trailing newline\n",
		"multi\nline\nbody\n",
		"",
	} {
		live, err := parserLiveObject(lt, sampleParser("x/logTypes/GCP_CLOUDAUDIT/parsers/pa_1", cbn))
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		if err := writeParserObject(dir, live); err != nil {
			t.Fatal(err)
		}
		loaded, err := loadParsers(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded) != 1 || !bytes.Equal(loaded[0].Canonical, live.Canonical) {
			t.Errorf("CBN %q did not round-trip byte-exact", cbn)
		}
	}
}
