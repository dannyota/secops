package mirror

import (
	"bytes"
	"path/filepath"
	"testing"

	"danny.vn/secops/chronicle"
)

func sampleRefList() chronicle.ReferenceList {
	return chronicle.ReferenceList{
		Name:        "projects/p/locations/us/instances/i/referenceLists/my_list",
		DisplayName: "my_list",
		Description: "a test list",
		SyntaxType:  chronicle.RefListSyntaxString,
		Entries: []chronicle.ReferenceListEntry{
			{Value: "alpha"}, {Value: "beta"}, {Value: "gamma"},
		},
		RevisionCreateTime: "2026-01-01T00:00:00Z", // volatile — must not affect canonical
	}
}

// TestReflistPullPushRoundTrips: a live list rendered to disk and reloaded yields
// the SAME canonical, server id, and slug — so a pulled snapshot diffs clean and
// pushes back without conversion. This is the SIEM-side equivalent of the
// jsonSurface round-trip test and validates the engine is product-neutral.
func TestReflistPullPushRoundTrips(t *testing.T) {
	live, err := reflistObject(sampleRefList())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := writeReferenceList(dir, live); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadReferenceLists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d objects, want 1", len(loaded))
	}
	lo := loaded[0]
	if lo.ServerID != live.ServerID {
		t.Errorf("server id: loaded %q != live %q", lo.ServerID, live.ServerID)
	}
	if lo.Slug != live.Slug {
		t.Errorf("slug: loaded %q != live %q", lo.Slug, live.Slug)
	}
	if !bytes.Equal(lo.Canonical, live.Canonical) {
		t.Errorf("canonical differs (would be a phantom diff):\nlive:\n%s\nloaded:\n%s", live.Canonical, lo.Canonical)
	}

	// The .txt holds the entries verbatim; the .yaml holds the typed metadata.
	txt, _ := readEntryLines(filepath.Join(dir, "my_list.txt"))
	if len(txt) != 3 || txt[0] != "alpha" || txt[2] != "gamma" {
		t.Errorf("entries round-trip wrong: %v", txt)
	}
}

// TestReflistEmptyListNoPhantomDrift: an EMPTY reference list must canonicalize
// identically live (entries built as make([]string,0) → JSON []) and on disk (an
// empty .txt read as nil → JSON null). Without the nonNil normalization in
// canonicalRefList the two differ and drift phantom-reports the list as ~1 right
// after a clean pull.
func TestReflistEmptyListNoPhantomDrift(t *testing.T) {
	live, err := reflistObject(chronicle.ReferenceList{
		Name: "projects/p/locations/us/instances/i/referenceLists/empty", DisplayName: "empty",
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := writeReferenceList(dir, live); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadReferenceLists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d objects, want 1", len(loaded))
	}
	if !bytes.Equal(loaded[0].Canonical, live.Canonical) {
		t.Errorf("empty-list canonical differs (phantom drift):\nlive:\n%s\nloaded:\n%s", live.Canonical, loaded[0].Canonical)
	}
}

func TestReadEntryLines(t *testing.T) {
	dir := t.TempDir()
	// Missing file -> no entries, no error.
	if got, err := readEntryLines(filepath.Join(dir, "nope.txt")); err != nil || got != nil {
		t.Errorf("missing file: got %v, %v", got, err)
	}
	// Empty list writes an empty .txt -> no entries.
	empty, _ := reflistObject(chronicle.ReferenceList{Name: "x/referenceLists/empty", DisplayName: "empty"})
	if err := writeReferenceList(dir, empty); err != nil {
		t.Fatal(err)
	}
	if got, _ := readEntryLines(filepath.Join(dir, "empty.txt")); got != nil {
		t.Errorf("empty list should read back as no entries, got %v", got)
	}
}
