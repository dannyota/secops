package mirror

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

// TestLiveReconcileDataTapWriteSmoke exercises the datataps engine write loop on an
// inert throwaway: create a tap pointing at a clearly-nonexistent Pub/Sub topic
// (so it streams nowhere), round-trip it, edit the filter, then delete it.
// Gated on SECOPS_SIEM_SMOKE=1 + SECOPS_SIEM_SMOKE_WRITE=1.
//
// A working tap requires a real topic + the Pub/Sub Publisher grant to
// publisher@chronicle-data-tap.iam.gserviceaccount.com, which a smoke can't set
// up; if the API rejects the create on that prerequisite (4xx) it is skipped
// rather than failed.
func TestLiveReconcileDataTapWriteSmoke(t *testing.T) {
	c, ctx := liveChronicleClient(t)
	requireSIEMSmokeWrite(t)

	s, ok := BuildSIEMSurface("datataps", c)
	if !ok {
		t.Fatal("datataps is not a registered engine surface")
	}
	label := smokeLabel("tap")
	st := c.Settings()
	topic := fmt.Sprintf("projects/%s/topics/secopsctl-smoketest-nonexistent", st.ProjectID)
	dir := t.TempDir()

	createCanon, err := reconcile.Canonicalize(fmt.Appendf(nil,
		`{"display_name":%q,"filter":"ALL_UDM_EVENTS","serialization_format":"JSON_OBJECT","topic":%q}`, label, topic))
	if err != nil {
		t.Fatal(err)
	}
	local := reconcile.Object{Slug: Slugify(label), Canonical: createCanon}

	var serverID string
	deleted := false
	t.Cleanup(func() {
		if deleted || serverID == "" {
			return
		}
		if derr := c.DeleteDataTap(ctx, lastSegment(serverID)); derr != nil {
			t.Logf("cleanup: delete throwaway tap %q: %v", label, derr)
		}
	})

	echo, err := s.Create(ctx, local)
	if err != nil {
		// Missing topic / Pub/Sub grant / feature-gate → skip (prerequisite, not a bug).
		if ae, ok := errors.AsType[*chronicle.APIError](err); ok && ae.Status >= 400 && ae.Status < 500 {
			t.Skipf("dataTap create needs a real Pub/Sub topic + publisher grant (HTTP %d): %s", ae.Status, ae.Body)
		}
		t.Fatalf("create data tap: %v", err)
	}
	serverID = echo.ServerID
	if serverID == "" {
		t.Fatal("create returned no ServerID")
	}

	// Round-trip: write the echo, reload, canonical must match.
	if err := s.Write(dir, echo); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := s.LoadDir(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 || !bytes.Equal(loaded[0].Canonical, echo.Canonical) {
		t.Fatalf("round-trip canonical mismatch:\n echo: %s\n disk: %v", echo.Canonical, loaded)
	}

	// Update: change the filter; one update reconciles clean.
	edited := reconcile.Object{
		Slug: echo.Slug, ServerID: serverID,
		Canonical: json.RawMessage(strings.Replace(string(echo.Canonical), "ALL_UDM_EVENTS", "ALERT_UDM_EVENTS", 1)),
	}
	echo2, err := s.Update(ctx, edited, echo)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	serverID = echo2.ServerID // update is delete+recreate → the id changed; track the new one
	if !strings.Contains(string(echo2.Canonical), "ALERT_UDM_EVENTS") {
		t.Errorf("update not applied:\n%s", echo2.Canonical)
	}

	// Delete + confirm gone.
	if err := s.Delete(ctx, echo2); err != nil {
		t.Fatalf("delete: %v", err)
	}
	deleted = true
	if _, gerr := c.GetDataTap(ctx, lastSegment(serverID)); gerr == nil {
		t.Errorf("data tap still present after delete")
	}
}
