package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"danny.vn/secops/soar/legacy"
)

// Live reconcile WRITE smoke. Two opt-in gates keep it out of normal/CI runs and
// keep mutations safe (mirroring soar/legacy/live_support_test.go):
//
//   - SECOPS_SOAR_SMOKE=1        builds a live client (read).
//   - SECOPS_SOAR_SMOKE_WRITE=1  additionally runs the create/update/delete cycle.
//
// It drives the actual engine (reconcile.Pull/BuildPlan/Push) — the write machinery
// shared by every reconcile surface — against a uniquely-labeled, inert, self-
// deleting throwaway, so validating it once de-risks all surfaces. The webhook is a
// passive inbound endpoint that does nothing until called, and t.Cleanup deletes it
// even on failure.

// TestLiveSOARCaseVerbsWriteSmoke validates the imperative `soar case` verbs
// against DISPOSABLE manual cases. It creates two throwaway cases (CreateManualCase
// — which forces the non-null Entities/Playbooks/Tags the server NPEs on if
// omitted, so it returns the new id cleanly), exercises the reversible per-case
// verbs on one, merges the second into it, and closes it. Cleanup closes both
// (BulkCloseCases) and best-effort hard-deletes them — RetentionDeleteCases needs
// a retention permission the AppKey role may lack (403), in which case the cases
// remain CLOSED. So this is rerun-tolerant but not zero-residue without that grant.
func TestLiveSOARCaseVerbsWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	env := firstEnvironmentName(ctx, lc)
	if env == "" {
		t.Skip("no environment available to scope a throwaway case")
	}
	role := firstSocRoleName(ctx, lc)
	if role == "" {
		t.Skip("no SOC role available for assignedUser")
	}

	mkCase := func() int {
		label := strings.ReplaceAll(smokeLabel("case"), "-", "_")
		id, err := lc.CreateManualCase(ctx, legacy.ManualCaseRequest{
			Title: label, AssignedUser: "@" + role, Reason: "secopsctl smoke",
			Priority: legacy.PriorityLow, Environment: env, AlertName: label,
			OccurenceTime: time.Now().UTC().Format(time.RFC3339),
			// Entities/Playbooks/Tags left nil → the SDK sends [] (server NPEs on null).
		})
		if err != nil {
			t.Fatalf("create case: %v", err)
		}
		if id == 0 {
			t.Fatal("create returned case id 0")
		}
		return id
	}

	a := mkCase()
	b := mkCase()
	t.Cleanup(func() {
		_, _ = lc.BulkCloseCases(ctx, legacy.BulkCloseRequest{
			CasesIDs: []int{a, b}, CloseReason: legacy.CloseMaintenance,
			RootCause: "secopsctl smoke", CloseComment: "secopsctl smoke", DynamicParameters: []any{},
		})
		// Best-effort hard delete (no-op 403 unless the role has retention perms).
		_, _ = lc.RetentionDeleteCases(ctx, map[string]any{"batchSize": 100, "caseIds": []int{a, b}})
	})

	chk := func(name string, _ json.RawMessage, err error) {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Reversible per-case verbs on A.
	r, e := lc.RenameCase(ctx, map[string]any{"caseId": a, "title": "secopsctl smoke (renamed)"})
	chk("rename", r, e)
	r, e = lc.ChangeCaseDescription(ctx, map[string]any{"caseId": a, "description": "secopsctl smoke"})
	chk("describe", r, e)
	r, e = lc.ChangeCaseImportanceStatus(ctx, map[string]any{"caseId": a, "isImportant": true})
	chk("importance", r, e)
	r, e = lc.ChangeCasePriority(ctx, map[string]any{"caseId": a, "priority": int(legacy.PriorityMedium)})
	chk("priority", r, e)
	r, e = lc.AddCaseTag(ctx, map[string]any{"caseId": a, "tag": "secopsctl-smoke"})
	chk("tag", r, e)
	r, e = lc.RemoveCaseTag(ctx, map[string]any{"caseId": a, "tag": "secopsctl-smoke"})
	chk("untag", r, e)
	r, e = lc.ChangeCaseStage(ctx, map[string]any{"caseId": a, "stage": "Triage"})
	chk("stage", r, e)

	r, e = lc.AddCaseComment(ctx, map[string]any{"caseId": a, "comment": "secopsctl smoke comment"})
	chk("comment add", r, e)
	if raw, err := lc.CaseXListComments(ctx, url.Values{"CaseId": {strconv.Itoa(a)}}); err != nil {
		t.Errorf("comment list: %v", err)
	} else if !bytes.Contains(raw, []byte("secopsctl smoke comment")) {
		t.Errorf("comment list does not include the added comment: %s", truncate(string(raw), 400))
	}

	// Per-ALERT verbs on a third throwaway case (its manual alert is the target):
	// re-prioritize → close → reopen → move into A. Then the case-level close →
	// reopen round-trip on the now-empty case.
	cse := mkCase()
	t.Cleanup(func() {
		_, _ = lc.BulkCloseCases(ctx, legacy.BulkCloseRequest{
			CasesIDs: []int{cse}, CloseReason: legacy.CloseMaintenance,
			RootCause: "secopsctl smoke", CloseComment: "secopsctl smoke", DynamicParameters: []any{},
		})
	})
	ident, alertName, alertPrio := firstCaseAlert(ctx, t, lc, cse)
	if ident != "" {
		r, e = lc.UpdateAlertPriority(ctx, legacy.UpdateAlertPriorityRequest{
			CaseID: cse, AlertIdentifier: ident, AlertName: alertName,
			PreviousPriority: legacy.CasePriority(alertPrio), Priority: legacy.PriorityMedium,
		})
		chk("alert priority", r, e)
		r, e = lc.CloseAlert(ctx, legacy.CloseAlertRequest{
			SourceCaseID: cse, AlertIdentifier: ident, Reason: "Maintenance",
			RootCause: "secopsctl smoke", Comment: "secopsctl smoke",
		})
		chk("alert close", r, e)
		r, e = lc.ReopenAlert(ctx, legacy.ReopenAlertRequest{CaseID: cse, AlertIdentifier: ident})
		chk("alert reopen", r, e)
		r, e = lc.MoveAlertToNewCase(ctx, legacy.MoveAlertRequest{
			AlertIdentifier: ident, SourceCaseID: cse, DestinationCaseID: a,
		})
		chk("alert move", r, e)
	} else {
		t.Log("throwaway case has no alert identifier; per-alert verbs skipped")
	}
	r, e = lc.CloseCase(ctx, map[string]any{
		"caseId": cse, "reason": "Maintenance", "rootCause": "secopsctl smoke", "comment": "secopsctl smoke",
	})
	chk("close (pre-reopen)", r, e)
	r, e = lc.BulkReopenCase(ctx, map[string]any{"casesIds": []int{cse}, "reopenComment": "secopsctl smoke"})
	chk("case reopen", r, e)

	// Merge B into A, then close A (destructive — both are throwaways). The merge
	// target must be present in casesIds ("Cannot merge cases with case that is not
	// selected"), so the set includes both A and B.
	r, e = lc.MergeCases(ctx, map[string]any{"casesIds": []int{a, b}, "caseToMergeWith": a})
	chk("merge", r, e)
	r, e = lc.CloseCase(ctx, map[string]any{
		"caseId": a, "reason": "Maintenance", "rootCause": "secopsctl smoke", "comment": "secopsctl smoke",
	})
	chk("close", r, e)
}

// TestLiveJobInstanceSetWriteSmoke validates the `soar job instance set` PUT
// shape with an IDEMPOTENT same-value update: the first instance's record is
// fetched, isEnabled overlaid with its CURRENT value (byte-preserving
// RawMessage overlay — the same construction the CLI uses), PUT back, and read
// back to confirm nothing changed. This answers the open shape question — the
// swagger's JobDataUpdateRequest declares jobDefinitionId/jobDefinitionName,
// which the live list records do not carry — without disturbing any schedule.
func TestLiveJobInstanceSetWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)

	raw, err := lc.ListJobInstances(ctx)
	if err != nil {
		t.Fatalf("list job instances: %v", err)
	}
	var records []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &records); err != nil || len(records) == 0 {
		t.Skipf("no job instances to exercise (err=%v)", err)
	}
	body := records[0]
	id := strings.Trim(string(body["id"]), `"`)
	before := append(json.RawMessage(nil), body["isEnabled"]...)
	// Same-value overlay: the PUT changes nothing if the shape is accepted.
	body["isEnabled"] = before
	if _, err := lc.UpdateJobInstance(ctx, body); err != nil {
		t.Fatalf("same-value update: %v", err)
	}
	after, err := lc.ListJobInstances(ctx)
	if err != nil {
		t.Fatalf("re-list: %v", err)
	}
	var post []map[string]json.RawMessage
	if err := json.Unmarshal(after, &post); err != nil {
		t.Fatalf("decode re-list: %v", err)
	}
	for _, rec := range post {
		if strings.Trim(string(rec["id"]), `"`) == id {
			if !bytes.Equal(bytes.TrimSpace(rec["isEnabled"]), bytes.TrimSpace(before)) {
				t.Errorf("instance %s isEnabled changed: %s -> %s", id, before, rec["isEnabled"])
			}
			return
		}
	}
	t.Errorf("instance %s missing after same-value update", id)
}

// firstCaseAlert reads a case's first alert (identifier, name, priority) for the
// per-alert verb smoke; empty identifier means the case carries no alert yet.
func firstCaseAlert(ctx context.Context, t *testing.T, lc *legacy.Client, caseID int) (string, string, int) {
	t.Helper()
	raw, err := lc.GetCaseFullDetails(ctx, caseID)
	if err != nil {
		t.Errorf("get case %d details: %v", caseID, err)
		return "", "", 0
	}
	var cs struct {
		Alerts []struct {
			Identifier string `json:"identifier"`
			Name       string `json:"name"`
			Priority   int    `json:"priority"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(raw, &cs); err != nil || len(cs.Alerts) == 0 {
		return "", "", 0
	}
	return cs.Alerts[0].Identifier, cs.Alerts[0].Name, cs.Alerts[0].Priority
}
