package mirror

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/secops/internal/mirror/reconcile"
)

// TestLiveReconcileForwarderWriteSmoke exercises the forwarders engine write loop
// on a uniquely-labeled throwaway forwarder (no collectors, server settings
// disabled → it ingests nothing): push-create, an in-sync round-trip, push-update
// of the config, then delete-by-id. Additive throughout (never --prune), so it can
// only ever touch its own throwaway, which t.Cleanup deletes even on failure.
// Gated on SECOPS_SIEM_SMOKE=1 + SECOPS_SIEM_SMOKE_WRITE=1.
func TestLiveReconcileForwarderWriteSmoke(t *testing.T) {
	c, ctx := liveChronicleClient(t)
	requireSIEMSmokeWrite(t)

	s, ok := BuildSIEMSurface("forwarders", c)
	if !ok {
		t.Fatal("forwarders is not a registered engine surface")
	}

	label := smokeLabel("forwarder")
	slug := Slugify(label)
	dir := t.TempDir()

	// Baseline pull so existing forwarders have local files; otherwise they show as
	// delete candidates and the in-sync check fails.
	if _, err := reconcile.Pull(ctx, s, dir, io.Discard); err != nil {
		t.Fatalf("baseline pull: %v", err)
	}

	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		if o, found := findBySlug(ctx, s, slug); found && s.Delete != nil {
			if err := s.Delete(ctx, o); err != nil {
				t.Logf("cleanup: could not delete throwaway forwarder %q: %v", label, err)
			}
		}
	})

	// New local file (no name → the engine plans a create).
	writeSmokeForwarderFile(t, dir, slug, label, false)

	// 1. Create.
	var buf strings.Builder
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
		t.Fatalf("push create: %v\n%s", err, buf.String())
	}
	live, found := findBySlug(ctx, s, slug)
	if !found {
		t.Fatalf("forwarder %q not found after create. push log:\n%s", label, buf.String())
	}

	// 2. refreshLocal must have recorded the server name back into the YAML.
	var od fwdOnDisk
	if err := readYAMLFile(filepath.Join(dir, slug+".yaml"), &od); err != nil {
		t.Fatal(err)
	}
	if od.Name == "" || od.Name != live.ServerID {
		t.Fatalf("create did not record server name (yaml=%q live=%q)", od.Name, live.ServerID)
	}

	// 3. Fresh plan must be clean.
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
		t.Fatalf("post-create plan not in sync: +%d ~%d -%d",
			len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}

	// 4. Edit the config (toggle uploadCompression) → exactly one update reconciles clean.
	writeSmokeForwarderFile(t, dir, slug, label, true)
	od2 := mustReadForwarder(t, dir, slug)
	od2.Name = od.Name // keep the recorded server identity
	if err := writeYAML(filepath.Join(dir, slug+".yaml"), od2); err != nil {
		t.Fatal(err)
	}
	assertOneUpdate(t, ctx, s, dir, "config")

	// 5. Delete by id (not --prune).
	if err := s.Delete(ctx, live); err != nil {
		t.Fatalf("delete: %v", err)
	}
	deleted = true
	if _, still := findBySlug(ctx, s, slug); still {
		t.Fatalf("throwaway forwarder %q still present after delete", label)
	}
}

// writeSmokeForwarderFile writes the `<slug>.yaml` for a throwaway forwarder (no
// server name → a create). serverSettings stays disabled so it never ingests.
func writeSmokeForwarderFile(t *testing.T, dir, slug, display string, uploadCompression bool) {
	t.Helper()
	od := fwdOnDisk{
		DisplayName: display,
		Config: map[string]any{
			"uploadCompression": uploadCompression,
			"metadata":          map[string]any{},
			"serverSettings":    map[string]any{"enabled": false},
		},
	}
	if err := writeYAML(filepath.Join(dir, slug+".yaml"), od); err != nil {
		t.Fatal(err)
	}
}

// mustReadForwarder reads back a `<slug>.yaml` forwarder record.
func mustReadForwarder(t *testing.T, dir, slug string) fwdOnDisk {
	t.Helper()
	var od fwdOnDisk
	if err := readYAMLFile(filepath.Join(dir, slug+".yaml"), &od); err != nil {
		t.Fatal(err)
	}
	return od
}
