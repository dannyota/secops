package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// findAny returns any one live object from a surface (a clone template).
func findAny(ctx context.Context, s reconcile.Surface) (reconcile.Object, bool) {
	res, err := s.List(ctx)
	if err != nil || len(res.Objects) == 0 {
		return reconcile.Object{}, false
	}
	return res.Objects[0], true
}
