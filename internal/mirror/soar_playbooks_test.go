package mirror

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/secops/internal/mirror/reconcile"
)

// A playbook body round-trips through the surface (Write → LoadDir) with
// matching canonicals — the diff basis is stable across write and reload.
func TestPlaybookSurfaceRoundTrips(t *testing.T) {
	s := playbooksSurface(nil)

	body := []byte(`{
	  "name": "Notify",
	  "steps": [
	    {"id": 1, "parameters": {"URL": "https://example.com/hook?sig=SECRETTOKEN&x=1"}}
	  ]
	}`)

	dir := t.TempDir()
	liveObj := mustBuildPlaybook(t, body)
	if err := s.Write(dir, liveObj); err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(filepath.Join(dir, liveObj.Slug+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), "SECRETTOKEN") {
		t.Fatalf("body should preserve inline values: %s", onDisk)
	}

	loaded, err := s.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("LoadDir returned %d objects", len(loaded))
	}
	if !bytes.Equal(loaded[0].Canonical, liveObj.Canonical) {
		t.Errorf("loaded canonical != live canonical (would phantom-drift):\nlive:\n%s\ndisk:\n%s", liveObj.Canonical, loaded[0].Canonical)
	}
}

func mustBuildPlaybook(t *testing.T, raw []byte) reconcile.Object {
	t.Helper()
	canon, err := canonicalPlaybook(raw)
	if err != nil {
		t.Fatal(err)
	}
	return reconcile.Object{Slug: Slugify(playbookNameOf(raw)), ServerID: playbookNameOf(raw), Canonical: canon, Raw: raw}
}
