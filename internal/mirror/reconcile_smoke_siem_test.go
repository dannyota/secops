package mirror

import (
	"bytes"
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

// TestLiveReconcileDashboardWriteSmoke exercises the dashboards write loop on a
// throwaway CUSTOM dashboard: create → write/load round-trip → update the
// description → delete. It drives the surface closures DIRECTLY rather than
// reconcile.Push, because the dashboards List fetches every CUSTOM dashboard in
// FULL view — repeating that per plan rebuild can rate-limit (429) on instances
// with many dashboards. The engine plan path itself is covered by the read round-trip and the
// data_tables/feeds write smokes; here we validate the dashboard-specific
// create/update/delete + canonical round-trip with a minimal call count.
func TestLiveReconcileDashboardWriteSmoke(t *testing.T) {
	c, ctx := liveChronicleClient(t)
	requireSIEMSmokeWrite(t)

	s, ok := BuildSIEMSurface("dashboards", c)
	if !ok {
		t.Fatal("dashboards is not a registered engine surface")
	}
	label := smokeLabel("dash")
	dir := t.TempDir()

	createCanon, err := reconcile.Canonicalize(fmt.Appendf(nil,
		`{"displayName":%q,"description":"secopsctl reconcile smoke","access":"DASHBOARD_PRIVATE","type":"CUSTOM"}`, label))
	if err != nil {
		t.Fatal(err)
	}
	local := reconcile.Object{Slug: Slugify(label), Canonical: createCanon}

	deleted := false
	var serverID string
	t.Cleanup(func() {
		if deleted || serverID == "" {
			return
		}
		if err := c.DeleteDashboard(ctx, lastSegment(serverID)); err != nil {
			t.Logf("cleanup: could not delete throwaway dashboard %q: %v", label, err)
		}
	})

	// Create (direct closure).
	echo, err := s.Create(ctx, local)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	serverID = echo.ServerID
	if serverID == "" {
		t.Fatal("create returned no ServerID")
	}

	// Round-trip: write the echo, reload from disk, canonical must match.
	if err := s.Write(dir, echo); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := s.LoadDir(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 || loaded[0].ServerID != serverID {
		t.Fatalf("round-trip: loaded %d obj (id=%v), want 1 with id %q", len(loaded), loaded, serverID)
	}
	if !bytes.Equal(loaded[0].Canonical, echo.Canonical) {
		t.Fatalf("create round-trip canonical mismatch:\n echo: %s\n disk: %s", echo.Canonical, loaded[0].Canonical)
	}

	// Update: edit the description, run the Update closure, confirm it applied.
	editedCanon := json.RawMessage(strings.Replace(string(echo.Canonical),
		"secopsctl reconcile smoke", "secopsctl reconcile smoke (edited)", 1))
	edited := reconcile.Object{Slug: echo.Slug, ServerID: serverID, Canonical: editedCanon}
	echo2, err := s.Update(ctx, edited, echo)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(string(echo2.Canonical), "(edited)") {
		t.Errorf("update not applied:\n%s", echo2.Canonical)
	}

	// Delete + confirm gone.
	if err := s.Delete(ctx, echo2); err != nil {
		t.Fatalf("delete: %v", err)
	}
	deleted = true
	if _, gerr := c.GetDashboard(ctx, lastSegment(serverID), false); gerr == nil {
		t.Errorf("dashboard still present after delete")
	}
}

// TestLiveRulesLifecycleWriteSmoke exercises the rule lifecycle pushes on an
// inert throwaway rule: create (PushRulesCreate) → update its YARA-L text
// (PushRulesUpdate, etag-guarded) → disable its deployment (PushRulesDeploy) →
// delete. The rule matches a nonexistent host, so it produces no detections even
// while briefly enabled; DeleteRule cleans it up even on failure.
func TestLiveRulesLifecycleWriteSmoke(t *testing.T) {
	c, ctx := liveChronicleClient(t)
	requireSIEMSmokeWrite(t)

	name := strings.ReplaceAll(smokeLabel("rule"), "-", "_") // YARA-L name: no hyphens
	slug := Slugify(name)
	dir := t.TempDir()
	yaral := filepath.Join(dir, slug+".yaral")
	yaml := filepath.Join(dir, slug+".yaml")

	ruleText := func(desc string) string {
		return fmt.Sprintf(`rule %s {
  meta:
    author = "secopsctl-smoketest"
    description = %q
  events:
    $e.principal.hostname = "secopsctl-smoketest-nonexistent-zzzzzz"
  condition:
    $e
}
`, name, desc)
	}

	// New .yaral, no companion → PushRulesCreate creates + enables (inert) + tracks.
	if err := os.WriteFile(yaral, []byte(ruleText("inert smoke-test rule; safe to delete")), 0o644); err != nil {
		t.Fatal(err)
	}

	cleaned := false
	t.Cleanup(func() {
		if cleaned {
			return
		}
		if comp, err := readRuleCompanion(yaml); err == nil && comp.RuleID != "" {
			if derr := c.DeleteRule(ctx, comp.RuleID, true); derr != nil {
				t.Logf("cleanup: delete rule %q: %v", comp.RuleID, derr)
			}
		}
	})

	if _, err := PushRulesCreate(ctx, c, dir, false, true, io.Discard); err != nil {
		t.Fatalf("create: %v", err)
	}
	comp, err := readRuleCompanion(yaml)
	if err != nil || comp.RuleID == "" {
		t.Fatalf("create did not record a companion with a rule_id: %v", err)
	}
	ruleID := comp.RuleID

	// Update the YARA-L text → PushRulesUpdate (etag round-trip).
	if err := os.WriteFile(yaral, []byte(ruleText("inert smoke-test rule (edited); safe to delete")), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := PushRulesUpdate(ctx, c, dir, false, true, io.Discard)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if n != 1 {
		t.Fatalf("update changed %d rules, want 1", n)
	}
	full, err := c.GetRule(ctx, ruleID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if !strings.Contains(full.Text, "(edited)") {
		t.Errorf("update not applied live (text has no '(edited)')")
	}

	// Disable the deployment via PushRulesDeploy (set the companion enabled=false).
	comp, _ = readRuleCompanion(yaml)
	comp.Deployment.Enabled = false
	if err := comp.write(yaml); err != nil {
		t.Fatal(err)
	}
	if _, err := PushRulesDeploy(ctx, c, dir, false, true, io.Discard); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if dep, err := c.GetRuleDeployment(ctx, ruleID); err != nil {
		t.Fatalf("get deployment: %v", err)
	} else if dep.Enabled {
		t.Errorf("deploy did not disable the rule (still enabled)")
	}

	// Delete + confirm gone.
	if err := c.DeleteRule(ctx, ruleID, true); err != nil {
		t.Fatalf("delete: %v", err)
	}
	cleaned = true
	if _, err := c.GetRule(ctx, ruleID); err == nil {
		t.Errorf("rule still present after delete")
	}
}

// TestLiveRetrohuntCreateWriteSmoke validates the retrohunt create path (the SDK
// call `rules retrohunt create` wraps) on an inert throwaway rule that matches a
// nonexistent host — so the retrohunt scans history but produces no detections.
// The throwaway rule is deleted on cleanup (which also removes its retrohunt).
func TestLiveRetrohuntCreateWriteSmoke(t *testing.T) {
	c, ctx := liveChronicleClient(t)
	requireSIEMSmokeWrite(t)

	name := strings.ReplaceAll(smokeLabel("retro"), "-", "_")
	text := fmt.Sprintf(`rule %s {
  meta:
    author = "secopsctl-smoketest"
    description = "inert retrohunt smoke; matches a nonexistent host; safe to delete"
  events:
    $e.principal.hostname = "secopsctl-smoketest-nonexistent-zzzzzz"
  condition:
    $e
}
`, name)

	rule, err := c.CreateRule(ctx, text)
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	ruleID := rule.RuleID()
	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		if derr := c.DeleteRule(ctx, ruleID, true); derr != nil {
			t.Logf("cleanup: delete rule %q: %v", ruleID, derr)
		}
	})

	// Create a retrohunt over the last hour (minimal scan).
	end := time.Now().UTC()
	start := end.Add(-1 * time.Hour)
	rh, err := c.CreateRetrohunt(ctx, ruleID, start, end)
	if err != nil {
		t.Fatalf("create retrohunt: %v", err)
	}
	rhID := lastSegment(rh.Name)
	if rhID == "" {
		t.Fatalf("retrohunt has no name: %+v", rh)
	}

	// It must appear in the list and be gettable.
	rhs, err := c.ListRetrohunts(ctx, ruleID)
	if err != nil {
		t.Fatalf("list retrohunts: %v", err)
	}
	found := false
	for i := range rhs {
		if lastSegment(rhs[i].Name) == rhID {
			found = true
		}
	}
	if !found {
		t.Errorf("created retrohunt %q not in list", rhID)
	}
	if got, gerr := c.GetRetrohunt(ctx, ruleID, rhID); gerr != nil {
		t.Errorf("get retrohunt: %v", gerr)
	} else if got.State == "" {
		t.Errorf("retrohunt has no state")
	}

	if err := c.DeleteRule(ctx, ruleID, true); err != nil {
		t.Fatalf("delete rule: %v", err)
	}
	deleted = true
}

// TestLiveReconcileRuleExclusionWriteSmoke exercises the rule_exclusions engine
// write loop on an inert throwaway exclusion (a UDM query matching a nonexistent
// host → suppresses nothing): create → in-sync → update the query → in-sync, then
// ARCHIVE it for cleanup. Rule exclusions have no delete API; archiving
// (deployment enabled=false, archived=true) is the documented teardown — the same
// state several live exclusions already sit in.
func TestLiveReconcileRuleExclusionWriteSmoke(t *testing.T) {
	c, ctx := liveChronicleClient(t)
	requireSIEMSmokeWrite(t)

	s, ok := BuildSIEMSurface("rule_exclusions", c)
	if !ok {
		t.Fatal("rule_exclusions is not a registered engine surface")
	}
	label := smokeLabel("excl")
	slug := Slugify(label)
	dir := t.TempDir()

	if _, err := reconcile.Pull(ctx, s, dir, io.Discard); err != nil {
		t.Fatalf("baseline pull: %v", err)
	}

	archived := false
	t.Cleanup(func() {
		if archived {
			return
		}
		if o, found := findBySlug(ctx, s, slug); found {
			if _, err := c.UpdateRuleExclusionDeployment(ctx, lastSegment(o.ServerID),
				chronicle.RuleExclusionDeploymentUpdate{Enabled: new(false), Archived: new(true)}); err != nil {
				t.Logf("cleanup: archive exclusion %q: %v", label, err)
			}
		}
	})

	// New local file (no name → create): an inert exclusion that matches nothing.
	if err := writeYAML(filepath.Join(dir, slug+".yaml"), ruleExclusionMeta{
		DisplayName: label,
		Type:        string(chronicle.DetectionExclusion),
		Query:       `(principal.hostname = "secopsctl-smoketest-nonexistent-zzzzzz")`,
	}); err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
		t.Fatalf("push create: %v\n%s", err, buf.String())
	}
	live, found := findBySlug(ctx, s, slug)
	if !found {
		t.Fatalf("exclusion %q not found after create. push log:\n%s", label, buf.String())
	}
	var meta ruleExclusionMeta
	if err := readYAMLFile(filepath.Join(dir, slug+".yaml"), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Name == "" || meta.Name != live.ServerID {
		t.Fatalf("create did not record server name (yaml=%q live=%q)", meta.Name, live.ServerID)
	}
	if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
		t.Fatalf("post-create plan not in sync: +%d ~%d -%d",
			len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
	}

	// Edit the query → exactly one update reconciles clean.
	meta.Query = `(principal.hostname = "secopsctl-smoketest-nonexistent-yyyyyy")`
	if err := writeYAML(filepath.Join(dir, slug+".yaml"), meta); err != nil {
		t.Fatal(err)
	}
	assertOneUpdate(t, ctx, s, dir, "query")

	// Cleanup: archive (the only teardown the API offers) + verify.
	exclusionID := lastSegment(live.ServerID)
	if _, err := c.UpdateRuleExclusionDeployment(ctx, exclusionID,
		chronicle.RuleExclusionDeploymentUpdate{Enabled: new(false), Archived: new(true)}); err != nil {
		t.Fatalf("archive: %v", err)
	}
	archived = true
	if dep, err := c.GetRuleExclusionDeployment(ctx, exclusionID); err != nil {
		t.Fatalf("get deployment: %v", err)
	} else if !dep.Archived {
		t.Errorf("exclusion not archived after cleanup")
	}
}

// TestLiveReconcileFeedWriteSmoke validates the feeds create + delete path on an
// inert throwaway: an HTTP feed pointed at a dead URL (example.com) ingests
// nothing and needs no bucket/IAM. It exercises CreateFeed (incl. the short→full
// logType expansion) and the surface's Delete closure (DeleteFeed) — the delete
// the reconcile push deliberately can't do (feeds aren't prune-eligible).
func TestLiveReconcileFeedWriteSmoke(t *testing.T) {
	c, ctx := liveChronicleClient(t)
	requireSIEMSmokeWrite(t)

	s, ok := BuildSIEMSurface("feeds", c)
	if !ok {
		t.Fatal("feeds is not a registered engine surface")
	}
	label := smokeLabel("feed")

	created, err := c.CreateFeed(ctx, label, "HTTP", "WINEVTLOG", "", map[string]any{
		"httpSettings": map[string]any{"uri": "https://example.com/secopsctl-smoke", "sourceType": "FILES"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := created.Name
	if id == "" {
		t.Fatal("create returned no resource name")
	}

	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		if derr := c.DeleteFeed(ctx, id); derr != nil {
			t.Logf("cleanup: delete feed %q: %v", id, derr)
		}
	})

	if got, gerr := c.GetFeed(ctx, id); gerr != nil {
		t.Fatalf("get after create: %v", gerr)
	} else if got.DisplayName != label {
		t.Errorf("displayName = %q, want %q", got.DisplayName, label)
	}

	// Update (display-name-only PATCH) → verify it applied.
	updated := label + " (updated)"
	if _, uerr := c.UpdateFeed(ctx, id, updated, "", "", "", nil); uerr != nil {
		t.Fatalf("update: %v", uerr)
	}
	if got, gerr := c.GetFeed(ctx, id); gerr != nil {
		t.Fatalf("get after update: %v", gerr)
	} else if got.DisplayName != updated {
		t.Errorf("update not applied: displayName = %q, want %q", got.DisplayName, updated)
	}

	// Delete via the surface closure (the path reconcile push won't take).
	if derr := s.Delete(ctx, reconcile.Object{ServerID: id}); derr != nil {
		t.Fatalf("delete: %v", derr)
	}
	deleted = true
	if _, gerr := c.GetFeed(ctx, id); gerr == nil {
		t.Errorf("feed still present after delete")
	}
}

// firstActiveParser returns a log type that has an ACTIVE parser and that
// parser's full record (CBN populated), to borrow valid parser source from. The
// log-type set is the same feed-derived set the puller uses.
func firstActiveParser(ctx context.Context, t *testing.T, c *chronicle.Client) (string, *chronicle.Parser) {
	t.Helper()
	logTypes, err := logTypesInUse(ctx, c)
	if err != nil {
		t.Skipf("logTypesInUse: %v", err)
	}
	for _, lt := range logTypes {
		parsers, perr := c.ListParsers(ctx, lt)
		if perr != nil {
			continue
		}
		a := activeParser(parsers)
		if a == nil {
			continue
		}
		full, gerr := c.GetParser(ctx, lt, lastSegment(a.Name))
		if gerr != nil || full.CBN == "" {
			continue
		}
		return lt, full
	}
	return "", nil
}

// TestLiveReconcileParserWriteSmoke validates the parser write lifecycle WITHOUT
// disturbing live ingestion. Parsers are versioned/immutable: a created parser is
// INACTIVE until separately activated, and only the ACTIVE one processes logs. The
// smoke (1) runs RunParser — pure inert validation that creates no server state —
// then (2) borrows a real ACTIVE parser's source (a unique trailing comment makes
// it a distinct version), creates it as a new INACTIVE parser, confirms it never
// went ACTIVE, and deletes the throwaway. The live ACTIVE parser is asserted
// unchanged throughout; cleanup force-deletes the throwaway even on failure.
func TestLiveReconcileParserWriteSmoke(t *testing.T) {
	c, ctx := liveChronicleClient(t)
	requireSIEMSmokeWrite(t)

	logType, base := firstActiveParser(ctx, t, c)
	if base == nil {
		t.Skip("no log type with an ACTIVE parser found to borrow source from")
	}
	src, err := decodeCBN(base.CBN)
	if err != nil || src == "" {
		t.Skipf("active parser source unavailable: %v", err)
	}

	// 1. RunParser: evaluate the real source against a dummy log. Inert — nothing
	// is created or activated; we only assert the API path round-trips.
	if _, err := c.RunParser(ctx, logType, src, []string{"secopsctl-smoketest dummy log line"}); err != nil {
		t.Fatalf("RunParser: %v", err)
	}

	// 2. Create a new INACTIVE version from the same source (unique comment → a
	// distinct version), never activated, then delete it.
	smokeSrc := src + "\n# secopsctl-smoketest " + smokeLabel("parser") + "\n"
	created, err := c.CreateParser(ctx, logType, smokeSrc, false)
	if err != nil {
		t.Fatalf("CreateParser: %v", err)
	}
	id := lastSegment(created.Name)
	if id == "" {
		t.Fatalf("create returned no parser id: %+v", created)
	}
	if created.State == "ACTIVE" {
		t.Fatalf("throwaway parser is ACTIVE on create (would shadow live ingestion); state=%q", created.State)
	}

	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		if derr := c.DeleteParser(ctx, logType, id, true); derr != nil {
			t.Logf("cleanup: delete throwaway parser %s/%s: %v", logType, id, derr)
		}
	})

	got, err := c.GetParser(ctx, logType, id)
	if err != nil {
		t.Fatalf("GetParser after create: %v", err)
	}
	if got.State == "ACTIVE" {
		t.Errorf("throwaway parser became ACTIVE: %q", got.State)
	}

	// Delete the inactive throwaway (force=false: it must not be active).
	if err := c.DeleteParser(ctx, logType, id, false); err != nil {
		t.Fatalf("DeleteParser: %v", err)
	}
	deleted = true
	if _, err := c.GetParser(ctx, logType, id); err == nil {
		t.Errorf("throwaway parser still present after delete")
	}

	// The borrowed log type's ACTIVE parser must be the same one as before.
	parsers, err := c.ListParsers(ctx, logType)
	if err != nil {
		t.Fatalf("relist parsers: %v", err)
	}
	if a := activeParser(parsers); a == nil || a.Name != base.Name {
		t.Errorf("active parser for %s changed (want %q, got %+v)", logType, base.Name, a)
	}
}

// TestLiveReconcileReferenceListWriteSmoke validates the reference_lists engine
// write loop. Reference lists have NO delete API (NoDelete), so this can't be a
// create-and-delete throwaway like the other surfaces; instead it reuses a single
// fixed, clearly-labeled inert list ("secopsctl_smoke_reflist"): it creates the
// list on first run (or reuses an existing one), then drives exactly one engine
// update — a fresh description + entries each run, so the update is always present
// — and asserts a clean round-trip. Rerunnable: it reuses the one list rather than
// accumulating throwaways. The list's entries match no telemetry and it is left in
// place by design (no delete endpoint exists).
func TestLiveReconcileReferenceListWriteSmoke(t *testing.T) {
	c, ctx := liveChronicleClient(t)
	requireSIEMSmokeWrite(t)

	s, ok := BuildSIEMSurface("reference_lists", c)
	if !ok {
		t.Fatal("reference_lists is not a registered engine surface")
	}

	const display = "secopsctl_smoke_reflist" // valid ref-list id: letter + letters/digits/underscores
	slug := Slugify(display)
	dir := t.TempDir()

	// Baseline pull so existing lists have local files (else they show as deletes
	// and the in-sync checks fail).
	if _, err := reconcile.Pull(ctx, s, dir, io.Discard); err != nil {
		t.Fatalf("baseline pull: %v", err)
	}

	// Create the fixed inert list if this tenant doesn't have it yet.
	if _, found := findBySlug(ctx, s, slug); !found {
		if err := writeYAML(filepath.Join(dir, slug+".yaml"), refListMeta{
			DisplayName: display,
			Description: "secopsctl reconcile smoke — inert; reference lists have no delete API",
			SyntaxType:  chronicle.RefListSyntaxString,
		}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, slug+".txt"),
			[]byte("secopsctl-smoketest-inert-entry\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var buf strings.Builder
		if _, err := reconcile.Push(ctx, s, dir, reconcile.PushOpts{AssumeYes: true}, &buf); err != nil {
			t.Fatalf("push create: %v\n%s", err, buf.String())
		}
		live, ok := findBySlug(ctx, s, slug)
		if !ok {
			t.Fatalf("reference list %q not found after create. push log:\n%s", display, buf.String())
		}
		var meta refListMeta
		if err := readYAMLFile(filepath.Join(dir, slug+".yaml"), &meta); err != nil {
			t.Fatal(err)
		}
		if meta.Name == "" || meta.Name != live.ServerID {
			t.Fatalf("create did not record server name (yaml=%q live=%q)", meta.Name, live.ServerID)
		}
		if plan, _, _ := reconcile.BuildPlan(ctx, s, dir); !plan.Empty() {
			t.Fatalf("post-create plan not in sync: +%d ~%d -%d",
				len(plan.Creates()), len(plan.Updates()), len(plan.Deletes()))
		}
	}

	// Drive exactly one update: a fresh description + entries (unique per run so an
	// update is always present and the test stays rerunnable). This exercises the
	// description PATCH and the wholesale entries replacement in one reconcile.
	token := strings.ReplaceAll(smokeLabel("rev"), "-", "_")
	var meta refListMeta
	if err := readYAMLFile(filepath.Join(dir, slug+".yaml"), &meta); err != nil {
		t.Fatal(err)
	}
	meta.Description = "secopsctl reconcile smoke " + token
	if err := writeYAML(filepath.Join(dir, slug+".yaml"), meta); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, slug+".txt"),
		[]byte("secopsctl-smoketest-inert-entry\n"+token+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertOneUpdate(t, ctx, s, dir, "description+entries")

	t.Logf("reference_lists: no delete API — inert list %q left in place by design (reused, not accumulated)", display)
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
