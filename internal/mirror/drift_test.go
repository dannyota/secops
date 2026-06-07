package mirror

import (
	"context"
	"io"
	"strings"
	"testing"

	"danny.vn/secops/internal/mirror/reconcile"
)

// driftFakeSurface builds a minimal prune-eligible reconcile.Surface whose List
// returns fixed live objects and whose LoadDir returns fixed local objects, so
// Drift can be tested without a live client.
func driftFakeSurface(name string, live, local []reconcile.Object) reconcile.Surface {
	s := driftFakeSurfaceCaps(name, live, local, false, reconcile.Capabilities{PruneEligible: true})
	return s
}

// driftFakeSurfaceCaps is driftFakeSurface with explicit caps + incomplete flag.
func driftFakeSurfaceCaps(name string, live, local []reconcile.Object, incomplete bool, caps reconcile.Capabilities) reconcile.Surface {
	return reconcile.Surface{
		Name: name,
		Caps: caps,
		List: func(context.Context) (reconcile.ListResult, error) {
			return reconcile.ListResult{Objects: live, Incomplete: incomplete}, nil
		},
		LoadDir: func(string) ([]reconcile.Object, error) { return local, nil },
	}
}

func obj(id, canon string) reconcile.Object {
	return reconcile.Object{Slug: id, ServerID: id, Canonical: []byte(canon)}
}

// TestDriftClassifies: Drift reports local-only (create), changed (update), and
// live-only (delete/orphan) as drift, and matching objects as in sync.
func TestDriftClassifies(t *testing.T) {
	// in-sync surface: identical local + live.
	inSync := driftFakeSurface("clean",
		[]reconcile.Object{obj("a", `{"v":1}`)},
		[]reconcile.Object{obj("a", `{"v":1}`)})

	// drifted surface: a changed object (update) + a live-only orphan (delete).
	drifted := driftFakeSurface("dirty",
		[]reconcile.Object{obj("a", `{"v":2}`), obj("b", `{"v":9}`)}, // live
		[]reconcile.Object{obj("a", `{"v":1}`)})                      // local (a changed; b orphan)

	var buf strings.Builder
	rep := Drift(context.Background(), []DriftTarget{
		{Surface: inSync, Dir: "x"},
		{Surface: drifted, Dir: "y"},
	}, &buf)

	if !rep.Drifted() {
		t.Fatal("expected overall drift")
	}
	if len(rep.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(rep.Items))
	}
	if rep.Items[0].Drifted() {
		t.Errorf("surface 'clean' should be in sync: %+v", rep.Items[0])
	}
	d := rep.Items[1]
	if d.Updated != 1 || d.Deleted != 1 || d.Created != 0 {
		t.Errorf("surface 'dirty': got +%d ~%d -%d, want +0 ~1 -1", d.Created, d.Updated, d.Deleted)
	}
	if !strings.Contains(buf.String(), "DRIFT") || !strings.Contains(buf.String(), "in sync") {
		t.Errorf("report missing markers:\n%s", buf.String())
	}
}

// TestDriftNoTargets: an empty target list reports no drift.
func TestDriftNoTargets(t *testing.T) {
	rep := Drift(context.Background(), nil, io.Discard)
	if rep.Drifted() {
		t.Error("empty target set should not report drift")
	}
}

// TestDriftIncompleteIsIndeterminate: an incomplete live list must NOT be reported
// as drift (phantom creates from the missing items would flake the CI gate) — it is
// indeterminate ("could not verify") instead.
func TestDriftIncompleteIsIndeterminate(t *testing.T) {
	// Local references a server id that the (incomplete) live list dropped → the
	// plan would classify it as a recreate; that must be suppressed as indeterminate.
	s := driftFakeSurfaceCaps("flaky",
		nil, // live list came back empty (the item was skipped)
		[]reconcile.Object{obj("a", `{"v":1}`)},
		true, // incomplete
		reconcile.Capabilities{PruneEligible: true})
	rep := Drift(context.Background(), []DriftTarget{{Surface: s, Dir: "x"}}, io.Discard)
	if rep.Drifted() {
		t.Error("incomplete live list must not count as drift")
	}
	if !rep.Indeterminate() {
		t.Error("incomplete live list must be indeterminate")
	}
}

// TestDriftNoDeleteUntracked: a live-only object on a NoDelete surface is reported
// as untracked (pull to adopt), not gate-failing drift (push can never prune it).
func TestDriftNoDeleteUntracked(t *testing.T) {
	s := driftFakeSurfaceCaps("refs",
		[]reconcile.Object{obj("a", `{"v":1}`), obj("b", `{"v":1}`)}, // live: a + orphan b
		[]reconcile.Object{obj("a", `{"v":1}`)},                      // local: only a
		false,
		reconcile.Capabilities{NoDelete: true})
	rep := Drift(context.Background(), []DriftTarget{{Surface: s, Dir: "x"}}, io.Discard)
	if rep.Drifted() {
		t.Error("live-only orphan on a NoDelete surface must not fail the gate")
	}
	if rep.Items[0].Untracked != 1 {
		t.Errorf("expected 1 untracked, got %d", rep.Items[0].Untracked)
	}
}
