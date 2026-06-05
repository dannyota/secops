// Shared foundation for the LIVE SOAR external-API tests.
//
// Two opt-in gates keep these out of normal/CI runs and keep mutations safe:
//
//   - SECOPS_SOAR_SMOKE=1        enables read smoke tests (safe; read-only).
//   - SECOPS_SOAR_SMOKE_WRITE=1  additionally enables CRUD/lifecycle tests
//     (they create/edit/delete throwaway resources).
//
// A read-only run (only SECOPS_SOAR_SMOKE=1) exercises every read test and
// AUTO-SKIPS every CRUD test, because each CRUD test calls requireWrite. So the
// CRUD flows can be committed and reviewed without ever running by accident.
//
// All of this is _test.go-only: it ships no runtime code and needs live config
// (config.Load) + a SOAR AppKey to do anything; otherwise every test skips.
package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"danny.vn/secops/auth"
	"danny.vn/secops/config"
)

const (
	envSmoke = "SECOPS_SOAR_SMOKE"       // "1" -> run read smoke tests
	envWrite = "SECOPS_SOAR_SMOKE_WRITE" // "1" -> also run CRUD/write tests
)

// liveClient builds a live SOAR client from the resolved instance config + SOAR
// AppKey, or skips the test when the gate/config/key is absent.
func liveClient(t *testing.T) (*Client, context.Context) {
	t.Helper()
	if os.Getenv(envSmoke) != "1" {
		t.Skipf("live SOAR smoke test — set %s=1 (with instance config + SOAR AppKey) to run", envSmoke)
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
	lc := NewClient(Settings{
		BaseURL:       inst.SOARURL,
		ProjectNumber: inst.ProjectNumberString(),
		Region:        inst.Region,
		CustomerID:    inst.CustomerID,
		ForceIPv4:     inst.ForceIPv4,
	}, auth.SOARAppKey(key), nil)
	return lc, context.Background()
}

// requireWrite gates a test behind the SECOND opt-in so live mutations never run
// under a read-only smoke run. Every CRUD/lifecycle test must call this first.
func requireWrite(t *testing.T) {
	t.Helper()
	if os.Getenv(envWrite) != "1" {
		t.Skipf("live SOAR WRITE/CRUD test — set %s=1 to run (it mutates the tenant)", envWrite)
	}
}

// readProbe runs one read method, fails NON-fatally on error (so one bad
// endpoint doesn't mask the rest), and logs the response shape.
func readProbe(t *testing.T, name string, fn func() (RawJSON, error)) RawJSON {
	t.Helper()
	raw, err := fn()
	if err != nil {
		t.Errorf("%-46s ERR  %v", name, err)
		return nil
	}
	t.Logf("%-46s OK   %s", name, shapeOf(raw))
	return raw
}

// shapeOf summarizes a response without dumping tenant data.
func shapeOf(raw RawJSON) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return fmt.Sprintf("%d bytes", len(raw))
	}
	switch x := v.(type) {
	case []any:
		return fmt.Sprintf("array len=%d", len(x))
	case map[string]any:
		return fmt.Sprintf("object (%d keys)", len(x))
	default:
		return fmt.Sprintf("%v", x)
	}
}

// smokeLabel returns a unique, clearly-marked name for a throwaway test resource.
func smokeLabel(kind string) string {
	return fmt.Sprintf("secopsctl-smoketest-%s-%d", kind, time.Now().UnixNano())
}

// objects decodes a list response into []map[string]any. It accepts either a
// bare JSON array or an object whose first array-typed field holds the records
// (e.g. {"records":[...], "count":N}).
func objects(t *testing.T, label string, raw RawJSON, err error) []map[string]any {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) == nil {
		return arr
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		for _, v := range obj {
			var inner []map[string]any
			if json.Unmarshal(v, &inner) == nil {
				return inner
			}
		}
	}
	t.Fatalf("%s: response is neither an array nor an object wrapping one (%s)", label, shapeOf(raw))
	return nil
}

// intField / strField / setField build field accessors for a list object.
func intField(key string) func(map[string]any) (int, bool) {
	return func(m map[string]any) (int, bool) {
		switch n := m[key].(type) {
		case float64:
			return int(n), true
		case int:
			return n, true
		case json.Number:
			i, _ := n.Int64()
			return int(i), true
		}
		return 0, false
	}
}

func strField(key string) func(map[string]any) string {
	return func(m map[string]any) string { s, _ := m[key].(string); return s }
}

// lenField returns the length of an array-valued field, or 0 if absent/non-array.
func lenField(key string) func(map[string]any) int {
	return func(m map[string]any) int {
		if a, ok := m[key].([]any); ok {
			return len(a)
		}
		return 0
	}
}

func setField(key string) func(map[string]any, string) {
	return func(m map[string]any, v string) { m[key] = v }
}

func cloneObj(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func findBy(objs []map[string]any, pred func(map[string]any) bool) map[string]any {
	for _, o := range objs {
		if pred(o) {
			return o
		}
	}
	return nil
}

// lifecycleSpec wires a single resource into runLifecycle via closures, so the
// per-resource specifics (method names, id/name fields, delete body shape) live
// in the test while the chain logic stays shared.
type lifecycleSpec struct {
	kind     string                                 // label, e.g. "playbook-category"
	list     func(context.Context) (RawJSON, error) // returns the resource list
	idOf     func(map[string]any) (int, bool)       // integer id of a list object
	nameOf   func(map[string]any) string            // display name of a list object
	rename   func(map[string]any, string)           // set the display name on an object
	template func() map[string]any                  // optional: build the create body from scratch (use when the list may be empty / fields are known)
	prep     func(map[string]any)                   // optional: adjust a cloned template before create (default: strip id + timestamps). Ignored when template is set.
	create   func(context.Context, map[string]any) (RawJSON, error)
	update   func(context.Context, map[string]any) (RawJSON, error)
	remove   func(context.Context, map[string]any) (RawJSON, error)
}

// runLifecycle exercises list -> create -> list -> read -> edit -> read ->
// delete -> list against a LIVE tenant for one throwaway resource. The create
// body is a clone of an existing object (so it carries the fields the server
// expects), renamed to a unique smoke label; cleanup is registered via
// t.Cleanup. Gated by requireWrite, so it never runs under a read-only smoke run.
func runLifecycle(t *testing.T, ctx context.Context, s lifecycleSpec) {
	t.Helper()
	requireWrite(t)

	label := smokeLabel(s.kind)

	// 1. list (baseline)
	raw, err := s.list(ctx)
	base := objects(t, s.kind+" list#0", raw, err)
	if findBy(base, func(o map[string]any) bool { return s.nameOf(o) == label }) != nil {
		t.Fatalf("smoke label %q unexpectedly already exists", label)
	}

	// 2. create — from an explicit template when provided (works on an empty
	//    tenant), else clone an existing object (skip when there's none to clone).
	var tmpl map[string]any
	if s.template != nil {
		tmpl = s.template()
	} else {
		if len(base) == 0 {
			t.Skipf("no existing %s to clone as a create template; skipping lifecycle", s.kind)
		}
		tmpl = cloneObj(base[0])
		if s.prep != nil {
			s.prep(tmpl)
		} else {
			delete(tmpl, "id")
			delete(tmpl, "creationTimeUnixTimeInMs")
			delete(tmpl, "modificationTimeUnixTimeInMs")
		}
	}
	s.rename(tmpl, label)
	if _, err := s.create(ctx, tmpl); err != nil {
		t.Fatalf("create %s: %v", s.kind, err)
	}

	// Register cleanup immediately so a later failure still deletes the resource.
	// cleanup holds the full CURRENT object (so delete works even when it needs
	// more than an id); nil means there is nothing to clean up.
	var cleanup map[string]any
	t.Cleanup(func() {
		if cleanup == nil {
			return
		}
		if _, err := s.remove(ctx, cleanup); err != nil {
			t.Logf("cleanup: could not delete throwaway %s %q: %v", s.kind, label, err)
		}
	})

	// 3. list -> find created, capture id
	raw, err = s.list(ctx)
	after := objects(t, s.kind+" list#1", raw, err)
	created := findBy(after, func(o map[string]any) bool { return s.nameOf(o) == label })
	if created == nil {
		t.Fatalf("created %s %q not found after create", s.kind, label)
	}
	id, ok := s.idOf(created)
	if !ok {
		t.Fatalf("created %s has no integer id", s.kind)
	}
	cleanup = cloneObj(created)

	// 4. read (find by id)
	if got := findBy(after, byID(s, id)); got == nil || s.nameOf(got) != label {
		t.Fatalf("read#1: %s id=%d not readable or name mismatch", s.kind, id)
	}

	// 5. edit (rename the created object)
	edited := cloneObj(created)
	s.rename(edited, label+"-edited")
	if _, err := s.update(ctx, edited); err != nil {
		t.Fatalf("update %s: %v", s.kind, err)
	}
	cleanup = cloneObj(edited)

	// 6. read (verify the edit)
	raw, err = s.list(ctx)
	after2 := objects(t, s.kind+" list#2", raw, err)
	if got := findBy(after2, byID(s, id)); got == nil || s.nameOf(got) != label+"-edited" {
		t.Fatalf("read#2: edit not reflected for %s id=%d", s.kind, id)
	}

	// 7. delete
	if _, err := s.remove(ctx, edited); err != nil {
		t.Fatalf("delete %s: %v", s.kind, err)
	}
	cleanup = nil // deleted; cancel the cleanup delete

	// 8. list -> gone
	raw, err = s.list(ctx)
	after3 := objects(t, s.kind+" list#3", raw, err)
	if got := findBy(after3, byID(s, id)); got != nil {
		t.Fatalf("%s id=%d still present after delete", s.kind, id)
	}
}

func byID(s lifecycleSpec, id int) func(map[string]any) bool {
	return func(o map[string]any) bool { i, _ := s.idOf(o); return i == id }
}
