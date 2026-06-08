package mirror

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"danny.vn/secops/chronicle"
)

// Rule lifecycle pushes beyond create/disable: update the YARA-L text of a
// tracked rule (etag-guarded) and reconcile its deployment state (enabled /
// alerting / run frequency). Both share the dry-run / LIVE-banner / --yes guard
// with the other rule pushes and write to the supplied io.Writer.

// PushRulesUpdate updates the live YARA-L text of every tracked rule whose local
// `<slug>.yaral` differs from the live revision. Identity + the optimistic-
// concurrency etag come from the companion `<slug>.yaml`, so an out-of-band live
// edit since the last pull is rejected (etag mismatch) rather than clobbered.
// Each candidate's new text is validated before any mutation. Returns the number
// of rules updated (0 on dry-run / no work).
func PushRulesUpdate(ctx context.Context, c *chronicle.Client, rulesDir string, dryRun, assumeYes bool, w io.Writer) (int, error) {
	comps, err := trackedRules(rulesDir)
	if err != nil {
		return 0, err
	}
	if len(comps) == 0 {
		fmt.Fprintf(w, "Nothing to update -- no tracked rules (companion .yaml) in %s.\n", rulesDir)
		return 0, nil
	}

	// Live text, indexed by ruleID (FULL list carries text; fall back per-rule).
	liveText := map[string]string{}
	rules, lerr := c.ListRules(ctx)
	if lerr != nil {
		return 0, lerr
	}
	for i := range rules {
		liveText[rules[i].RuleID()] = rules[i].Text
	}

	type cand struct {
		comp *ruleCompanion
		path string // .yaral path
		text string // new local text
	}
	var cands []cand
	for _, tc := range comps {
		if tc.comp.RuleID == "" {
			continue // never created live → rules-create handles it
		}
		raw, rerr := os.ReadFile(tc.yaral)
		if rerr != nil {
			fmt.Fprintf(w, "  SKIP %s: cannot read .yaral: %v\n", filepath.Base(tc.yaral), rerr)
			continue
		}
		local := string(raw)
		live := liveText[tc.comp.RuleID]
		if live == "" {
			// FULL list omitted the text (or the rule is gone) — fetch it.
			if full, gerr := c.GetRule(ctx, tc.comp.RuleID); gerr == nil && full != nil {
				live = full.Text
			}
		}
		if sameRuleText(local, live) {
			continue
		}
		cands = append(cands, cand{comp: tc.comp, path: tc.yaral, text: local})
	}

	if len(cands) == 0 {
		fmt.Fprintf(w, "Nothing to update -- every tracked rule's .yaral matches live.\n")
		return 0, nil
	}

	liveBanner(w, fmt.Sprintf("UPDATE %d rule(s) (YARA-L text)", len(cands)))
	fmt.Fprintf(w, "%-3s %-55s %-10s\n", "#", "Rule", "Validate")
	fmt.Fprintln(w, strings.Repeat("-", 75))
	var valid []cand
	allValid := true
	for i, cd := range cands {
		ok, msg := true, ""
		res, verr := c.ValidateRule(ctx, cd.text)
		if verr != nil {
			ok, msg = false, verr.Error()
		} else if res != nil && !res.Success {
			ok, msg = false, res.Message
		}
		status := "OK"
		if !ok {
			status, allValid = "FAIL", false
		} else {
			valid = append(valid, cd)
		}
		suffix := ""
		if !ok {
			suffix = " -- " + truncate(msg, 80)
		}
		fmt.Fprintf(w, "%-3d %-55s %-10s%s\n", i+1, truncate(cd.comp.DisplayName, 55), status, suffix)
	}
	fmt.Fprintln(w)

	if !allValid {
		fmt.Fprintln(w, "At least one rule failed validation. Fix the YARA-L and retry.")
		return 0, nil
	}
	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API calls made. Re-run without --dry-run to apply.")
		return 0, nil
	}
	if !assumeYes {
		fmt.Fprintf(w, "Refusing to update %d rule(s) without confirmation (pass --yes). Aborted.\n", len(valid))
		return 0, nil
	}

	updated, failed := 0, 0
	for _, cd := range valid {
		r, uerr := c.UpdateRule(ctx, cd.comp.RuleID, cd.text, cd.comp.Etag)
		if uerr != nil {
			failed++
			fmt.Fprintf(w, "  FAIL     %s: %v\n", cd.comp.DisplayName, uerr)
			continue
		}
		// Record the new etag (and any server-normalized metadata) locally.
		cd.comp.Etag = r.Etag
		if r.Name != "" {
			cd.comp.Name = r.Name
		}
		if werr := cd.comp.write(companionPath(cd.path)); werr != nil {
			fmt.Fprintf(w, "  WARN %s: updated live but companion write failed: %v\n", cd.comp.DisplayName, werr)
		}
		updated++
		fmt.Fprintf(w, "  updated  %s\n", cd.comp.DisplayName)
	}
	fmt.Fprintf(w, "\nDone. %d updated, %d failed.\n", updated, failed)
	return updated, nil
}

// PushRulesDeploy reconciles each tracked rule's deployment state (enabled /
// alerting / run frequency) from its companion `<slug>.yaml` to live, applying
// only the rules whose desired deployment differs from live. This is the rule
// deployment state machine as code (it subsumes enable / disable / alerting).
// Returns the number of deployments changed (0 on dry-run / no work).
func PushRulesDeploy(ctx context.Context, c *chronicle.Client, rulesDir string, dryRun, assumeYes bool, w io.Writer) (int, error) {
	return PushRulesDeployFiltered(ctx, c, rulesDir, "", dryRun, assumeYes, w)
}

// PushRulesDeployFiltered is PushRulesDeploy scoped to one tracked rule when
// ruleFilter is non-empty. The filter accepts a rule id, full resource name,
// display name, or local slug/stem, so operators can target the form they have
// open without listing live state first.
func PushRulesDeployFiltered(ctx context.Context, c *chronicle.Client, rulesDir, ruleFilter string, dryRun, assumeYes bool, w io.Writer) (int, error) {
	comps, err := trackedRules(rulesDir)
	if err != nil {
		return 0, err
	}
	if len(comps) == 0 {
		fmt.Fprintf(w, "Nothing to deploy -- no tracked rules (companion .yaml) in %s.\n", rulesDir)
		return 0, nil
	}
	if ruleFilter != "" {
		comps = filterTrackedRules(comps, ruleFilter)
		if len(comps) == 0 {
			return 0, fmt.Errorf("rules-deploy --rule %q: no tracked rule matched (use rule id, display name, or slug)", ruleFilter)
		}
		if len(comps) > 1 {
			return 0, fmt.Errorf("rules-deploy --rule %q: matched %d tracked rules; use a rule id or unique slug", ruleFilter, len(comps))
		}
	}

	live := map[string]chronicle.RuleDeployment{}
	deps, lerr := c.ListRuleDeployments(ctx)
	if lerr != nil {
		return 0, lerr
	}
	for _, d := range deps {
		live[d.RuleID()] = d
	}

	type cand struct {
		comp *ruleCompanion
		path string
		want deploymentMeta
		have chronicle.RuleDeployment
	}
	var cands []cand
	for _, tc := range comps {
		if tc.comp.RuleID == "" {
			continue
		}
		want := tc.comp.Deployment
		have := live[tc.comp.RuleID]
		if want.Enabled == have.Enabled && want.Alerting == have.Alerting &&
			(want.RunFrequency == "" || want.RunFrequency == have.RunFrequency) {
			continue
		}
		cands = append(cands, cand{comp: tc.comp, path: tc.path, want: want, have: have})
	}

	if len(cands) == 0 {
		fmt.Fprintf(w, "Nothing to deploy -- every tracked rule's deployment matches live.\n")
		return 0, nil
	}

	liveBanner(w, fmt.Sprintf("DEPLOY %d rule(s) (enable/alerting/frequency)", len(cands)))
	fmt.Fprintf(w, "%-3s %-50s %-22s %-22s\n", "#", "Rule", "live (en/al/freq)", "desired (en/al/freq)")
	fmt.Fprintln(w, strings.Repeat("-", 98))
	for i, cd := range cands {
		fmt.Fprintf(w, "%-3d %-50s %-22s %-22s\n", i+1, truncate(cd.comp.DisplayName, 50),
			deployTriple(cd.have.Enabled, cd.have.Alerting, cd.have.RunFrequency),
			deployTriple(cd.want.Enabled, cd.want.Alerting, cd.want.RunFrequency))
	}
	fmt.Fprintln(w)

	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API calls made. Re-run without --dry-run to apply.")
		return 0, nil
	}
	if !assumeYes {
		fmt.Fprintf(w, "Refusing to deploy %d rule(s) without confirmation (pass --yes). Aborted.\n", len(cands))
		return 0, nil
	}

	changed, failed := 0, 0
	for _, cd := range cands {
		upd := chronicle.RuleDeploymentUpdate{
			Enabled:  new(cd.want.Enabled),
			Alerting: new(cd.want.Alerting),
		}
		if cd.want.RunFrequency != "" {
			upd.RunFrequency = cd.want.RunFrequency
		}
		dep, derr := c.UpdateRuleDeployment(ctx, cd.comp.RuleID, upd)
		if derr != nil {
			failed++
			fmt.Fprintf(w, "  FAIL     %s: %v\n", cd.comp.DisplayName, derr)
			continue
		}
		if dep != nil {
			cd.comp.Deployment = deploymentMeta{
				Name: dep.Name, Enabled: dep.Enabled, Alerting: dep.Alerting,
				RunFrequency: dep.RunFrequency, ExecutionState: dep.ExecutionState,
			}
			if werr := cd.comp.write(cd.path); werr != nil {
				fmt.Fprintf(w, "  WARN %s: deployed live but companion write failed: %v\n", cd.comp.DisplayName, werr)
			}
		}
		changed++
		fmt.Fprintf(w, "  deployed %s\n", cd.comp.DisplayName)
	}
	fmt.Fprintf(w, "\nDone. %d deployed, %d failed.\n", changed, failed)
	return changed, nil
}

// --- helpers ----------------------------------------------------------------

// trackedRule is a companion `.yaml` plus the paths to it and its `.yaral`.
type trackedRule struct {
	comp  *ruleCompanion
	path  string // .yaml path
	yaral string // sibling .yaral path
}

// trackedRules loads every companion `.yaml` (a tracked rule) from rulesDir,
// sorted by filename. A missing dir yields no rules, not an error.
func trackedRules(rulesDir string) ([]trackedRule, error) {
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var out []trackedRule
	for _, name := range names {
		path := filepath.Join(rulesDir, name)
		comp, rerr := readRuleCompanion(path)
		if rerr != nil {
			continue
		}
		out = append(out, trackedRule{
			comp:  comp,
			path:  path,
			yaral: strings.TrimSuffix(path, ".yaml") + ".yaral",
		})
	}
	return out, nil
}

// filterTrackedRules keeps rules matching filter as a rule id, full resource
// name, display name, or local slug/stem.
func filterTrackedRules(rules []trackedRule, filter string) []trackedRule {
	filter = strings.TrimSpace(filter)
	needles := map[string]bool{
		strings.ToLower(filter):              true,
		strings.ToLower(lastSegment(filter)): true,
	}
	var out []trackedRule
	for _, r := range rules {
		stem := strings.TrimSuffix(filepath.Base(r.path), filepath.Ext(r.path))
		candidates := []string{
			r.comp.RuleID,
			lastSegment(r.comp.Name),
			r.comp.DisplayName,
			Slugify(r.comp.DisplayName),
			stem,
		}
		for _, c := range candidates {
			if needles[strings.ToLower(strings.TrimSpace(c))] {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// companionPath returns the `.yaml` path for a given `.yaral` path.
func companionPath(yaralPath string) string {
	return strings.TrimSuffix(yaralPath, filepath.Ext(yaralPath)) + ".yaml"
}

// sameRuleText compares two YARA-L bodies ignoring a trailing-newline-only diff
// (editors commonly add one), so an unedited rule is never a phantom update.
func sameRuleText(a, b string) bool {
	return strings.TrimRight(a, "\n") == strings.TrimRight(b, "\n")
}

// deployTriple renders the enabled/alerting/frequency triple for a preview.
func deployTriple(enabled, alerting bool, freq string) string {
	if freq == "" {
		freq = "-"
	}
	return fmt.Sprintf("en=%v al=%v %s", enabled, alerting, freq)
}
