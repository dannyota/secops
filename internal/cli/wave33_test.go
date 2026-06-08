package cli

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"danny.vn/secops/internal/mirror"
)

// findCmd returns the immediate child of parent named name, or nil.
func findCmd(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// childNames returns the names of parent's immediate subcommands.
func childNames(parent *cobra.Command) []string {
	out := make([]string, 0, len(parent.Commands()))
	for _, c := range parent.Commands() {
		out = append(out, c.Name())
	}
	return out
}

// knownReconcileTargets collects every CLI target that maps to a config-as-code
// surface: the SIEM `push`/`pull` positional ValidArgs plus the `soar push`
// per-surface subcommands. It is the binary's own answer to "what targets exist",
// independent of the registry — so a guard can cross-check the two can't drift.
func knownReconcileTargets(t *testing.T) map[string]struct{} {
	t.Helper()
	set := make(map[string]struct{})
	add := func(ss ...string) {
		for _, s := range ss {
			set[s] = struct{}{}
		}
	}
	if push := findCmd(rootCmd, "push"); push != nil {
		add(push.ValidArgs...)
	} else {
		t.Fatal("no `push` command registered")
	}
	if pull := findCmd(rootCmd, "pull"); pull != nil {
		add(pull.ValidArgs...)
	} else {
		t.Fatal("no `pull` command registered")
	}
	if soar := findCmd(rootCmd, "soar"); soar != nil {
		if sp := findCmd(soar, "push"); sp != nil {
			add(childNames(sp)...)
		} else {
			t.Fatal("no `soar push` command registered")
		}
	} else {
		t.Fatal("no `soar` command registered")
	}
	return set
}

// TestReconcileSurfacesAreCLITargets asserts every engine-backed reconcile surface
// (the registry's source of truth) is reachable as a real CLI target, and that the
// registry knows every CLI engine target — the durable doc↔CLI consistency guard
// the surfaces view documents. A surface added to the registry but never wired to a
// command (or vice-versa) fails here instead of shipping a dangling entry.
func TestReconcileSurfacesAreCLITargets(t *testing.T) {
	known := knownReconcileTargets(t)

	// SIEM engine surfaces must each be a `push` target (rules and curated are
	// bespoke — rules is split across rules-* targets; curated is one batch-diff
	// target over deployments.yaml, not an engine surface).
	for _, name := range mirror.SIEMSurfaceNames() {
		if _, ok := known[name]; !ok {
			t.Errorf("SIEM engine surface %q is not a CLI push/pull target", name)
		}
		if len(mirror.SurfaceFamilyByName(name)) == 0 {
			t.Errorf("SIEM engine surface %q has no registry family", name)
		}
	}
	// SOAR engine surfaces must each be a `soar push <name>` subcommand.
	for _, name := range mirror.SOARSurfaceNames() {
		if _, ok := known[name]; !ok {
			t.Errorf("SOAR engine surface %q is not a `soar push` subcommand", name)
		}
		if len(mirror.SurfaceFamilyByName(name)) == 0 {
			t.Errorf("SOAR engine surface %q has no registry family", name)
		}
	}

	// Every registry reconcile-lane family must resolve to a CLI target. The single
	// bespoke exceptions: "rules" is split across rules-* push targets, while
	// "curated" is the single `push curated` target over deployments.yaml.
	for _, f := range mirror.SurfaceFamilies {
		if f.Lane != mirror.LaneReconcile {
			continue
		}
		if f.Name == "rules" {
			for _, rt := range []string{"rules-create", "rules-update", "rules-deploy", "rules-disable"} {
				if _, ok := known[rt]; !ok {
					t.Errorf("bespoke rules target %q missing from the CLI", rt)
				}
			}
			continue
		}
		if _, ok := known[f.Name]; !ok {
			t.Errorf("reconcile family %q (%s) has no CLI target", f.Name, f.Area)
		}
	}
}

// TestSurfaceProseTargetsExist asserts every hand-written behavior note keys off a
// real CLI target, so the per-target help can't reference a renamed/removed target.
func TestSurfaceProseTargetsExist(t *testing.T) {
	known := knownReconcileTargets(t)
	// "rules" is a pull target; the rules-* push targets are in `known` too.
	for key := range surfaceProse {
		if _, ok := known[key]; !ok {
			t.Errorf("surfaceProse key %q is not a known CLI target (stale note?)", key)
		}
	}
}

// TestEngineTargetsHaveHelpNote asserts every engine surface renders a non-empty
// per-target help note, so `push/pull/drift <surface> --help` is never blank for a
// real reconcile target.
func TestEngineTargetsHaveHelpNote(t *testing.T) {
	targets := append([]string{}, mirror.SIEMSurfaceNames()...)
	targets = append(targets, mirror.SOARSurfaceNames()...)
	targets = append(targets, "rules-create", "rules-update", "rules-deploy", "rules-disable", "curated")
	for _, tg := range targets {
		note := surfaceNote(tg)
		if strings.TrimSpace(note) == "" {
			t.Errorf("surfaceNote(%q) is empty — no per-target help", tg)
		}
	}
}

// TestKnownTargetsStable is a canary: it prints the known target set sorted so an
// accidental rename shows up as a readable diff in -v output (no assertion).
func TestKnownTargetsStable(t *testing.T) {
	known := knownReconcileTargets(t)
	names := make([]string, 0, len(known))
	for n := range known {
		names = append(names, n)
	}
	sort.Strings(names)
	t.Logf("known reconcile CLI targets: %s", strings.Join(names, ", "))
}
