package mirror

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/secops/internal/mirror/reconcile"
)

// withValueRedactor sets the process-wide value redactor for the duration of a
// test and restores it after.
func withValueRedactor(t *testing.T, patterns []string) {
	t.Helper()
	r, err := NewValueRedactor(patterns)
	if err != nil {
		t.Fatal(err)
	}
	prev := valueRedactor
	SetValueRedactor(r)
	t.Cleanup(func() { SetValueRedactor(prev) })
}

// A playbook with an inline secret (a webhook URL with a token in a step param)
// must be written to disk REDACTED, and the redacted file must canonicalize to the
// SAME value the live object canonicalizes to — so a redacted pull does not make
// drift/push phantom-report the masked field.
func TestPlaybookSurfaceRedactionRoundTrips(t *testing.T) {
	withValueRedactor(t, []string{`sig=[^&"]+`})
	s := playbooksSurface(nil) // List/Create/Update unused here (no client needed)

	live := []byte(`{
	  "name": "Notify",
	  "steps": [
	    {"id": 1, "parameters": {"URL": "https://example.com/hook?sig=SUPERSECRET&x=1"}}
	  ]
	}`)

	// The live object: Write must persist the redacted body (build keepRaw=false is
	// what List uses). Reach it via the same path the engine takes — build through
	// Write, then LoadDir, and compare canonicals.
	dir := t.TempDir()
	liveObj := mustBuildPlaybook(t, live, false)
	if strings.Contains(string(liveObj.Raw), "SUPERSECRET") {
		t.Fatalf("live Raw still carries the secret: %s", liveObj.Raw)
	}
	if err := s.Write(dir, liveObj); err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(filepath.Join(dir, liveObj.Slug+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(onDisk), "SUPERSECRET") {
		t.Fatalf("secret leaked to disk: %s", onDisk)
	}

	loaded, err := s.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("LoadDir returned %d objects", len(loaded))
	}
	if !bytes.Equal(loaded[0].Canonical, liveObj.Canonical) {
		t.Errorf("redacted file canonical != live canonical (would phantom-drift):\nlive:\n%s\ndisk:\n%s", liveObj.Canonical, loaded[0].Canonical)
	}
}

// The save guard refuses to deploy a body still carrying the redaction marker, so
// a pulled-and-redacted playbook can't be pushed back masking the real value.
func TestPlaybookSurfaceRefusesMarkerOnSave(t *testing.T) {
	withValueRedactor(t, nil)
	s := playbooksSurface(nil)
	body := []byte(`{"name":"Notify","steps":[{"parameters":{"URL":"` + redactedMarker + `"}}]}`)
	_, err := s.Create(context.Background(), reconcile.Object{Slug: "notify", Raw: body})
	if err == nil || !strings.Contains(err.Error(), "redaction marker") {
		t.Fatalf("expected a redaction-marker refusal, got %v", err)
	}
}

// mustBuildPlaybook reaches the surface's unexported build via Write/LoadDir is
// awkward, so reconstruct the canonical the same way the surface does.
func mustBuildPlaybook(t *testing.T, raw []byte, keepRaw bool) reconcile.Object {
	t.Helper()
	redacted, err := valueRedactor.RedactJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	canon, err := canonicalPlaybook(redacted)
	if err != nil {
		t.Fatal(err)
	}
	stored := redacted
	if keepRaw {
		stored = raw
	}
	return reconcile.Object{Slug: Slugify(playbookNameOf(raw)), ServerID: playbookNameOf(raw), Canonical: canon, Raw: stored}
}
