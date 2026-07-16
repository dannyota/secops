package mirror

import (
	"context"
	"crypto/rand"
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
