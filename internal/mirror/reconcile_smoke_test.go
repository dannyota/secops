package mirror

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"danny.vn/secops/auth"
	"danny.vn/secops/config"
	"danny.vn/secops/internal/mirror/reconcile"
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

const (
	smokeEnvRead  = "SECOPS_SOAR_SMOKE"
	smokeEnvWrite = "SECOPS_SOAR_SMOKE_WRITE"
)

func liveLegacyClient(t *testing.T) (*legacy.Client, context.Context) {
	t.Helper()
	if os.Getenv(smokeEnvRead) != "1" {
		t.Skipf("live reconcile smoke — set %s=1 (with instance config + SOAR AppKey) to run", smokeEnvRead)
	}
	inst, err := config.Load("")
	if err != nil {
		t.Skipf("no instance config: %v", err)
	}
	key := inst.SOARAppKey
	if key == "" {
		key = auth.FromEnv("SECOPS_SOAR_APP_KEY", "SECOPS_API_KEY")
	}
	if inst.SOARURL == "" || key == "" {
		t.Skip("soar_url and/or SOAR AppKey not configured")
	}
	lc := legacy.NewClient(legacy.Settings{
		BaseURL:       inst.SOARURL,
		ProjectNumber: inst.ProjectNumberString(),
		Region:        inst.Region,
		CustomerID:    inst.CustomerID,
		ForceIPv4:     inst.ForceIPv4,
	}, auth.SOARAppKey(key), nil)
	return lc, context.Background()
}

func requireSmokeWrite(t *testing.T) {
	t.Helper()
	if os.Getenv(smokeEnvWrite) != "1" {
		t.Skipf("live WRITE smoke — set %s=1 to run (it creates/edits/deletes a throwaway on the tenant)", smokeEnvWrite)
	}
}

// smokeLabel is a unique, clearly-marked name for a throwaway resource.
func smokeLabel(kind string) string {
	return fmt.Sprintf("secopsctl-smoketest-%s-%d", kind, time.Now().UnixNano())
}

// findBySlug returns the live object whose slug matches, or false.
func findBySlug(ctx context.Context, s reconcile.Surface, slug string) (reconcile.Object, bool) {
	res, err := s.List(ctx)
	if err != nil {
		return reconcile.Object{}, false
	}
	for _, o := range res.Objects {
		if o.Slug == slug {
			return o, true
		}
	}
	return reconcile.Object{}, false
}

// firstEnvironmentName returns a live environment name to scope the webhook to.
func firstEnvironmentName(ctx context.Context, lc *legacy.Client) string {
	raw, err := lc.GetEnvironments(ctx, allRecordsSelector)
	if err != nil {
		return ""
	}
	items, err := decodeRawList(raw)
	if err != nil {
		return ""
	}
	for _, it := range items {
		if n := jsonField(it, "name"); n != "" {
			return n
		}
	}
	return ""
}

// TestLiveReconcileWebhookWriteSmoke exercises the engine's full write loop on a
// throwaway webhook: push-create (records _server.id), in-sync round-trip,
// push-update (redaction-overlay preserves the server apiKey), then delete-by-id.
// Additive throughout (never --prune), so it can only ever touch its own throwaway.
func TestLiveReconcileWebhookWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)

	s, ok := BuildSOARSurface("webhooks", lc)
	if !ok {
		t.Fatal("webhooks is not a registered engine surface")
	}
	env := firstEnvironmentName(ctx, lc)
	if env == "" {
		t.Skip("no environment available to scope a throwaway webhook")
	}

	label := smokeLabel("webhook")
	slug := Slugify(label)
	dir := t.TempDir()
	path := filepath.Join(dir, slug+".json")

	// Pull the baseline so existing objects have local files; otherwise they would
	// show as (additive-skipped) delete candidates and the in-sync check would fail.
	if _, err := reconcile.Pull(ctx, s, dir, io.Discard); err != nil {
		t.Fatalf("baseline pull: %v", err)
	}

	// Delete the throwaway on cleanup even if an assertion fails midway.
	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		if o, found := findBySlug(ctx, s, slug); found && s.Delete != nil {
			if err := s.Delete(ctx, o); err != nil {
				t.Logf("cleanup: could not delete throwaway webhook %q: %v", label, err)
			}
		}
	})

	// 1. New local file (no _server) -> engine plans a create.
	body := fmt.Sprintf(`{"name":%q,"description":"secopsctl reconcile smoke","defaultEnvironment":%q}`, label, env)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// 2. Push (additive create). A resource limit can refuse webhook create — skip then.
	var buf strings.Builder
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
		t.Fatalf("push create: %v\n%s", err, buf.String())
	}
	live, found := findBySlug(ctx, s, slug)
	if !found {
		t.Skipf("webhook create not permitted here (likely a resource limit); skipping. push log:\n%s", buf.String())
	}

	// 3. refreshLocal must have written the server id back into the operator's file.
	id, _ := serverBlock([]byte(readFile(t, path)))
	if id == "" {
		t.Fatalf("create did not record _server.id in %s:\n%s", path, readFile(t, path))
	}
	if id != live.ServerID {
		t.Fatalf("local _server.id %q != live id %q", id, live.ServerID)
	}

	// 4. A fresh plan must be clean (the created object round-trips, secret redacted).
	plan, _, err := reconcile.BuildPlan(ctx, s, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Empty() {
		t.Fatalf("post-create plan not in sync: +%d ~%d -%d",
			len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}

	// 5. Edit a field -> push update. DeepMerge overlays the edit onto the live body
	//    and drops the redacted apiKey marker, preserving the real server key.
	edited := strings.Replace(readFile(t, path), "secopsctl reconcile smoke", "secopsctl reconcile smoke (edited)", 1)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, _, _ = reconcile.BuildPlan(ctx, s, dir)
	if len(plan.Updates()) != 1 || len(plan.Creates()) != 0 {
		t.Fatalf("expected exactly one update, got +%d ~%d", len(plan.Creates()), len(plan.Updates()))
	}
	buf.Reset()
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
		t.Fatalf("push update: %v\n%s", err, buf.String())
	}
	if plan, _, _ = reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
		t.Fatalf("post-update plan not in sync: +%d ~%d -%d",
			len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}

	// 6. Delete the throwaway by id (NOT --prune, so only this object is touched).
	if err := s.Delete(ctx, live); err != nil {
		t.Fatalf("delete: %v", err)
	}
	deleted = true
	if _, stillThere := findBySlug(ctx, s, slug); stillThere {
		t.Fatalf("throwaway webhook %q still present after delete", label)
	}
}

// TestLiveReconcileTrackingListWriteSmoke validates the engine's create + update
// success path on a surface where create is permitted: it CLONES an existing
// tracking-list record (so the body is guaranteed valid), renames the inert
// entity identifier to a throwaway label, and runs engine create -> in-sync ->
// update -> in-sync. tracking-lists has no engine delete (its API delete takes a
// body), so cleanup removes the throwaway via the raw SDK. A dummy tracked entity
// matches no real events and is removed immediately.
func TestLiveReconcileTrackingListWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)

	s, ok := BuildSOARSurface("tracking-lists", lc)
	if !ok {
		t.Fatal("tracking-lists is not a registered engine surface")
	}
	seed, ok := findAny(ctx, s)
	if !ok {
		t.Skip("no existing tracking-list record to clone as a valid template")
	}

	// Clone the seed's full body, drop server-managed fields, set the throwaway id.
	var rec map[string]any
	if err := json.Unmarshal(seed.Raw, &rec); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "creationTimeUnixTimeInMs", "modificationTimeUnixTimeInMs"} {
		delete(rec, k)
	}
	label := smokeLabel("tracking")
	rec["entityIdentifier"] = label
	slug := Slugify(label)
	dir := t.TempDir()
	path := filepath.Join(dir, slug+".json")

	// Pull the baseline so the existing records have files (else they show as
	// additive-skipped delete candidates and the in-sync check fails).
	if _, err := reconcile.Pull(ctx, s, dir, io.Discard); err != nil {
		t.Fatalf("baseline pull: %v", err)
	}
	raw, _ := json.Marshal(rec)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	removed := false
	t.Cleanup(func() {
		if removed {
			return
		}
		if o, found := findBySlug(ctx, s, slug); found {
			if _, err := lc.RemoveTrackingListRecords(ctx, o.Raw); err != nil {
				t.Logf("cleanup: could not remove throwaway tracking entity %q: %v", label, err)
			}
		}
	})

	// Create.
	var buf strings.Builder
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
		t.Fatalf("push create: %v\n%s", err, buf.String())
	}
	live, found := findBySlug(ctx, s, slug)
	if !found {
		t.Fatalf("created tracking entity %q not found after create. push log:\n%s", label, buf.String())
	}

	// _server.id recorded back into the operator's file.
	if id, _ := serverBlock([]byte(readFile(t, path))); id == "" || id != live.ServerID {
		t.Fatalf("create did not record the server id (file id=%q, live id=%q)", id, live.ServerID)
	}
	// Clean round-trip.
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
		t.Fatalf("post-create plan not in sync: +%d ~%d -%d",
			len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}

	// Update: flip a benign field and confirm a single update reconciles clean.
	cur := readFile(t, path)
	edited := cur
	if strings.Contains(cur, `"category"`) {
		var m map[string]any
		_ = json.Unmarshal([]byte(cur), &m)
		m["category"] = "secopsctl-smoke"
		b, _ := json.MarshalIndent(m, "", "  ")
		edited = string(b)
	} else {
		t.Skip("seed record has no 'category' field to edit; create path validated")
	}
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); len(plan.Updates()) != 1 {
		t.Fatalf("expected one update, got +%d ~%d", len(plan.Creates()), len(plan.Updates()))
	}
	buf.Reset()
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
		t.Fatalf("push update: %v\n%s", err, buf.String())
	}
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
		t.Fatalf("post-update plan not in sync: +%d ~%d -%d",
			len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}

	// Cleanup via the raw delete (tracking-lists delete takes a body).
	if _, err := lc.RemoveTrackingListRecords(ctx, live.Raw); err != nil {
		t.Fatalf("remove throwaway: %v", err)
	}
	removed = true
	if _, stillThere := findBySlug(ctx, s, slug); stillThere {
		t.Fatalf("throwaway tracking entity %q still present after remove", label)
	}
}

// TestLiveReconcileReadAllSOAR pulls every registered SOAR reconcile surface and
// asserts a clean round-trip (a fresh pull diffs in-sync), validating each
// surface's list shape, identity extraction, redaction, and canonical stability
// against the live tenant. Read-only — runs under SECOPS_SOAR_SMOKE=1 (no writes).
func TestLiveReconcileReadAllSOAR(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	for _, name := range SOARSurfaceNames() {
		t.Run(name, func(t *testing.T) {
			s, ok := BuildSOARSurface(name, lc)
			if !ok {
				t.Fatalf("%s not registered", name)
			}
			dir := t.TempDir()
			n, err := reconcile.Pull(ctx, s, dir, io.Discard)
			if err != nil {
				t.Fatalf("pull: %v", err)
			}
			plan, _, err := reconcile.BuildPlan(ctx, s, dir)
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			if !plan.Empty() {
				t.Errorf("fresh pull of %d object(s) not in sync: +%d ~%d -%d",
					n, len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
			}
			t.Logf("%s: %d object(s), clean round-trip", name, n)
		})
	}
}

// runReconcileCreateUpdateSmoke drives the engine create -> in-sync -> update ->
// in-sync cycle for one throwaway record, then runs cleanup (these surfaces have
// no engine delete, so the caller supplies a raw remove). createBody is the new
// record (no _server); editField/editValue is a benign string change.
func runReconcileCreateUpdateSmoke(t *testing.T, ctx context.Context, s reconcile.Surface, label string, createBody map[string]any, editField, editValue string, cleanup func(reconcile.Object) error) {
	t.Helper()
	slug := Slugify(label)
	dir := t.TempDir()
	path := filepath.Join(dir, slug+".json")
	if _, err := reconcile.Pull(ctx, s, dir, io.Discard); err != nil {
		t.Fatalf("baseline pull: %v", err)
	}
	raw, _ := json.Marshal(createBody)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	cleaned := false
	t.Cleanup(func() {
		if cleaned {
			return
		}
		if o, found := findBySlug(ctx, s, slug); found {
			if err := cleanup(o); err != nil {
				t.Logf("cleanup %q: %v", label, err)
			}
		}
	})

	var buf strings.Builder
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
		t.Fatalf("push create: %v\n%s", err, buf.String())
	}
	live, found := findBySlug(ctx, s, slug)
	if !found {
		t.Fatalf("created %q not found after create. push log:\n%s", label, buf.String())
	}
	if id, _ := serverBlock([]byte(readFile(t, path))); id == "" || id != live.ServerID {
		t.Fatalf("create did not record the server id (file=%q live=%q)", id, live.ServerID)
	}
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
		t.Fatalf("post-create not in sync: +%d ~%d -%d", len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}

	// Edit a benign field -> exactly one update reconciles clean. Skipped when
	// editField is "" (clone-based records with no obvious safe scalar to flip);
	// the create+delete path is still fully exercised, and the update path is
	// proven on other surfaces.
	if editField != "" {
		var m map[string]any
		_ = json.Unmarshal([]byte(readFile(t, path)), &m)
		m[editField] = editValue
		b, _ := json.MarshalIndent(m, "", "  ")
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); len(plan.Updates()) != 1 || len(plan.Creates()) != 0 {
			t.Fatalf("expected exactly one update, got +%d ~%d", len(plan.Creates()), len(plan.Updates()))
		}
		buf.Reset()
		if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
			t.Fatalf("push update: %v\n%s", err, buf.String())
		}
		if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
			t.Fatalf("post-update not in sync: +%d ~%d -%d", len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
		}
	}

	// Cleanup (raw remove) + verify gone.
	if err := cleanup(live); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	cleaned = true
	if _, still := findBySlug(ctx, s, slug); still {
		t.Fatalf("%q still present after cleanup", label)
	}
}

// TestLiveReconcileNetworkWriteSmoke validates the engine write loop on networks:
// a throwaway named network on the RFC 5737 test range (192.0.2.0/24) matches no
// real traffic and is removed afterward. Construct-based (the tenant may have no
// network to clone). Cleanup uses the array-shaped RemoveNetworkDetailsRecords.
func TestLiveReconcileNetworkWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	s, ok := BuildSOARSurface("networks", lc)
	if !ok {
		t.Fatal("networks not registered")
	}
	env := firstEnvironmentName(ctx, lc)
	if env == "" {
		t.Skip("no environment available to scope a throwaway network")
	}
	label := smokeLabel("network")
	body := map[string]any{"name": label, "address": "192.0.2.0/24", "priority": 1, "environments": []any{env}}
	runReconcileCreateUpdateSmoke(t, ctx, s, label, body, "address", "192.0.2.128/25",
		func(o reconcile.Object) error {
			var rec map[string]any
			_ = json.Unmarshal(o.Raw, &rec)
			_, err := lc.RemoveNetworkDetailsRecords(ctx, []any{rec})
			return err
		})
}

// TestLiveReconcileVisualFamilyWriteSmoke validates the engine write loop on
// visual-families — exercising the wrapKey envelope ({visualFamilyDataModel: ...})
// live. A throwaway custom family with no rules is display-only and inert; cleanup
// deletes it by id via DeleteFamilyData.
func TestLiveReconcileVisualFamilyWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	s, ok := BuildSOARSurface("visual-families", lc)
	if !ok {
		t.Fatal("visual-families not registered")
	}
	label := smokeLabel("vfamily")
	body := map[string]any{"family": label, "description": "secopsctl reconcile smoke", "isCustom": true, "rules": []any{}}
	runReconcileCreateUpdateSmoke(t, ctx, s, label, body, "description", "secopsctl reconcile smoke (edited)",
		func(o reconcile.Object) error {
			_, err := lc.DeleteFamilyData(ctx, o.ServerID)
			return err
		})
}

// cloneSeedRecord clones an existing record (full body), strips server-managed
// fields, and renames it to label — a guaranteed-valid create body for surfaces
// where construction is fiddly. Skips the test if there is nothing to clone.
func cloneSeedRecord(t *testing.T, ctx context.Context, s reconcile.Surface, nameField, label string, extraStrip ...string) map[string]any {
	t.Helper()
	seed, ok := findAny(ctx, s)
	if !ok {
		t.Skipf("no existing %s record to clone as a template", s.Name)
	}
	var m map[string]any
	if err := json.Unmarshal(seed.Raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range append([]string{"id", "creationTimeUnixTimeInMs", "modificationTimeUnixTimeInMs"}, extraStrip...) {
		delete(m, k)
	}
	m[nameField] = label
	return m
}

// The four write-safe case/playbook config surfaces (workflow-classified inert +
// self-cleaning), using ground-truth bodies from the existing passing live tests.
// create + delete is exercised (update is proven on other surfaces); cleanup uses
// each surface's body-shaped raw remove.

func TestLiveReconcileCaseTagWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	s, _ := BuildSOARSurface("case-tags", lc)
	label := smokeLabel("tag")
	body := cloneSeedRecord(t, ctx, s, "name", label)
	runReconcileCreateUpdateSmoke(t, ctx, s, label, body, "", "", func(o reconcile.Object) error {
		var m map[string]any
		_ = json.Unmarshal(o.Raw, &m)
		_, err := lc.RemoveTagDefinitionRecords(ctx, m)
		return err
	})
}

func TestLiveReconcileRootCauseWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	s, _ := BuildSOARSurface("close-root-causes", lc)
	label := smokeLabel("rootcause")
	body := cloneSeedRecord(t, ctx, s, "rootCause", label)
	runReconcileCreateUpdateSmoke(t, ctx, s, label, body, "", "", func(o reconcile.Object) error {
		var m map[string]any
		_ = json.Unmarshal(o.Raw, &m)
		_, err := lc.RemoveRootCauseClose(ctx, m)
		return err
	})
}

func TestLiveReconcileBlacklistWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	s, _ := BuildSOARSurface("blacklists", lc)
	label := smokeLabel("block")
	// A throwaway USER block-list entry for a smoke-label user matches no real entity.
	body := map[string]any{"entityIdentifier": label, "entityType": "USER", "elementType": int(legacy.BlockUserUniqName), "scope": int(legacy.BlockScopeForModel), "environments": []any{}}
	runReconcileCreateUpdateSmoke(t, ctx, s, label, body, "", "", func(o reconcile.Object) error {
		var m map[string]any
		_ = json.Unmarshal(o.Raw, &m)
		_, err := lc.RemoveModelBlockRecords(ctx, m)
		return err
	})
}

func TestLiveReconcilePlaybookCategoryWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	s, _ := BuildSOARSurface("playbook-categories", lc)
	label := smokeLabel("pbcat")
	body := cloneSeedRecord(t, ctx, s, "name", label, "isDefaultCategory")
	body["isDefaultCategory"] = false
	runReconcileCreateUpdateSmoke(t, ctx, s, label, body, "", "", func(o reconcile.Object) error {
		id, err := strconv.Atoi(o.ServerID)
		if err != nil {
			return err
		}
		_, err = lc.RemovePlaybookCategories(ctx, map[string]any{"ids": []int{id}})
		return err
	})
}

// TestLiveReconcilePlaybookSaveSemantics verifies the load-bearing assumption of
// the playbook reconcile Update path: that SavePlaybook UPDATES the named playbook
// in place (minting a new uuid) rather than creating a duplicate. It duplicates a
// DISABLED playbook into an inert throwaway, edits it, calls the exact SavePlaybook
// path the engine uses, and asserts exactly one playbook carries the throwaway name.
// All created playbooks are deleted on cleanup (set diff — robust to nesting and to
// the save minting a new uuid).
func TestLiveReconcilePlaybookSaveSemantics(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)

	cards, err := lc.ListPlaybooks(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := map[string]bool{}
	var srcID string
	for _, c := range cards {
		before[c.Identifier] = true
		if srcID == "" && !c.IsEnabled && c.Identifier != "" {
			srcID = c.Identifier
		}
	}
	if srcID == "" {
		t.Skip("no disabled playbook to duplicate as a safe source")
	}

	src, err := lc.GetWorkflowFullInfo(ctx, srcID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	var sm map[string]any
	if err := json.Unmarshal(src, &sm); err != nil {
		t.Fatal(err)
	}
	label := smokeLabel("pbsave") // letters/digits/hyphens only -> valid playbook name
	sm["name"] = label
	sm["isEnabled"] = false
	// Pass the map (not marshaled bytes): a []byte through `body any` would be
	// base64-encoded by json.Marshal.
	if _, err := lc.DuplicateWorkflow(ctx, sm); err != nil {
		t.Fatalf("duplicate workflow: %v", err)
	}

	// Delete every playbook created during the test (anything not in the before set).
	t.Cleanup(func() {
		cs, err := lc.ListPlaybooks(ctx, nil)
		if err != nil {
			return
		}
		for _, c := range cs {
			if c.Identifier == "" || before[c.Identifier] {
				continue
			}
			full, ferr := lc.GetWorkflowFullInfo(ctx, c.Identifier)
			if ferr != nil {
				continue
			}
			var o map[string]any
			if json.Unmarshal(full, &o) == nil {
				if _, derr := lc.DeleteWorkflow(ctx, o); derr != nil {
					t.Logf("cleanup: delete %q: %v", c.Identifier, derr)
				}
			}
		}
	})

	// Edit the duplicate (fetched by NAME) and save it via the engine's SavePlaybook.
	body, err := lc.GetPlaybookByName(ctx, label, false)
	if err != nil {
		t.Fatalf("get duplicate by name: %v", err)
	}
	var pb map[string]any
	_ = json.Unmarshal(body, &pb)
	pb["description"] = "secopsctl save-semantics check"
	edited, _ := json.Marshal(pb)
	if _, err := lc.SavePlaybook(ctx, edited); err != nil {
		t.Fatalf("SavePlaybook: %v", err)
	}

	// THE VERIFICATION: update-by-name, not duplicate.
	cs, err := lc.ListPlaybooks(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range cs {
		if c.Name == label {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("SavePlaybook produced %d playbooks named %q (want 1): it DUPLICATES rather than updating by name — the playbook reconcile Update path would multiply playbooks; do not push playbooks until resolved", n, label)
	}
	got, err := lc.GetPlaybookByName(ctx, label, false)
	if err != nil {
		t.Fatal(err)
	}
	var gm map[string]any
	_ = json.Unmarshal(got, &gm)
	if gm["description"] != "secopsctl save-semantics check" {
		t.Errorf("edit not applied after save: description=%v", gm["description"])
	}
}

// boolField reports the boolean value of key in an object's raw body.
func boolField(o reconcile.Object, key string) bool {
	var m map[string]any
	if json.Unmarshal(o.Raw, &m) != nil {
		return false
	}
	b, _ := m[key].(bool)
	return b
}

// editFileField overlays one field on an on-disk object file (preserving the
// _server block) so the engine plans exactly one update.
func editFileField(t *testing.T, path, field string, value any) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(readFile(t, path)), &m); err != nil {
		t.Fatal(err)
	}
	m[field] = value
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLiveReconcileConnectorWriteSmoke validates the connectors engine full CUD on a
// THROWAWAY, never touching a real connector. It creates a DISABLED connector from a
// template — the create path triggers by OMITTING the identifier (the server assigns
// one; sending a client id routes to the update path and 404s) with the mandatory
// params filled with placeholders — then runs an engine update and an engine delete.
// It iterates templates until one creates cleanly, so it stays tenant-neutral. The
// connector is always DISABLED (never ingests) and self-cleaning even on failure.
func TestLiveReconcileConnectorWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	s, ok := BuildSOARSurface("connectors", lc)
	if !ok {
		t.Fatal("connectors not registered")
	}
	env := firstEnvironmentName(ctx, lc)
	if env == "" {
		t.Skip("no environment to scope a throwaway connector")
	}
	raw, err := lc.ListConnectorTemplateCards(ctx)
	if err != nil {
		t.Fatalf("list connector templates: %v", err)
	}
	templates, err := decodeRawList(raw)
	if err != nil {
		t.Fatalf("decode connector templates: %v", err)
	}
	if len(templates) == 0 {
		t.Skip("no connector templates to instantiate")
	}

	label := smokeLabel("connector")
	slug := Slugify(label)
	dir := t.TempDir()
	if _, err := reconcile.Pull(ctx, s, dir, io.Discard); err != nil {
		t.Fatalf("baseline pull: %v", err)
	}
	path := filepath.Join(dir, slug+".json")

	cleaned := false
	t.Cleanup(func() {
		if cleaned {
			return
		}
		if o, ok := findBySlug(ctx, s, slug); ok {
			if _, err := lc.DeleteConnector(ctx, o.ServerID); err != nil {
				t.Logf("cleanup: delete throwaway connector %q: %v", label, err)
			}
		}
	})

	// Create a DISABLED throwaway from the first template that the engine creates
	// cleanly (identifier omitted -> server-assigned create path; mandatory params
	// filled with placeholders).
	created := false
	var buf strings.Builder
	for i, tc := range templates {
		if i >= 10 {
			break
		}
		integ, def := jsonField(tc, "integration"), jsonField(tc, "connectorDefinitionName")
		if integ == "" || def == "" {
			continue
		}
		tplRaw, terr := lc.GetConnectorTemplate(ctx, map[string]any{"integration": integ, "connectorDefinitionName": def})
		if terr != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(tplRaw, &m) != nil {
			continue
		}
		delete(m, "identifier") // omit -> create path (server assigns the id)
		m["displayName"] = label
		m["environment"] = env
		m["isEnabled"] = false
		fillConnectorParams(m)
		b, _ := json.Marshal(m)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		buf.Reset()
		if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
			t.Fatalf("push create: %v\n%s", err, buf.String())
		}
		if _, ok := findBySlug(ctx, s, slug); ok {
			created = true
			break
		}
	}
	if !created {
		t.Skip("no connector template produced a clean engine create with placeholder params")
	}

	live, _ := findBySlug(ctx, s, slug)
	if boolField(live, "isEnabled") {
		_, _ = lc.DeleteConnector(ctx, live.ServerID)
		t.Fatal("throwaway connector came up ENABLED; deleted it and failing")
	}
	if id, _ := serverBlock([]byte(readFile(t, path))); id == "" || id != live.ServerID {
		t.Fatalf("create did not record server id (file=%q live=%q)", id, live.ServerID)
	}
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
		t.Fatalf("post-create not in sync: +%d ~%d -%d (volatile field needs extraStrip?)",
			len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}

	// Engine update: benign edit -> exactly one update -> in-sync.
	editFileField(t, path, "description", "secopsctl reconcile smoke (edited)")
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); len(plan.Updates()) != 1 || len(plan.Creates()) != 0 {
		t.Fatalf("expected exactly one update, got +%d ~%d", len(plan.Creates()), len(plan.Updates()))
	}
	buf.Reset()
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
		t.Fatalf("push update: %v\n%s", err, buf.String())
	}
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
		t.Fatalf("post-update not in sync: +%d ~%d -%d", len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}

	// Engine delete by id -> gone.
	if err := s.Delete(ctx, live); err != nil {
		t.Fatalf("engine delete: %v", err)
	}
	cleaned = true
	if _, still := findBySlug(ctx, s, slug); still {
		t.Fatalf("throwaway connector %q still present after delete", label)
	}
}

// fillConnectorParams gives every empty-valued connector parameter a placeholder so a
// DISABLED throwaway passes the server's mandatory-field validation (it never runs).
func fillConnectorParams(m map[string]any) {
	ps, _ := m["parameters"].([]any)
	for _, p := range ps {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		if v, _ := pm["value"].(string); v != "" {
			continue
		}
		name, _ := pm["name"].(string)
		switch {
		case strings.Contains(strings.ToLower(name), "json"):
			pm["value"] = "{}"
		case strings.Contains(strings.ToLower(name), "email"):
			pm["value"] = "noreply@example.com"
		default:
			pm["value"] = "secopsctl-smoke-placeholder"
		}
	}
}

// TestLiveReconcileJobWriteSmoke validates the jobs engine update path on a
// THROWAWAY, never touching a real job. The engine has no job create/delete
// (create needs a trimmed body; delete takes a body), so the throwaway is created
// from a job template (DISABLED → never scheduled) and removed via the raw SDK; the
// ENGINE update is exercised in between. Self-cleaning even on failure.
func TestLiveReconcileJobWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	s, ok := BuildSOARSurface("jobs", lc)
	if !ok {
		t.Fatal("jobs not registered")
	}
	raw, err := lc.ListJobTemplates(ctx)
	if err != nil {
		t.Fatalf("list job templates: %v", err)
	}
	templates, err := decodeRawList(raw)
	if err != nil {
		t.Fatalf("decode job templates: %v", err)
	}
	if len(templates) == 0 {
		t.Skip("no job templates to clone")
	}
	// Prefer the template with the fewest parameters (simplest valid create body).
	src := templates[0]
	for _, tpl := range templates {
		if paramCount(tpl) < paramCount(src) {
			src = tpl
		}
	}
	var sm map[string]any
	if err := json.Unmarshal(src, &sm); err != nil {
		t.Fatal(err)
	}

	label := smokeLabel("job")
	slug := Slugify(label)
	// Minimal create body: definition-identity + scheduling only. Echoing the
	// template's read-only/audit fields (id/version/creationTime/lastRun*) is rejected.
	body := map[string]any{
		"jobDefinitionId":      sm["jobDefinitionId"],
		"jobDefinitionName":    sm["jobDefinitionName"],
		"integration":          sm["integration"],
		"script":               sm["script"],
		"description":          "secopsctl reconcile smoke (disabled)",
		"parameters":           orEmptyArray(sm["parameters"]),
		"name":                 label,
		"isCustom":             true,
		"isEnabled":            false,
		"runIntervalInSeconds": 86400,
	}

	cleaned := false
	t.Cleanup(func() {
		if cleaned {
			return
		}
		if full, ok := findJobByName(ctx, lc, label); ok {
			if _, err := lc.DeleteJobData(ctx, full); err != nil {
				t.Logf("cleanup: delete throwaway job %q: %v", label, err)
			}
		}
	})
	if _, err := lc.SaveOrUpdateJob(ctx, body); err != nil {
		t.Fatalf("create throwaway job: %v", err)
	}

	dir := t.TempDir()
	if _, err := reconcile.Pull(ctx, s, dir, io.Discard); err != nil {
		t.Fatalf("pull after create: %v", err)
	}
	live, found := findBySlug(ctx, s, slug)
	if !found {
		t.Fatalf("throwaway job %q not found after create", label)
	}
	if boolField(live, "isEnabled") {
		t.Fatal("throwaway job is ENABLED; expected disabled (inert)")
	}
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
		t.Fatalf("post-create plan not in sync: +%d ~%d -%d (volatile field needs extraStrip?)",
			len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}

	// ENGINE update: benign edit -> one update -> in-sync.
	path := filepath.Join(dir, slug+".json")
	editFileField(t, path, "description", "secopsctl reconcile smoke (edited)")
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); len(plan.Updates()) != 1 {
		t.Fatalf("expected one update, got +%d ~%d", len(plan.Creates()), len(plan.Updates()))
	}
	var buf strings.Builder
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
		t.Fatalf("push update: %v\n%s", err, buf.String())
	}
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
		t.Fatalf("post-update plan not in sync: +%d ~%d -%d",
			len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}

	// Cleanup via the raw delete (jobs delete takes a body) + verify gone.
	full, ok := findJobByName(ctx, lc, label)
	if !ok {
		t.Fatalf("throwaway job %q not found for delete", label)
	}
	if _, err := lc.DeleteJobData(ctx, full); err != nil {
		t.Fatalf("delete throwaway job: %v", err)
	}
	cleaned = true
	if _, still := findBySlug(ctx, s, slug); still {
		t.Fatalf("throwaway job %q still present after delete", label)
	}
}

// paramCount returns the length of a record's "parameters" array.
func paramCount(raw json.RawMessage) int {
	var m struct {
		Parameters []json.RawMessage `json:"parameters"`
	}
	_ = json.Unmarshal(raw, &m)
	return len(m.Parameters)
}

// orEmptyArray returns v when it is a non-nil array, else an empty array (the
// legacy server NPEs on a null collection where it wants []).
func orEmptyArray(v any) any {
	if a, ok := v.([]any); ok && a != nil {
		return a
	}
	return []any{}
}

// findJobByName returns the full installed-job object (as a map, so it marshals as a
// JSON object — a json.RawMessage through `body any` would be base64-encoded) whose
// name matches, for the body-shaped DeleteJobData.
func findJobByName(ctx context.Context, lc *legacy.Client, name string) (map[string]any, bool) {
	raw, err := lc.ListInstalledJobs(ctx)
	if err != nil {
		return nil, false
	}
	items, err := decodeRawList(raw)
	if err != nil {
		return nil, false
	}
	for _, it := range items {
		if jsonField(it, "name") == name {
			var m map[string]any
			if json.Unmarshal(it, &m) == nil {
				return m, true
			}
		}
	}
	return nil, false
}

// newUUIDv4 generates a random v4 UUID (a connector instance is keyed by a
// client-assigned identifier).
func newUUIDv4(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("uuid: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// findByServerID returns the live object with the given ServerID (robust to a
// name/slug edit, unlike findBySlug).
func findByServerID(ctx context.Context, s reconcile.Surface, id string) (reconcile.Object, bool) {
	if id == "" {
		return reconcile.Object{}, false
	}
	res, err := s.List(ctx)
	if err != nil {
		return reconcile.Object{}, false
	}
	for _, o := range res.Objects {
		if o.ServerID == id {
			return o, true
		}
	}
	return reconcile.Object{}, false
}

// runReconcileCloneLifecycle drives engine create -> (optional) update of one field
// -> raw delete on a throwaway built from `body`. It tracks the object by ServerID
// (so the update may change the name/slug) and removes it via `del`, with a t.Cleanup
// safety-net. editValue may be any JSON type (string, number, bool); editField ""
// skips the update leg.
func runReconcileCloneLifecycle(t *testing.T, ctx context.Context, s reconcile.Surface, label string, body map[string]any, editField string, editValue any, del func(reconcile.Object) error) {
	t.Helper()
	slug := Slugify(label)
	dir := t.TempDir()
	if _, err := reconcile.Pull(ctx, s, dir, io.Discard); err != nil {
		t.Fatalf("baseline pull: %v", err)
	}
	path := filepath.Join(dir, slug+".json")
	raw, _ := json.Marshal(body)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	createdID := ""
	cleaned := false
	t.Cleanup(func() {
		if cleaned || createdID == "" {
			return
		}
		if o, ok := findByServerID(ctx, s, createdID); ok {
			if err := del(o); err != nil {
				t.Logf("cleanup %s %q: %v", s.Name, label, err)
			}
		}
	})

	var buf strings.Builder
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
		t.Fatalf("push create: %v\n%s", err, buf.String())
	}
	live, ok := findBySlug(ctx, s, slug)
	if !ok {
		t.Fatalf("created %s %q not found. push log:\n%s", s.Name, label, buf.String())
	}
	createdID = live.ServerID
	if id, _ := serverBlock([]byte(readFile(t, path))); id == "" || id != live.ServerID {
		t.Fatalf("create did not record server id (file=%q live=%q)", id, live.ServerID)
	}
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
		t.Fatalf("post-create %s not in sync: +%d ~%d -%d", s.Name, len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}

	if editField != "" {
		editFileField(t, path, editField, editValue)
		if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); len(plan.Updates()) != 1 || len(plan.Creates()) != 0 {
			t.Fatalf("%s: expected exactly one update, got +%d ~%d", s.Name, len(plan.Creates()), len(plan.Updates()))
		}
		buf.Reset()
		if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
			t.Fatalf("push update: %v\n%s", err, buf.String())
		}
		if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
			t.Fatalf("post-update %s not in sync: +%d ~%d -%d", s.Name, len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
		}
	}

	live2, ok := findByServerID(ctx, s, createdID)
	if !ok {
		t.Fatalf("%s %q gone before delete", s.Name, label)
	}
	if err := del(live2); err != nil {
		t.Fatalf("delete %s: %v", s.Name, err)
	}
	cleaned = true
	if _, still := findByServerID(ctx, s, createdID); still {
		t.Fatalf("%s %q still present after delete", s.Name, label)
	}
}

// delByBody returns a del closure that removes an object via a body-shaped legacy
// remover (the object is sent as a JSON object, not base64-encoded bytes).
func delByBody(ctx context.Context, remove func(context.Context, any) (legacy.RawJSON, error)) func(reconcile.Object) error {
	return func(o reconcile.Object) error {
		var m map[string]any
		if err := json.Unmarshal(o.Raw, &m); err != nil {
			return err
		}
		_, err := remove(ctx, m)
		return err
	}
}

// TestLiveReconcileConnectorDuplicateDeleteSmoke attempts to create a connector by
// DUPLICATING an existing one (full GetConnector body → new UUID + label, DISABLED,
// isNew) and then exercising the engine delete-by-id. On this tenant SaveConnector is
// update-only (404 for a new id), so this is expected to skip cleanly; where create
// IS supported it validates connector create + delete end-to-end. The duplicate is
// always DISABLED (never ingests) and deleted on cleanup.
func TestLiveReconcileConnectorDuplicateDeleteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	s, ok := BuildSOARSurface("connectors", lc)
	if !ok {
		t.Fatal("connectors not registered")
	}
	res, err := s.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(res.Objects) == 0 {
		t.Skip("no connector to duplicate")
	}
	var seed map[string]any
	if err := json.Unmarshal(res.Objects[0].Raw, &seed); err != nil {
		t.Fatal(err)
	}

	label := smokeLabel("connector")
	newID := newUUIDv4(t)
	seed["identifier"] = newID
	seed["displayName"] = label
	seed["isEnabled"] = false
	seed["isNew"] = true

	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		if _, err := lc.DeleteConnector(ctx, newID); err != nil {
			t.Logf("cleanup: delete duplicate connector %q (%s): %v", label, newID, err)
		}
	})
	if _, err := lc.SaveConnector(ctx, seed); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			t.Skipf("connector create unsupported on this tenant (SaveConnector is update-only): %v", err)
		}
		t.Fatalf("duplicate connector: %v", err)
	}

	live, found := findBySlug(ctx, s, Slugify(label))
	if !found {
		t.Fatalf("duplicate connector %q not found after create", label)
	}
	if boolField(live, "isEnabled") {
		// Must never leave a duplicate ingesting; delete immediately.
		_, _ = lc.DeleteConnector(ctx, live.ServerID)
		t.Fatal("duplicate connector came up ENABLED; deleted it and failing")
	}
	// Engine delete-by-id.
	if err := s.Delete(ctx, live); err != nil {
		t.Fatalf("engine delete duplicate connector: %v", err)
	}
	deleted = true
	if _, still := findBySlug(ctx, s, Slugify(label)); still {
		t.Fatalf("duplicate connector %q still present after delete", label)
	}
}

// TestLiveReconcileNetworkDeleteByIDSmoke validates the by-id DeleteNetwork path
// (the prune candidate) on an RFC 5737 throwaway: engine create → DeleteNetwork(id)
// → verify gone. Confirms the record id == the DELETE path identifier; if it passes,
// networks can flip to PruneEligible.
func TestLiveReconcileNetworkDeleteByIDSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	s, ok := BuildSOARSurface("networks", lc)
	if !ok {
		t.Fatal("networks not registered")
	}
	env := firstEnvironmentName(ctx, lc)
	if env == "" {
		t.Skip("no environment to scope a throwaway network")
	}
	label := smokeLabel("network")
	slug := Slugify(label)
	dir := t.TempDir()
	if _, err := reconcile.Pull(ctx, s, dir, io.Discard); err != nil {
		t.Fatalf("baseline pull: %v", err)
	}
	body, _ := json.Marshal(map[string]any{"name": label, "address": "192.0.2.0/24", "priority": 1, "environments": []any{env}})
	if err := os.WriteFile(filepath.Join(dir, slug+".json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	removed := false
	t.Cleanup(func() {
		if removed {
			return
		}
		if o, ok := findBySlug(ctx, s, slug); ok {
			var m map[string]any
			_ = json.Unmarshal(o.Raw, &m)
			_, _ = lc.RemoveNetworkDetailsRecords(ctx, []any{m})
		}
	})
	var buf strings.Builder
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
		t.Fatalf("push create: %v\n%s", err, buf.String())
	}
	live, ok := findBySlug(ctx, s, slug)
	if !ok {
		t.Fatalf("throwaway network %q not found after create", label)
	}
	// THE TEST: delete by the record's id via DeleteNetwork.
	if _, err := lc.DeleteNetwork(ctx, live.ServerID); err != nil {
		t.Fatalf("DeleteNetwork(%q): %v (record id may not equal the DELETE path identifier)", live.ServerID, err)
	}
	removed = true
	if _, still := findBySlug(ctx, s, slug); still {
		t.Fatalf("network %q still present after DeleteNetwork by id", label)
	}
}

// TestLiveReconcileSocRoleWriteSmoke validates the soc-roles engine create/update +
// delete on a throwaway role (cloned, no users assigned → inert). RBAC surface kept
// read-only-by-choice operationally; this validates the write path only. Self-cleaning.
func TestLiveReconcileSocRoleWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	s, _ := BuildSOARSurface("soc-roles", lc)
	label := smokeLabel("socrole")
	body := cloneSeedRecord(t, ctx, s, "name", label, "usersAssigned", "isDefault")
	body["isDefault"] = false
	// DeleteSocRole takes {socRoleId:int}, not the full record.
	del := func(o reconcile.Object) error {
		id, err := strconv.Atoi(o.ServerID)
		if err != nil {
			return err
		}
		_, err = lc.SocRoleDelete(ctx, map[string]any{"socRoleId": id})
		return err
	}
	runReconcileCloneLifecycle(t, ctx, s, label, body, "name", label+"-edited", del)
}

// TestLiveReconcileIdpWriteSmoke validates the idp engine create/update + by-id
// delete on a throwaway mapping for a FAKE group (nobody is a member → no real users
// affected). SSO surface kept read-only-by-choice operationally. Self-cleaning.
func TestLiveReconcileIdpWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	s, _ := BuildSOARSurface("idp", lc)
	label := smokeLabel("idpgroup")
	body := cloneSeedRecord(t, ctx, s, "idpGroup", label, "groupMembers", "isSystem", "isDefault", "workforcePoolId")
	body["isDefault"] = false
	body["isSystem"] = false
	del := func(o reconcile.Object) error { _, err := lc.DeleteIdpGroupMapping(ctx, o.ServerID); return err }
	runReconcileCloneLifecycle(t, ctx, s, label, body, "idpGroup", label+"-edited", del)
}

// TestLiveReconcileCaseStageWriteSmoke validates the case-stages engine create/update
// + delete on a throwaway stage (used by no case). Update flips the numeric `order`.
// Self-cleaning.
func TestLiveReconcileCaseStageWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	s, _ := BuildSOARSurface("case-stages", lc)
	label := smokeLabel("stage")
	body := cloneSeedRecord(t, ctx, s, "name", label)
	runReconcileCloneLifecycle(t, ctx, s, label, body, "order", 999, delByBody(ctx, lc.RemoveCaseStageDefinitionRecords))
}

// TestLiveReconcileSlaWriteSmoke validates the sla-definitions engine create/update +
// delete on a throwaway "Case Priority = High" SLA. The legacy ApiSlaDefinition uses
// integer enums documented in the swagger schema descriptions: valueType
// (ApiSlaProviderTypeEnum) 2=AlertRuleGenerator, 3=CaseStage, 4=CasePriority,
// 5=AlertPriority; slaPeriodType/criticalPeriodType (ApiPeriodTypeEnum) 0=Minutes,
// 1=Hours, 2=Days, 3=Seconds; alertType (ApiSlaAlertType) 0=AllAlerts, 1=SpecificAlerts.
// The SLA's identity (nameField) is its `value` ("High"), so the slug is "high"; it is
// created then deleted within the test (the tenant otherwise has none), so the
// routing-surface window is seconds. Self-cleaning.
func TestLiveReconcileSlaWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)
	s, ok := BuildSOARSurface("sla-definitions", lc)
	if !ok {
		t.Fatal("sla-definitions not registered")
	}
	env := firstEnvironmentName(ctx, lc)
	if env == "" {
		t.Skip("no environment to scope a throwaway SLA")
	}
	// For CasePriority the server normalizes `value` to a JSON-array string
	// (`["High"]`, matching the v1alpha slaTypeValue form), so send it that way for a
	// clean round-trip; `values` is the plain array.
	value := `["High"]`
	body := map[string]any{
		"valueType": int(legacy.SlaCasePriority), "value": value, "values": []any{"High"},
		"slaPeriod": 24, "slaPeriodType": int(legacy.SlaHours),
		"criticalPeriod": 23, "criticalPeriodType": int(legacy.SlaHours),
		"alertType": int(legacy.SlaAllAlerts), "environments": []any{env},
	}
	runReconcileCloneLifecycle(t, ctx, s, value, body, "criticalPeriod", 22, delByBody(ctx, lc.RemoveSlaDefinitionRecords))
}

// findAny returns any one live object from a surface (a clone template).
func findAny(ctx context.Context, s reconcile.Surface) (reconcile.Object, bool) {
	res, err := s.List(ctx)
	if err != nil || len(res.Objects) == 0 {
		return reconcile.Object{}, false
	}
	return res.Objects[0], true
}

// firstSocRoleName returns any live SOC role name, for the "@RoleName" form
// CreateManualCase requires in assignedUser.
func firstSocRoleName(ctx context.Context, lc *legacy.Client) string {
	s, ok := BuildSOARSurface("soc-roles", lc)
	if !ok {
		return ""
	}
	res, err := s.List(ctx)
	if err != nil {
		return ""
	}
	for _, o := range res.Objects {
		var m map[string]any
		if json.Unmarshal(o.Raw, &m) == nil {
			if n, _ := m["name"].(string); n != "" {
				return n
			}
		}
	}
	return ""
}

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
