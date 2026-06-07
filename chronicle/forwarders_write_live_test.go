package chronicle_test

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestLiveForwarderWriteSmoke validates the Wave 12 forwarder write path against a
// labeled throwaway: create → get → update (displayName) → delete → confirm gone.
// Self-cleaning (t.Cleanup deletes even on failure). The create body matches the
// shape ingest.go already uses in production. Gated on SECOPS_SIEM_SMOKE=1 +
// SECOPS_SIEM_SMOKE_WRITE=1.
func TestLiveForwarderWriteSmoke(t *testing.T) {
	c, ctx := liveChronicle(t)
	if os.Getenv("SECOPS_SIEM_SMOKE_WRITE") != "1" {
		t.Skip("set SECOPS_SIEM_SMOKE_WRITE=1 to run (creates + deletes a throwaway forwarder)")
	}

	name := fmt.Sprintf("secopsctl-smoketest-forwarder-%d", time.Now().UnixNano())
	body := map[string]any{
		"displayName": name,
		"config": map[string]any{
			"uploadCompression": false,
			"metadata":          map[string]any{},
			"serverSettings":    map[string]any{"enabled": false},
		},
	}

	created, err := c.CreateForwarder(ctx, body)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := created.ForwarderID()
	if id == "" {
		t.Fatalf("created forwarder has no id (name=%q)", created.Name)
	}
	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		if err := c.DeleteForwarder(ctx, id); err != nil {
			t.Logf("cleanup: delete forwarder %q: %v", id, err)
		}
	})

	got, err := c.GetForwarder(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DisplayName != name {
		t.Errorf("get displayName=%q, want %q", got.DisplayName, name)
	}

	upd := name + "-edited"
	if _, err := c.UpdateForwarder(ctx, id, map[string]any{"displayName": upd}, "display_name"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if g2, err := c.GetForwarder(ctx, id); err != nil {
		t.Fatalf("get after update: %v", err)
	} else if g2.DisplayName != upd {
		t.Errorf("update not applied: displayName=%q, want %q", g2.DisplayName, upd)
	}

	if err := c.DeleteForwarder(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	deleted = true
	if _, err := c.GetForwarder(ctx, id); err == nil {
		t.Errorf("forwarder still present after delete")
	}
}
