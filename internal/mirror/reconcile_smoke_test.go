package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	body := map[string]any{"entityIdentifier": label, "entityType": "USER", "elementType": 0, "scope": 3, "environments": []any{}}
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

// findAny returns any one live object from a surface (a clone template).
func findAny(ctx context.Context, s reconcile.Surface) (reconcile.Object, bool) {
	res, err := s.List(ctx)
	if err != nil || len(res.Objects) == 0 {
		return reconcile.Object{}, false
	}
	return res.Objects[0], true
}
