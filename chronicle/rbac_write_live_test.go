package chronicle_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"danny.vn/secops/chronicle"
)

// TestLiveDataAccessLabelWriteSmoke validates the data-access label write path
// (Wave 10) on a uniquely-named, UNBOUND throwaway label: create → read back →
// delete → confirm gone. An unbound label is referenced by no scope, so it
// grants/restricts no one's access, and it is deleted by exact id at the end, so
// it leaves zero residue.
//
// This surface has two quirks that shape the test:
//   - it can return an error on create yet still persist the object, and a
//     created-then-deleted id is tombstoned (its name can't be reused) — so the
//     id is made UNIQUE per run to avoid a stale-name clash;
//   - create→list has indexing lag (a just-created label may not appear in a
//     list yet) — so cleanup deletes by the EXACT id, never via a list sweep.
//
// Gated on SECOPS_SIEM_SMOKE=1 + SECOPS_SIEM_SMOKE_WRITE=1.
func TestLiveDataAccessLabelWriteSmoke(t *testing.T) {
	c, ctx := liveChronicle(t)
	if os.Getenv("SECOPS_SIEM_SMOKE_WRITE") != "1" {
		t.Skip("set SECOPS_SIEM_SMOKE_WRITE=1 to run (creates + deletes a throwaway data-access label)")
	}

	id := fmt.Sprintf("secopsctl-smoke-%d", time.Now().UnixNano())
	body := map[string]any{
		"displayName": id,
		"description": "secopsctl write-smoke; unbound, safe to delete",
		// A data-access label is defined by a UDM query; use one that is valid
		// syntax but matches nothing real, so the throwaway label is inert.
		"udmQuery": `metadata.vendor_name = "secopsctl-smoke-never-matches"`,
	}

	// Delete by exact id always — the only reliable cleanup given list lag.
	t.Cleanup(func() { _ = c.DeleteDataAccessLabel(ctx, id) })

	created, err := c.CreateDataAccessLabel(ctx, id, body)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID != id {
		t.Errorf("created id=%q, want %q", created.ID, id)
	}

	got, err := c.GetDataAccessLabel(ctx, id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	if got.ID != id {
		t.Errorf("read-back id=%q, want %q", got.ID, id)
	}

	if err := c.DeleteDataAccessLabel(ctx, id); err != nil {
		t.Fatalf("delete %s: %v", id, err)
	}
	if _, err := c.GetDataAccessLabel(ctx, id); !chronicle.IsNotFound(err) {
		t.Errorf("expected not-found after delete, got: %v", err)
	}
}
