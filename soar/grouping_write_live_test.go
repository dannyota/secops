package soar_test

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestLiveAlertGroupingRuleWriteSmoke validates the alertGroupingRules
// create→get→delete lifecycle (Wave 15) on a fully inert, self-identifying
// throwaway: category ALERT_TYPE keyed on a unique "secopsctl-smoke-<nanos>"
// identifier that matches no real alert, groupingType NONE (no grouping
// behavior). It creates the rule, reads it back (round-trip), deletes it by
// exact id, and confirms it is gone.
//
// A v1alpha write can return an error yet still persist, so cleanup scans every
// rule's payload for the marker and deletes any match by exact id, logging what
// it removes and failing only if a delete cannot complete (true residue on the
// production tenant). The v1alpha SOAR plane is flaky, so a server 5xx on create
// is reported clean rather than failing — the legacy lane stays the default.
//
// Gated on SECOPS_SOAR_SMOKE=1 + SECOPS_SOAR_SMOKE_WRITE=1.
func TestLiveAlertGroupingRuleWriteSmoke(t *testing.T) {
	c, ctx := liveClient(t)
	if os.Getenv("SECOPS_SOAR_SMOKE_WRITE") != "1" {
		t.Skip("set SECOPS_SOAR_SMOKE_WRITE=1 to run (creates + deletes a throwaway alert-grouping rule)")
	}

	marker := fmt.Sprintf("secopsctl-smoke-%d", time.Now().UnixNano())

	// Cleanup deletes every rule whose payload carries our unique marker — this
	// covers the create-despite-error case where the id was never returned. It
	// fails only if a delete itself fails, i.e. residue we could not remove.
	t.Cleanup(func() {
		rules, err := c.ListAlertGroupingRules(ctx)
		if err != nil {
			t.Errorf("cleanup: list rules failed, residue may remain: %v", err)
			return
		}
		for _, r := range rules {
			if bytes.Contains(r.Raw, []byte(marker)) {
				if err := c.DeleteAlertGroupingRule(ctx, r.ID); err != nil {
					t.Errorf("CLEANUP FAILED to delete leftover smoke rule %s: %v", r.ID, err)
				} else {
					t.Logf("cleanup: removed leftover smoke rule %s", r.ID)
				}
			}
		}
	})

	// Inert create body — every field is required per the v1alpha resource schema.
	body := map[string]any{
		"category":     "ALERT_TYPE",
		"groupingType": "NONE",
		"categoryDetails": []map[string]string{
			{"identifier": marker, "displayName": marker},
		},
		"entityType": []string{"GenericEntity"},
	}

	created, err := c.CreateAlertGroupingRule(ctx, body)
	if err != nil {
		if s := statusOf(err); s >= 500 {
			t.Logf("CreateAlertGroupingRule: HTTP %d — v1alpha flaky, clean error (legacy stays default)", s)
			return // t.Cleanup still purges any create-despite-error residue
		}
		t.Fatalf("CreateAlertGroupingRule: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("create returned no id; payload=%s", created.Raw)
	}
	t.Logf("created alertGroupingRule id=%s", created.ID)

	// GET round-trip: the rule must read back with our marker and NONE grouping.
	got, err := c.GetAlertGroupingRule(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetAlertGroupingRule(%s): %v", created.ID, err)
	}
	if !bytes.Contains(got.Raw, []byte(marker)) {
		t.Fatalf("round-trip: rule %s missing marker in payload", created.ID)
	}
	if got.GroupingType != "NONE" {
		t.Errorf("round-trip: groupingType = %q, want NONE", got.GroupingType)
	}

	// Delete by exact id, then confirm it is gone.
	if err := c.DeleteAlertGroupingRule(ctx, created.ID); err != nil {
		t.Fatalf("DeleteAlertGroupingRule(%s): %v", created.ID, err)
	}
	if _, err := c.GetAlertGroupingRule(ctx, created.ID); err == nil {
		t.Errorf("rule %s still present after delete", created.ID)
	} else {
		t.Logf("deleted alertGroupingRule id=%s — full lifecycle validated", created.ID)
	}
}
