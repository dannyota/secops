package mirror

import (
	"context"
	"fmt"
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
