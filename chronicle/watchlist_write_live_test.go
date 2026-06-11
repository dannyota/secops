package chronicle_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"danny.vn/secops/chronicle"
)

// TestLiveWatchlistEntityWriteSmoke validates the watchlist write family on a
// THROWAWAY watchlist of its own (never a pre-existing one): create a
// uniquely-named watchlist, add one throwaway hostname entity, batch-remove
// it, and delete the watchlist again — cleanup attempts run even when an
// inner step fails. Gated on SECOPS_SIEM_SMOKE=1 + SECOPS_SIEM_SMOKE_WRITE=1.
func TestLiveWatchlistEntityWriteSmoke(t *testing.T) {
	if os.Getenv("SECOPS_SIEM_SMOKE_WRITE") != "1" {
		t.Skip("write smoke — set SECOPS_SIEM_SMOKE_WRITE=1 (and SECOPS_SIEM_SMOKE=1) to run")
	}
	c, ctx := liveChronicle(t)

	stamp := time.Now().UnixNano()
	wlName := fmt.Sprintf("secopsctl-smoke-wl-%d", stamp)
	wl, err := c.CreateWatchlist(ctx, wlName, wlName, "secopsctl write smoke (safe to delete)", 1)
	if err != nil {
		t.Fatalf("create watchlist: %v", err)
	}
	wlID := wl.Name[strings.LastIndex(wl.Name, "/")+1:]
	t.Cleanup(func() {
		if err := c.DeleteWatchlist(ctx, wlID, true); err != nil {
			t.Errorf("cleanup: delete watchlist %s: %v — REMOVE %q manually", wlID, err, wlName)
		}
	})
	t.Logf("OK create -> watchlist %s", wlID)

	host := fmt.Sprintf("secopsctl-smoke-%d", stamp)
	entity := chronicle.WatchlistEntity{Asset: map[string]any{"hostname": host}}
	added, err := c.AddWatchlistEntity(ctx, wlID, entity)
	var ae *chronicle.APIError
	if errors.As(err, &ae) && ae.Status == 501 {
		// The membership ops can be UNIMPLEMENTED per instance while the
		// watchlist CRUD itself works; a 501 still proves the request shape
		// passed validation (a wrong shape is a 400).
		t.Skipf("entities:add UNIMPLEMENTED on this instance (501) — request shape accepted, surface gated")
	}
	if err != nil {
		t.Fatalf("entities:add: %v", err)
	}
	var stored struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(added, &stored); err != nil || stored.Name == "" {
		t.Fatalf("add response carries no entity name (%v): %s", err, added)
	}
	t.Logf("OK entities:add -> %s (hostname %s)", stored.Name[strings.LastIndex(stored.Name, "/")+1:], host)

	if err := c.RemoveWatchlistEntity(ctx, stored.Name); err != nil {
		t.Fatalf("entities:remove: %v (the throwaway watchlist is deleted by cleanup regardless)", err)
	}
	t.Log("OK entities:remove")
}
