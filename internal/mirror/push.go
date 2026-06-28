package mirror

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"danny.vn/secops/chronicle"
)

// Mutating push operations: every apply is a LIVE deploy to a production SecOps
// tenant. Both functions are dry-run-guarded and emit a loud banner before any
// mutation. They write all output to the provided io.Writer rather than stdout
// so the CLI layer controls the destination.
//
// DEVIATION (vs the legacy Python tool): interactive y/N confirmation is NOT
// handled here. These run non-interactively, so assumeYes==false means "abort
// with a message" — the CLI layer owns any interactive confirm and only calls
// in with assumeYes==true once the user has agreed. The LIVE banner and the
// dry-run default are split: the banner is printed here, the default value of
// dryRun is owned by the CLI flag parsing.

// Default deployment parameters applied to newly-created rules.
const (
	defaultEnabled      = true
	defaultAlerting     = true
	defaultRunFrequency = "LIVE"
)

// RulesCreateDeploymentOptions controls the initial deployment PATCH after a
// new YARA-L rule is created.
type RulesCreateDeploymentOptions struct {
	Enabled      bool
	Alerting     bool
	RunFrequency string
}

func DefaultRulesCreateDeploymentOptions() RulesCreateDeploymentOptions {
	return RulesCreateDeploymentOptions{
		Enabled:      defaultEnabled,
		Alerting:     defaultAlerting,
		RunFrequency: defaultRunFrequency,
	}
}

func normalizeRulesCreateDeploymentOptions(opts RulesCreateDeploymentOptions) (RulesCreateDeploymentOptions, error) {
	opts.RunFrequency = strings.ToUpper(strings.TrimSpace(opts.RunFrequency))
	if opts.RunFrequency == "" {
		opts.RunFrequency = defaultRunFrequency
	}
	switch opts.RunFrequency {
	case "LIVE", "HOURLY", "DAILY":
		return opts, nil
	default:
		return opts, fmt.Errorf("rules-create: invalid run frequency %q (want LIVE, HOURLY, or DAILY)", opts.RunFrequency)
	}
}

func ruleDeploymentUpdateFromCreateOptions(opts RulesCreateDeploymentOptions) chronicle.RuleDeploymentUpdate {
	enabled := opts.Enabled
	alerting := opts.Alerting
	return chronicle.RuleDeploymentUpdate{
		Enabled:      &enabled,
		Alerting:     &alerting,
		RunFrequency: opts.RunFrequency,
	}
}

// liveBanner prints a loud, unmissable warning before any LIVE mutation.
func liveBanner(w io.Writer, action string) {
	bar := strings.Repeat("!", 72)
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w, "!! LIVE DEPLOY -- this targets a PRODUCTION SecOps/Chronicle tenant !!")
	fmt.Fprintf(w, "!! Action: %s\n", action)
	fmt.Fprintln(w, "!! Changes are made directly against the live SIEM. Review carefully. !!")
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w)
}

// PushRulesCreate creates live rules from *.yaral files that have no companion
// *.yaml. A .yaral without a sibling .yaml is treated as a brand-new rule that
// has never been deployed.
//
// For each candidate it validates the YARA-L (printing an OK/FAIL table). On a
// dry run it stops after validation. If !assumeYes it aborts with a message
// (the CLI layer owns interactive confirmation). On apply it creates the rule,
// then applies the default deployment (enabled=true, alerting=true,
// runFrequency="LIVE"). Multi-event rules cannot run LIVE, so if the default
// deployment comes back disabled it re-issues the deployment at HOURLY. Finally it
// writes the companion .yaml so the rule is tracked locally. Returns the number
// of rules created (0 on dry-run/no work).
func PushRulesCreate(ctx context.Context, c *chronicle.Client, rulesDir string, dryRun, assumeYes bool, w io.Writer) (int, error) {
	return PushRulesCreateWithOptions(ctx, c, rulesDir, DefaultRulesCreateDeploymentOptions(), dryRun, assumeYes, w)
}

// PushRulesCreateWithOptions is PushRulesCreate with caller-controlled initial
// deployment state, used by the CLI flags.
func PushRulesCreateWithOptions(ctx context.Context, c *chronicle.Client, rulesDir string, opts RulesCreateDeploymentOptions, dryRun, assumeYes bool, w io.Writer) (int, error) {
	opts, err := normalizeRulesCreateDeploymentOptions(opts)
	if err != nil {
		return 0, err
	}
	candidates, err := newRuleCandidates(rulesDir)
	if err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		fmt.Fprintf(w, "Nothing to create -- every .yaral in %s has a companion .yaml.\n", rulesDir)
		return 0, nil
	}

	liveBanner(w, fmt.Sprintf("CREATE %d new rule(s)", len(candidates)))

	fmt.Fprintf(w, "About to create %d new rule(s) (deployment: enabled=%v, alerting=%v, frequency=%s):\n\n",
		len(candidates), opts.Enabled, opts.Alerting, opts.RunFrequency)
	fmt.Fprintf(w, "%-3s %-55s %-10s\n", "#", "File", "Validate")
	fmt.Fprintln(w, strings.Repeat("-", 75))

	type candidate struct {
		path string
		text string
	}
	var valid []candidate
	allValid := true
	for i, path := range candidates {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return 0, rerr
		}
		text := string(raw)

		ok := true
		msg := ""
		res, verr := c.ValidateRule(ctx, text)
		if verr != nil {
			ok, msg = false, verr.Error()
		} else if res != nil && !res.Success {
			ok, msg = false, res.Message
		}

		status := "OK"
		suffix := ""
		if !ok {
			status = "FAIL"
			allValid = false
			suffix = " -- " + truncate(msg, 80)
		} else {
			valid = append(valid, candidate{path: path, text: text})
		}
		fmt.Fprintf(w, "%-3d %-55s %-10s%s\n", i+1, filepath.Base(path), status, suffix)
	}
	fmt.Fprintln(w)

	if !allValid {
		fmt.Fprintln(w, "At least one rule failed validation. Fix the YARA-L and retry.")
		return 0, nil
	}
	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API calls made. Re-run without --dry-run to create.")
		return 0, nil
	}
	if !assumeYes {
		fmt.Fprintf(w, "Refusing to create %d rule(s) without confirmation (pass --yes). Aborted.\n", len(valid))
		return 0, nil
	}

	created := 0
	failed := 0
	disabled := 0
	for _, cand := range valid {
		landed, cerr := createAndDeployRule(ctx, c, cand.path, cand.text, opts, w)
		if cerr != nil {
			failed++
			fmt.Fprintf(w, "  FAIL     %s: %v\n", filepath.Base(cand.path), cerr)
			continue
		}
		created++
		if landed {
			disabled++
		}
	}

	// A high-volume/complex rule can be created yet left enabled=false by a platform
	// guard even when enabled was requested — call it out so the summary is not
	// mistaken for "live".
	if disabled > 0 {
		fmt.Fprintf(w, "\nDone. %d created (%d landed DISABLED — run `push rules-deploy` to enable), %d failed.\n",
			created, disabled, failed)
	} else {
		fmt.Fprintf(w, "\nDone. %d created, %d failed.\n", created, failed)
	}
	return created, nil
}

// PromoteRule validates one YARA-L file, then creates AND deploys it to the
// requested initial state in a single guarded step — the "ship a new rule" path
// that otherwise needs `push rules-create` followed by `push rules-deploy`. It
// refuses a file that already has a companion .yaml (that rule is tracked; use
// rules-update / rules-deploy). Returns the number of rules promoted (0 or 1).
func PromoteRule(ctx context.Context, c *chronicle.Client, file string, opts RulesCreateDeploymentOptions, dryRun, assumeYes bool, w io.Writer) (int, error) {
	opts, err := normalizeRulesCreateDeploymentOptions(opts)
	if err != nil {
		return 0, err
	}
	yamlPath := strings.TrimSuffix(file, filepath.Ext(file)) + ".yaml"
	if _, statErr := os.Stat(yamlPath); statErr == nil {
		return 0, fmt.Errorf("%s already has a companion %s — that rule is tracked; use `push rules-update` / `push rules-deploy`, not promote",
			filepath.Base(file), filepath.Base(yamlPath))
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return 0, err
	}
	text := string(raw)

	res, verr := c.ValidateRule(ctx, text)
	switch {
	case verr != nil:
		return 0, fmt.Errorf("validate %s: %w", filepath.Base(file), verr)
	case res != nil && !res.Success:
		return 0, fmt.Errorf("validate %s: %s", filepath.Base(file), res.Message)
	}

	liveBanner(w, fmt.Sprintf("PROMOTE rule %s (create + deploy)", filepath.Base(file)))
	fmt.Fprintf(w, "Validated %s. Would create and deploy (enabled=%v, alerting=%v, frequency=%s).\n",
		filepath.Base(file), opts.Enabled, opts.Alerting, opts.RunFrequency)

	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API calls made. Re-run without --dry-run to promote.")
		return 0, nil
	}
	if !assumeYes {
		fmt.Fprintln(w, "Refusing to promote without confirmation (pass --yes). Aborted.")
		return 0, nil
	}
	landed, cerr := createAndDeployRule(ctx, c, file, text, opts, w)
	if cerr != nil {
		return 0, cerr
	}
	if landed {
		fmt.Fprintln(w, "\nDone. 1 promoted (landed DISABLED — run `push rules-deploy` to enable).")
	} else {
		fmt.Fprintln(w, "\nDone. 1 promoted.")
	}
	return 1, nil
}

// createAndDeployRule creates one rule from YARA-L text, deploys it to the
// requested initial state (with the multi-event LIVE→HOURLY fallback), and writes
// the companion .yaml next to path. A non-nil error means the create failed (the
// rule was not made); deploy/companion-write problems are reported as warnings
// but do not fail the create, since the rule IS live. Shared by rules-create and
// rules promote.
// createAndDeployRule returns (landedDisabled, error): landedDisabled is true when
// enabled was requested but the live deployment came back disabled (a platform
// complexity/volume guard) — the rule IS created, just not running.
func createAndDeployRule(ctx context.Context, c *chronicle.Client, path, text string, opts RulesCreateDeploymentOptions, w io.Writer) (bool, error) {
	stem := stemOf(path)
	rule, cerr := c.CreateRule(ctx, text)
	if cerr != nil {
		return false, cerr
	}
	ruleID := rule.RuleID()

	// Deploy with the requested initial state. A freshly-created rule may not be
	// immediately deployable (indexing lag), so retry with backoff on failure.
	// Multi-event rules (those with a match block) cannot run LIVE, so the API may
	// return enabled=false for the default LIVE request; in that default-compatible
	// case, re-issue at HOURLY to preserve the operator's enabled=true intent.
	dep, derr := retryDeploy(ctx, c, ruleID, ruleDeploymentUpdateFromCreateOptions(opts))
	if derr != nil {
		fmt.Fprintf(w, "  WARN %s: created but deploy failed (after retries): %v\n", stem, derr)
		fmt.Fprintf(w, "        → run `push rules-deploy` to enable it manually\n")
		dep = nil
	} else if opts.Enabled && opts.RunFrequency == "LIVE" && dep != nil && !dep.Enabled {
		effective := dep.RunFrequency
		if effective == "" || effective == "LIVE" {
			effective = "HOURLY"
		}
		alerting := opts.Alerting
		if redep, rderr := c.UpdateRuleDeployment(ctx, ruleID, chronicle.RuleDeploymentUpdate{
			Enabled:      new(true),
			Alerting:     &alerting,
			RunFrequency: effective,
		}); rderr != nil {
			fmt.Fprintf(w, "  WARN %s: re-deploy at %s failed: %v\n", stem, effective, rderr)
		} else {
			dep = redep
		}
	}

	display := rule.DisplayName
	if display == "" {
		display = stem
	}
	severity := ""
	if rule.Severity != nil {
		severity = rule.Severity.DisplayName
	}
	comp := ruleCompanion{
		DisplayName:           display,
		RuleID:                ruleID,
		Name:                  rule.Name,
		Etag:                  rule.Etag,
		Type:                  rule.Type,
		Severity:              severity,
		AllowedRunFrequencies: rule.AllowedRunFrequencies,
		TimeWindowDuration:    rule.TimeWindowDuration,
		Deployment: deploymentMeta{
			Enabled:      opts.Enabled,
			Alerting:     opts.Alerting,
			Archived:     false,
			RunFrequency: opts.RunFrequency,
		},
	}
	if dep != nil {
		comp.Deployment = deploymentMeta{
			Name:           dep.Name,
			Enabled:        dep.Enabled,
			Alerting:       dep.Alerting,
			Archived:       dep.Archived,
			RunFrequency:   dep.RunFrequency,
			ExecutionState: dep.ExecutionState,
		}
	}

	yamlPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".yaml"
	if werr := comp.write(yamlPath); werr != nil {
		// The rule IS live; failing to record it locally is a real problem, but
		// don't lose the live state silently — report and keep going.
		fmt.Fprintf(w, "  WARN %s: created live but companion write failed: %v\n", stem, werr)
	}
	disabled := landedDisabled(opts, dep)
	note := ""
	if disabled {
		note = "  [DISABLED — platform guard; run `push rules-deploy` to enable]"
	}
	fmt.Fprintf(w, "  created  %s  (%s)%s\n", display, ruleID, note)
	return disabled, nil
}

// retryDeploy retries UpdateRuleDeployment with exponential backoff. A freshly-
// created rule may 400 because the server hasn't finished indexing it yet.
func retryDeploy(ctx context.Context, c *chronicle.Client, ruleID string, update chronicle.RuleDeploymentUpdate) (*chronicle.RuleDeployment, error) {
	backoff := [3]time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	var lastErr error
	for attempt := range len(backoff) + 1 {
		dep, err := c.UpdateRuleDeployment(ctx, ruleID, update)
		if err == nil {
			return dep, nil
		}
		lastErr = err
		if attempt < len(backoff) {
			time.Sleep(backoff[attempt])
		}
	}
	return nil, lastErr
}

// landedDisabled reports whether enabled was requested but the live deployment came
// back disabled — a platform complexity/volume guard, not a failure.
func landedDisabled(opts RulesCreateDeploymentOptions, dep *chronicle.RuleDeployment) bool {
	return opts.Enabled && dep != nil && !dep.Enabled
}

// PushRulesDisable disables locally-tracked rules whose companion
// deployment.enabled is currently true. For each it calls
// UpdateRuleDeployment(enabled=false) — alerting and run frequency are left
// untouched — and rewrites the companion .yaml to reflect the disabled state.
//
// On a dry run it prints a preview table and makes no API call. If !assumeYes it
// aborts with a message. Returns the number of rules disabled (0 on dry-run/no
// work).
func PushRulesDisable(ctx context.Context, c *chronicle.Client, rulesDir string, dryRun, assumeYes bool, w io.Writer) (int, error) {
	type target struct {
		path string
		comp *ruleCompanion
	}

	var targets []target
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(w, "Nothing to disable -- no rules directory at %s.\n", rulesDir)
			return 0, nil
		}
		return 0, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(rulesDir, name)
		comp, rerr := readRuleCompanion(path)
		if rerr != nil {
			fmt.Fprintf(w, "  SKIP %s: cannot read companion: %v\n", name, rerr)
			continue
		}
		if comp.Deployment.Enabled {
			targets = append(targets, target{path: path, comp: comp})
		}
	}

	if len(targets) == 0 {
		fmt.Fprintf(w, "Nothing to disable -- no locally tracked rule in %s has deployment.enabled=true.\n", rulesDir)
		return 0, nil
	}

	liveBanner(w, fmt.Sprintf("DISABLE %d live rule(s)", len(targets)))

	fmt.Fprintf(w, "About to disable %d rule(s):\n\n", len(targets))
	fmt.Fprintf(w, "%-3s %-60s %-10s %-14s\n", "#", "Rule", "Severity", "Current freq")
	fmt.Fprintln(w, strings.Repeat("-", 90))
	for i, t := range targets {
		sev := t.comp.Severity
		if sev == "" {
			sev = "-"
		}
		freq := t.comp.Deployment.RunFrequency
		if freq == "" {
			freq = "-"
		}
		fmt.Fprintf(w, "%-3d %-60s %-10s %-14s\n", i+1, truncate(t.comp.DisplayName, 60), sev, freq)
	}
	fmt.Fprintln(w)

	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API calls made. Re-run without --dry-run to apply.")
		return 0, nil
	}
	if !assumeYes {
		fmt.Fprintf(w, "Refusing to disable %d rule(s) without confirmation (pass --yes). Aborted.\n", len(targets))
		return 0, nil
	}

	disabled := 0
	failed := 0
	for _, t := range targets {
		ruleID := t.comp.RuleID
		if ruleID == "" {
			fmt.Fprintf(w, "  SKIP %s: no rule_id in metadata\n", filepath.Base(t.path))
			continue
		}
		dep, derr := c.UpdateRuleDeployment(ctx, ruleID, chronicle.RuleDeploymentUpdate{
			Enabled: new(false),
		})
		if derr != nil {
			failed++
			fmt.Fprintf(w, "  FAIL      %s: %v\n", t.comp.DisplayName, derr)
			continue
		}

		// Reflect the new (disabled) state locally. Prefer the server's echoed
		// deployment where available; otherwise just flip the enabled flag.
		if dep != nil {
			t.comp.Deployment.Name = dep.Name
			t.comp.Deployment.Enabled = dep.Enabled
			t.comp.Deployment.Alerting = dep.Alerting
			t.comp.Deployment.Archived = dep.Archived
			if dep.RunFrequency != "" {
				t.comp.Deployment.RunFrequency = dep.RunFrequency
			}
			t.comp.Deployment.ExecutionState = dep.ExecutionState
		} else {
			t.comp.Deployment.Enabled = false
		}
		if werr := t.comp.write(t.path); werr != nil {
			fmt.Fprintf(w, "  WARN %s: disabled live but companion write failed: %v\n", t.comp.DisplayName, werr)
		}
		disabled++
		fmt.Fprintf(w, "  disabled  %s\n", t.comp.DisplayName)
	}

	fmt.Fprintf(w, "\nDone. %d disabled, %d failed.\n", disabled, failed)
	return disabled, nil
}

// newRuleCandidates returns the sorted *.yaral files in rulesDir that have no
// sibling *.yaml (i.e. rules not yet created in the tenant). A missing directory
// yields no candidates rather than an error.
func newRuleCandidates(rulesDir string) ([]string, error) {
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaral") {
			continue
		}
		yamlSibling := strings.TrimSuffix(e.Name(), ".yaral") + ".yaml"
		if _, statErr := os.Stat(filepath.Join(rulesDir, yamlSibling)); statErr == nil {
			continue // companion exists -> already tracked
		}
		out = append(out, filepath.Join(rulesDir, e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

// stemOf returns the file's base name without its extension.
func stemOf(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// truncate shortens s to at most n runes (rune-safe for multi-byte text).
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
