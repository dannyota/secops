package mirror

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/secops/auth"
	"danny.vn/secops/chronicle"
	"danny.vn/secops/config"
	"danny.vn/secops/internal/mirror/reconcile"
)

// Live SIEM reconcile smoke, mirroring the SOAR harness (reconcile_smoke_test.go)
// but for the ADC/OAuth-authed Chronicle surfaces. Two opt-in gates keep it out of
// normal/CI runs and keep mutations safe:
//
//   - SECOPS_SIEM_SMOKE=1        builds a live client (read round-trips).
//   - SECOPS_SIEM_SMOKE_WRITE=1  additionally runs the create/update/delete cycle.
//
// The write smoke targets data_tables: a uniquely-labeled throwaway table whose
// rows match no real telemetry, deleted on cleanup even if an assertion fails.

const (
	siemSmokeEnvRead  = "SECOPS_SIEM_SMOKE"
	siemSmokeEnvWrite = "SECOPS_SIEM_SMOKE_WRITE"
)

func liveChronicleClient(t *testing.T) (*chronicle.Client, context.Context) {
	t.Helper()
	if os.Getenv(siemSmokeEnvRead) != "1" {
		t.Skipf("live SIEM reconcile smoke — set %s=1 (with instance config + ADC/token) to run", siemSmokeEnvRead)
	}
	inst, err := config.Load("")
	if err != nil {
		t.Skipf("no instance config: %v", err)
	}
	c, err := chronicle.NewClient(inst.Settings(), auth.OAuth(auth.WithForceIPv4(inst.ForceIPv4)))
	if err != nil {
		t.Skipf("chronicle client: %v", err)
	}
	return c, context.Background()
}

func requireSIEMSmokeWrite(t *testing.T) {
	t.Helper()
	if os.Getenv(siemSmokeEnvWrite) != "1" {
		t.Skipf("live SIEM WRITE smoke — set %s=1 to run (it creates/edits/deletes a throwaway data table on the tenant)", siemSmokeEnvWrite)
	}
}

// TestLiveReconcileReadAllSIEM pulls every registered SIEM reconcile surface and
// asserts a clean round-trip (a fresh pull diffs in-sync), validating each
// surface's list shape, identity, and canonical stability against the live
// tenant. Read-only — runs under SECOPS_SIEM_SMOKE=1 (no writes).
func TestLiveReconcileReadAllSIEM(t *testing.T) {
	c, ctx := liveChronicleClient(t)
	for _, name := range SIEMSurfaceNames() {
		t.Run(name, func(t *testing.T) {
			s, ok := BuildSIEMSurface(name, c)
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

// TestLiveReconcileDataTableWriteSmoke exercises the engine's full write loop on a
// throwaway data table: push-create (table + rows), in-sync round-trip, push-update
// of the description, push-update of the rows (the wholesale bulkReplace path), then
// delete-by-id. Additive throughout (never --prune), so it can only ever touch its
// own throwaway, which t.Cleanup deletes even on failure.
func TestLiveReconcileDataTableWriteSmoke(t *testing.T) {
	c, ctx := liveChronicleClient(t)
	requireSIEMSmokeWrite(t)

	s, ok := BuildSIEMSurface("data_tables", c)
	if !ok {
		t.Fatal("data_tables is not a registered engine surface")
	}

	// A valid data table id (letters/digits/underscores) doubles as the display name.
	label := strings.ReplaceAll(smokeLabel("dt"), "-", "_")
	slug := Slugify(label)
	dir := t.TempDir()

	// Baseline pull so existing tables have local files; otherwise they show as
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
				t.Logf("cleanup: could not delete throwaway data table %q: %v", label, err)
			}
		}
	})

	// New local files (no Name → engine plans a create): two STRING columns, one row.
	writeSmokeDataTableFiles(t, dir, slug, label, "secopsctl reconcile smoke",
		[][]string{{"smoke_host", "smoke_value"}})

	// 1. Create.
	var buf strings.Builder
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
		t.Fatalf("push create: %v\n%s", err, buf.String())
	}
	live, found := findBySlug(ctx, s, slug)
	if !found {
		t.Fatalf("data table %q not found after create. push log:\n%s", label, buf.String())
	}

	// 2. refreshLocal must have recorded the server name back into the YAML.
	var meta dataTableMeta
	if err := readYAMLFile(filepath.Join(dir, slug+".yaml"), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Name == "" || meta.Name != live.ServerID {
		t.Fatalf("create did not record server name (yaml=%q live=%q)", meta.Name, live.ServerID)
	}

	// 3. Fresh plan must be clean.
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
		t.Fatalf("post-create plan not in sync: +%d ~%d -%d",
			len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}

	// 4. Edit description → exactly one update reconciles clean.
	meta.Description = "secopsctl reconcile smoke (edited)"
	if err := writeYAML(filepath.Join(dir, slug+".yaml"), meta); err != nil {
		t.Fatal(err)
	}
	assertOneUpdate(t, ctx, s, dir, "description")

	// 5. Edit rows (the destroy-and-replace path) → one update reconciles clean.
	if err := os.WriteFile(filepath.Join(dir, slug+".csv"),
		[]byte("host,value\nsmoke_host2,smoke_value2\nsmoke_host3,smoke_value3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertOneUpdate(t, ctx, s, dir, "rows")

	// 6. Delete by id (not --prune).
	if err := s.Delete(ctx, live); err != nil {
		t.Fatalf("delete: %v", err)
	}
	deleted = true
	if _, still := findBySlug(ctx, s, slug); still {
		t.Fatalf("throwaway data table %q still present after delete", label)
	}
}

// writeSmokeDataTableFiles writes the `<slug>.yaml` + `<slug>.csv` for a new
// throwaway table (no server name → a create).
func writeSmokeDataTableFiles(t *testing.T, dir, slug, display, description string, rows [][]string) {
	t.Helper()
	colType := string(chronicle.ColumnTypeString)
	cols := []any{
		map[string]any{"originalColumn": "host", "columnType": colType},
		map[string]any{"originalColumn": "value", "columnType": colType},
	}
	if err := writeYAML(filepath.Join(dir, slug+".yaml"), dataTableMeta{
		DisplayName: display,
		Description: description,
		Columns:     cols,
		RowCount:    len(rows),
	}); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("host,value\n")
	for _, r := range rows {
		b.WriteString(strings.Join(r, ","))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, slug+".csv"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// assertOneUpdate confirms exactly one update is planned, applies it, and checks
// the surface is back in sync.
func assertOneUpdate(t *testing.T, ctx context.Context, s reconcile.Surface, dir, what string) {
	t.Helper()
	plan, _, _ := reconcile.BuildPlan(ctx, s, dir)
	if len(plan.Updates()) != 1 || len(plan.Creates()) != 0 {
		t.Fatalf("%s: expected exactly one update, got +%d ~%d", what, len(plan.Creates()), len(plan.Updates()))
	}
	var buf strings.Builder
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
		t.Fatalf("%s: push update: %v\n%s", what, err, buf.String())
	}
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
		t.Fatalf("%s: post-update plan not in sync: +%d ~%d -%d",
			what, len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}
}
