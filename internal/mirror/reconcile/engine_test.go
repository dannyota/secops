package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeServer is an in-memory stand-in for a per-object-CUD API: id -> raw body
// (the body carries its own "id" and "name"). It backs a Surface so the engine
// can be exercised with no network.
type fakeServer struct {
	objects    map[string]json.RawMessage
	seq        int
	incomplete bool
}

func newFakeServer() *fakeServer { return &fakeServer{objects: map[string]json.RawMessage{}} }

func (s *fakeServer) put(id, name string, extra map[string]any) {
	m := map[string]any{"id": id, "name": name}
	maps.Copy(m, extra)
	b, _ := json.Marshal(m)
	s.objects[id] = b
}

func nameField(raw json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	s, _ := m["name"].(string)
	return s
}

func idField(raw json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	switch v := m["id"].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	}
	return ""
}

func fakeSurface(srv *fakeServer, caps Capabilities) Surface {
	return Surface{
		Name: "fake", Dir: "fake", Caps: caps,
		List: func(_ context.Context) (ListResult, error) {
			var objs []Object
			for id, raw := range srv.objects {
				c, err := Canonicalize(raw)
				if err != nil {
					return ListResult{}, err
				}
				objs = append(objs, Object{Slug: nameField(raw), ServerID: id, Canonical: c, Raw: raw})
			}
			return ListResult{Objects: objs, Incomplete: srv.incomplete}, nil
		},
		LoadDir: func(dir string) ([]Object, error) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				if os.IsNotExist(err) {
					return nil, nil
				}
				return nil, err
			}
			var objs []Object
			for _, e := range entries {
				if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				b, err := os.ReadFile(filepath.Join(dir, e.Name()))
				if err != nil {
					return nil, err
				}
				c, err := Canonicalize(b)
				if err != nil {
					return nil, err
				}
				objs = append(objs, Object{
					Slug:      strings.TrimSuffix(e.Name(), ".json"),
					ServerID:  idField(b),
					Canonical: c,
				})
			}
			return objs, nil
		},
		Write: func(dir string, o Object) error {
			body := o.Raw
			if body == nil {
				body = o.Canonical
			}
			return os.WriteFile(filepath.Join(dir, o.Slug+".json"), body, 0o644)
		},
		Create: func(_ context.Context, local Object) (Object, error) {
			srv.seq++
			id := fmt.Sprintf("created-%d", srv.seq)
			var m map[string]any
			_ = json.Unmarshal(local.Canonical, &m)
			m["id"] = id
			raw, _ := json.Marshal(m)
			srv.objects[id] = raw
			c, _ := Canonicalize(raw)
			return Object{Slug: local.Slug, ServerID: id, Canonical: c, Raw: raw}, nil
		},
		Update: func(_ context.Context, local, live Object) (Object, error) {
			merged, err := DeepMerge(live.Raw, local.Canonical, nil)
			if err != nil {
				return Object{}, err
			}
			srv.objects[live.ServerID] = merged
			c, _ := Canonicalize(merged)
			return Object{Slug: local.Slug, ServerID: live.ServerID, Canonical: c, Raw: merged}, nil
		},
		Delete: func(_ context.Context, live Object) error {
			delete(srv.objects, live.ServerID)
			return nil
		},
	}
}

// writeLocal drops a local file for the fake surface; id=="" means a brand-new
// (uncreated) object.
func writeLocal(t *testing.T, dir, slug, id, name string) {
	t.Helper()
	m := map[string]any{"name": name}
	if id != "" {
		m["id"] = id
	}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, slug+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPlanClassifies(t *testing.T) {
	srv := newFakeServer()
	srv.put("srv-1", "alpha", nil)                   // matches an unchanged local
	srv.put("srv-2", "beta", map[string]any{"v": 1}) // matches a changed local -> update
	srv.put("srv-3", "gamma", nil)                   // no local -> delete candidate
	s := fakeSurface(srv, Capabilities{PruneEligible: true})

	dir := t.TempDir()
	writeLocal(t, dir, "alpha", "srv-1", "alpha") // unchanged
	writeLocal(t, dir, "beta", "srv-2", "beta")   // server has v:1, local doesn't -> update
	writeLocal(t, dir, "delta", "", "delta")      // new -> create

	plan, _, err := BuildPlan(context.Background(), s, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(plan.Creates()); got != 1 {
		t.Errorf("creates=%d want 1", got)
	}
	if got := len(plan.Updates()); got != 1 {
		t.Errorf("updates=%d want 1", got)
	}
	if got := len(plan.Deletes()); got != 1 {
		t.Errorf("deletes=%d want 1", got)
	}
	if got := plan.Unchanged(); got != 1 {
		t.Errorf("unchanged=%d want 1", got)
	}
}

func TestPushAdditiveSkipsDeletesWithSummary(t *testing.T) {
	srv := newFakeServer()
	srv.put("srv-1", "alpha", nil)
	srv.put("srv-9", "orphan", nil) // server-only -> delete candidate
	s := fakeSurface(srv, Capabilities{PruneEligible: true})

	dir := t.TempDir()
	writeLocal(t, dir, "alpha", "srv-1", "alpha")
	writeLocal(t, dir, "newone", "", "newone")

	var buf strings.Builder
	sum, err := Push(context.Background(), s, dir, PushOpts{AssumeYes: true, Prune: false}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Created != 1 || sum.Deleted != 0 {
		t.Errorf("created=%d deleted=%d want 1/0", sum.Created, sum.Deleted)
	}
	if len(sum.SkippedDeletes) != 1 {
		t.Fatalf("expected 1 skipped delete, got %d", len(sum.SkippedDeletes))
	}
	out := buf.String()
	if !strings.Contains(out, "PRUNE SKIPPED") || !strings.Contains(out, "orphan") {
		t.Errorf("final summary should reprint the skipped delete:\n%s", out)
	}
	// The orphan must still exist server-side (additive never deleted it).
	if _, ok := srv.objects["srv-9"]; !ok {
		t.Error("additive push must not delete the orphan")
	}
}

func TestPushPruneRefusedOnIncompletePull(t *testing.T) {
	srv := newFakeServer()
	srv.put("srv-9", "orphan", nil)
	s := fakeSurface(srv, Capabilities{PruneEligible: true})
	dir := t.TempDir()
	// No .pullstate.json written (or incomplete) -> prune must be refused.

	var buf strings.Builder
	sum, err := Push(context.Background(), s, dir, PushOpts{AssumeYes: true, Prune: true}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Deleted != 0 {
		t.Errorf("prune should be refused without a complete pull; deleted=%d", sum.Deleted)
	}
	if !strings.Contains(buf.String(), "incomplete pull") {
		t.Errorf("expected an incomplete-pull refusal reason:\n%s", buf.String())
	}
	if _, ok := srv.objects["srv-9"]; !ok {
		t.Error("orphan must survive a refused prune")
	}
}

func TestPullThenPrune(t *testing.T) {
	srv := newFakeServer()
	srv.put("srv-1", "alpha", nil)
	srv.put("srv-9", "orphan", nil)
	s := fakeSurface(srv, Capabilities{PruneEligible: true})
	dir := t.TempDir()

	// Pull writes both objects + a complete pullstate.
	if _, err := Pull(context.Background(), s, dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	// Operator removes the orphan locally, then prunes.
	if err := os.Remove(filepath.Join(dir, "orphan.json")); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	sum, err := Push(context.Background(), s, dir, PushOpts{AssumeYes: true, Prune: true}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Deleted != 1 {
		t.Errorf("prune after a complete pull should delete the orphan; deleted=%d\n%s", sum.Deleted, buf.String())
	}
	if _, ok := srv.objects["srv-9"]; ok {
		t.Error("orphan should be deleted after a satisfied prune")
	}
}

func TestPushDryRunMakesNoChange(t *testing.T) {
	srv := newFakeServer()
	srv.put("srv-1", "alpha", nil)
	s := fakeSurface(srv, Capabilities{PruneEligible: true})
	dir := t.TempDir()
	writeLocal(t, dir, "newone", "", "newone")

	var buf strings.Builder
	sum, err := Push(context.Background(), s, dir, PushOpts{DryRun: true}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Created != 0 {
		t.Errorf("dry run must not create; created=%d", sum.Created)
	}
	if len(srv.objects) != 1 {
		t.Errorf("dry run must not mutate the server; objects=%d", len(srv.objects))
	}
	if !strings.Contains(buf.String(), "DRY RUN") {
		t.Errorf("dry-run output missing banner:\n%s", buf.String())
	}
}
