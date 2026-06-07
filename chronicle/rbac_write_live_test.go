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

// TestLiveRiskConfigWriteSmoke validates UpdateRiskConfig idempotently: it reads
// the current entity-risk-scoring config and writes back the SAME score values,
// then re-reads and asserts nothing changed. Scoring is unchanged (no-op values);
// note this materializes an explicit risk-config record equal to the defaults.
// Gated on SECOPS_SIEM_SMOKE=1 + SECOPS_SIEM_SMOKE_WRITE=1.
func TestLiveRiskConfigWriteSmoke(t *testing.T) {
	c, ctx := liveChronicle(t)
	if os.Getenv("SECOPS_SIEM_SMOKE_WRITE") != "1" {
		t.Skip("set SECOPS_SIEM_SMOKE_WRITE=1 to run (idempotent same-value risk-config write)")
	}
	cur, err := c.GetRiskConfig(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// Write back exactly the current score values — a no-op for scoring.
	same := chronicle.RiskConfig{
		DefaultDetectionRiskScore:     cur.DefaultDetectionRiskScore,
		DefaultAlertRiskScore:         cur.DefaultAlertRiskScore,
		DefaultWeightingFactor:        cur.DefaultWeightingFactor,
		DefaultClosedAlertCoefficient: cur.DefaultClosedAlertCoefficient,
	}
	if _, err := c.UpdateRiskConfig(ctx, same); err != nil {
		t.Fatalf("update (same-value): %v", err)
	}
	after, err := c.GetRiskConfig(ctx)
	if err != nil {
		t.Fatalf("re-get: %v", err)
	}
	eq := func(a, b *float64) bool {
		if a == nil || b == nil {
			return a == b
		}
		return *a == *b
	}
	if !eq(cur.DefaultDetectionRiskScore, after.DefaultDetectionRiskScore) ||
		!eq(cur.DefaultAlertRiskScore, after.DefaultAlertRiskScore) ||
		!eq(cur.DefaultWeightingFactor, after.DefaultWeightingFactor) ||
		!eq(cur.DefaultClosedAlertCoefficient, after.DefaultClosedAlertCoefficient) {
		t.Errorf("risk config score values changed after an idempotent write")
	}
}

// TestLiveDataAccessScopeWriteSmoke validates the data-access scope write path on
// a throwaway, UNASSIGNED scope: it creates a throwaway label, a scope that allows
// that label, then reads back and deletes both (by exact id). An unassigned scope
// is granted to no user/role, so it changes no one's access. Same quirk-handling as
// the label smoke (unique ids, delete by exact id). Gated on SECOPS_SIEM_SMOKE=1 +
// SECOPS_SIEM_SMOKE_WRITE=1.
func TestLiveDataAccessScopeWriteSmoke(t *testing.T) {
	c, ctx := liveChronicle(t)
	if os.Getenv("SECOPS_SIEM_SMOKE_WRITE") != "1" {
		t.Skip("set SECOPS_SIEM_SMOKE_WRITE=1 to run (creates + deletes a throwaway scope + label)")
	}

	stamp := time.Now().UnixNano()
	labelID := fmt.Sprintf("secopsctl-smoke-%d", stamp)
	scopeID := fmt.Sprintf("secopsctl-smoke-scope-%d", stamp)
	t.Cleanup(func() {
		_ = c.DeleteDataAccessScope(ctx, scopeID)
		_ = c.DeleteDataAccessLabel(ctx, labelID)
	})

	label, err := c.CreateDataAccessLabel(ctx, labelID, map[string]any{
		"description": "secopsctl scope-smoke label; safe to delete",
		"udmQuery":    `metadata.vendor_name = "secopsctl-smoke-never-matches"`,
	})
	if err != nil {
		t.Fatalf("create label: %v", err)
	}

	_ = label
	scope, err := c.CreateDataAccessScope(ctx, scopeID, map[string]any{
		"displayName":             scopeID,
		"description":             "secopsctl write-smoke; unassigned, safe to delete",
		"allowAll":                false,
		"allowedDataAccessLabels": []map[string]any{{"dataAccessLabel": labelID}},
	})
	if err != nil {
		t.Fatalf("create scope: %v", err)
	}
	if scope.ID != scopeID {
		t.Errorf("created scope id=%q, want %q", scope.ID, scopeID)
	}

	if _, err := c.GetDataAccessScope(ctx, scopeID); err != nil {
		t.Fatalf("get scope: %v", err)
	}
	if err := c.DeleteDataAccessScope(ctx, scopeID); err != nil {
		t.Fatalf("delete scope: %v", err)
	}
	if _, err := c.GetDataAccessScope(ctx, scopeID); !chronicle.IsNotFound(err) {
		t.Errorf("expected scope not-found after delete, got: %v", err)
	}
	if err := c.DeleteDataAccessLabel(ctx, labelID); err != nil {
		t.Fatalf("delete label: %v", err)
	}
}
