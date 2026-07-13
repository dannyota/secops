package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

func init() {
	rootCmd.AddCommand(newCuratedCmd())
}

func newCuratedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "curated <verb>",
		Short: "Browse, search, and toggle Google-managed curated rule sets",
		Long: "Curated content is Google-managed: you cannot create or delete rule sets,\n" +
			"only toggle each deployment's `enabled` and `alerting` per precision\n" +
			"(precise|broad).\n\n" +
			"Browse:  categories → rule-sets → rules --set <id> → rule <id>\n" +
			"Search:  search <query> (searches across both rule sets and rules)\n" +
			"Toggle:  set --category C --ruleset R --precision P --enabled/--alerting",
	}
	cmd.AddCommand(newCuratedCategoriesCmd(), newCuratedRuleSetsCmd(), newCuratedSearchCmd(),
		newCuratedRulesCmd(), newCuratedRuleCmd(), newCuratedSetCmd(),
		newCuratedDetectionsCmd(), newCuratedTrendsCmd(), newCuratedEventsCmd())
	return cmd
}

// enrichedRuleSet joins a curated rule set with its category display name and
// deployment state (both precisions), giving a single unified view.
type enrichedRuleSet struct {
	ID              string `json:"id"`
	CategoryID      string `json:"categoryId"`
	CategoryName    string `json:"categoryName"`
	DisplayName     string `json:"displayName"`
	Description     string `json:"description,omitempty"`
	PreciseEnabled  bool   `json:"preciseEnabled"`
	PreciseAlerting bool   `json:"preciseAlerting"`
	BroadEnabled    bool   `json:"broadEnabled"`
	BroadAlerting   bool   `json:"broadAlerting"`
}

func (e *enrichedRuleSet) isEnabled() bool { return e.PreciseEnabled || e.BroadEnabled }

func (e *enrichedRuleSet) stateLabel() string {
	if e.PreciseEnabled && e.BroadEnabled {
		return "ENABLED"
	}
	if e.PreciseEnabled || e.BroadEnabled {
		return "PARTIAL"
	}
	return "DISABLED"
}

// curatedCategoryRow is the summary for one category in `curated categories`.
type curatedCategoryRow struct {
	ID           string `json:"id"`
	DisplayName  string `json:"displayName"`
	SetCount     int    `json:"setCount"`
	EnabledCount int    `json:"enabledCount"`
}

// loadEnrichedRuleSets joins categories, rule sets, and deployments into a
// single enriched view. Three API calls: categories (1), rule sets via wildcard
// (1), and deployments (1).
func loadEnrichedRuleSets(ctx context.Context, c *chronicle.Client) ([]enrichedRuleSet, []curatedCategoryRow, error) {
	cats, err := c.ListCuratedRuleSetCategories(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("categories: %w", err)
	}
	catNames := make(map[string]string, len(cats))
	for _, cat := range cats {
		catNames[lastSegment(cat.Name)] = cat.DisplayName
	}

	rs, err := c.ListCuratedRuleSets(ctx, "-")
	if err != nil {
		return nil, nil, fmt.Errorf("rule sets: %w", err)
	}
	sets := make([]enrichedRuleSet, 0, len(rs))
	catSetCount := map[string]int{}
	for _, s := range rs {
		catID := catIDFromSetName(s.Name)
		catSetCount[catID]++
		sets = append(sets, enrichedRuleSet{
			ID: lastSegment(s.Name), CategoryID: catID,
			CategoryName: catNames[catID], DisplayName: s.DisplayName,
			Description: s.Description,
		})
	}

	deps, err := c.ListCuratedRuleSetDeployments(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("deployments: %w", err)
	}
	type depKey struct{ cat, set, prec string }
	depByKey := make(map[depKey]*chronicle.CuratedRuleSetDeployment, len(deps))
	for i := range deps {
		cat, set, prec, perr := chronicle.ParseCuratedDeploymentName(deps[i].Name)
		if perr != nil {
			continue
		}
		depByKey[depKey{cat, set, prec}] = &deps[i]
	}
	for i := range sets {
		s := &sets[i]
		if d, ok := depByKey[depKey{s.CategoryID, s.ID, "precise"}]; ok {
			s.PreciseEnabled = d.Enabled
			s.PreciseAlerting = d.Alerting
		}
		if d, ok := depByKey[depKey{s.CategoryID, s.ID, "broad"}]; ok {
			s.BroadEnabled = d.Enabled
			s.BroadAlerting = d.Alerting
		}
	}

	catEnabled := map[string]int{}
	for i := range sets {
		if sets[i].isEnabled() {
			catEnabled[sets[i].CategoryID]++
		}
	}
	catRows := make([]curatedCategoryRow, 0, len(cats))
	for _, cat := range cats {
		catID := lastSegment(cat.Name)
		catRows = append(catRows, curatedCategoryRow{
			ID: catID, DisplayName: cat.DisplayName,
			SetCount: catSetCount[catID], EnabledCount: catEnabled[catID],
		})
	}

	return sets, catRows, nil
}

// catIDFromSetName extracts the category ID from a rule-set resource name
// (.../curatedRuleSetCategories/{catID}/curatedRuleSets/{setID}).
func catIDFromSetName(name string) string {
	const marker = "curatedRuleSetCategories/"
	_, after, ok := strings.Cut(name, marker)
	if !ok {
		return ""
	}
	cat, _, _ := strings.Cut(after, "/")
	return cat
}

// matchCategory returns true when q matches a category by UUID or
// case-insensitive display name substring.
func matchCategory(catID, catName, q string) bool {
	if strings.EqualFold(catID, q) {
		return true
	}
	return strings.Contains(strings.ToLower(catName), strings.ToLower(q))
}

func newCuratedCategoriesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "categories",
		Short: "Read-only: show curated rule-set categories with set and enabled counts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			_, catRows, err := loadEnrichedRuleSets(baseContext(), c)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSONValue(os.Stdout, catRows)
			}
			w := os.Stdout
			totalSets, totalEnabled := 0, 0
			for _, cr := range catRows {
				fmt.Fprintf(w, "%-55s %3d sets  %3d enabled\n", cr.DisplayName, cr.SetCount, cr.EnabledCount)
				totalSets += cr.SetCount
				totalEnabled += cr.EnabledCount
			}
			fmt.Fprintf(w, "\n%d categories, %d rule sets (%d enabled)\n", len(catRows), totalSets, totalEnabled)
			return nil
		},
	}
	return markJSON(cmd)
}

func newCuratedSetCmd() *cobra.Command {
	var (
		category, ruleSet, precision string
		enabled, alerting            bool
		dryRun, yes                  bool
	)
	cmd := &cobra.Command{
		Use:   "set --category C --ruleset R --precision precise|broad [--enabled[=bool]] [--alerting[=bool]]",
		Short: "MUTATING (guarded): toggle a curated deployment's enabled/alerting",
		Long: "Toggle one Google-managed curated rule-set deployment, addressed by\n" +
			"--category / --ruleset / --precision (precise|broad). The toggles are\n" +
			"tri-state: a flag you omit is left unchanged; `--enabled` enables and\n" +
			"`--enabled=false` disables the deployment; `--alerting` / `--alerting=false`\n" +
			"controls alerting independently of enablement. Guarded: dry-run by default,\n" +
			"--yes to apply against the live tenant.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Validate the precision up front — before the LIVE-deploy banner — so a
			// bad value fails fast rather than after the guard prints.
			if err := chronicle.ValidateCuratedPrecision(precision); err != nil {
				return err
			}
			upd := chronicle.CuratedDeploymentUpdate{}
			if cmd.Flags().Changed("enabled") {
				upd.Enabled = &enabled
			}
			if cmd.Flags().Changed("alerting") {
				upd.Alerting = &alerting
			}
			if upd.Enabled == nil && upd.Alerting == nil {
				return fmt.Errorf("nothing to set: pass --enabled and/or --alerting")
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			// Blast-radius preview: show the current → requested state of THIS
			// precision deployment and note the sibling precision is untouched, so the
			// operator sees exactly what a set×precision flip changes before --yes.
			// Best-effort (a read) — a lookup failure never blocks the toggle.
			if !jsonOut {
				printCuratedBlastRadius(baseContext(), c, category, ruleSet, precision, upd)
			}
			action := fmt.Sprintf("curated set %s/%s/%s -> %s", category, ruleSet, precision, describeCuratedUpd(upd))
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				_, err = c.UpdateCuratedRuleSetDeployment(baseContext(), category, ruleSet, precision, upd)
				return err
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&category, "category", "", "curated rule-set category id (required)")
	f.StringVar(&ruleSet, "ruleset", "", "curated rule-set id (required)")
	f.StringVar(&precision, "precision", "", "precise|broad (required)")
	f.BoolVar(&enabled, "enabled", false, "enable the deployment (--enabled=false disables); omit to leave unchanged")
	f.BoolVar(&alerting, "alerting", false, "turn alerting on (--alerting=false off); independent of --enabled; omit to leave unchanged")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("category")
	_ = cmd.MarkFlagRequired("ruleset")
	_ = cmd.MarkFlagRequired("precision")
	return markJSON(cmd)
}

// printCuratedBlastRadius previews what a `curated set` toggle changes: the
// current enabled/alerting state of the addressed precision deployment, the
// requested transition, and a reminder that a curated deployment is set ×
// precision (so the sibling precision is unaffected and per-rule control does not
// exist — a platform limit). Best-effort: any lookup error is a note, not a stop.
func printCuratedBlastRadius(ctx context.Context, c *chronicle.Client, category, ruleSet, precision string, upd chronicle.CuratedDeploymentUpdate) {
	deps, err := c.ListCuratedRuleSetDeployments(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: could not read current curated state (%v); preview shows the requested change only\n", err)
		return
	}
	var cur *chronicle.CuratedRuleSetDeployment
	for i := range deps {
		cat, rs, prec, perr := chronicle.ParseCuratedDeploymentName(deps[i].Name)
		if perr == nil && cat == category && rs == ruleSet && strings.EqualFold(prec, precision) {
			cur = &deps[i]
			break
		}
	}
	// To stderr: a preview aid, kept off stdout so a --json caller's stdout stays
	// clean (it is also gated on !jsonOut at the call site).
	w := os.Stderr
	fmt.Fprintf(w, "blast radius — curated deployment %s/%s/%s:\n", category, ruleSet, precision)
	if cur == nil {
		fmt.Fprintln(w, "  current: (deployment not found in the live list; the toggle creates/sets it)")
	} else {
		fmt.Fprintf(w, "  current: enabled=%v alerting=%v\n", cur.Enabled, cur.Alerting)
		transition := func(field string, from bool, to *bool) {
			if to != nil && *to != from {
				fmt.Fprintf(w, "  change : %s %v -> %v\n", field, from, *to)
			}
		}
		transition("enabled", cur.Enabled, upd.Enabled)
		transition("alerting", cur.Alerting, upd.Alerting)
	}
	fmt.Fprintln(w, "  scope  : a curated deployment is set × precision — the other precision and per-rule")
	fmt.Fprintln(w, "           state are unaffected (no per-rule toggle exists). See `curated trends` /")
	fmt.Fprintln(w, "           `curated detections` for this set's recent detection volume.")
}

// guardedSIEMMutation runs a live SIEM mutation behind the standard guard: a LIVE
// banner + the action, dry-run by default, --yes (or an interactive confirm) to
// apply. Mirrors the SOAR `soar case` guard for the SIEM side. In read-only mode
// every mutation degrades to a dry run; confirmed mutations are audit-logged
// (the decision core is the shared deriveGuard).
func guardedSIEMMutation(action string, dryRunFlag, yesFlag bool, do func() error) error {
	dryRun, assumeYes := deriveGuard(action, dryRunFlag, yesFlag)
	// In --json mode, emit a single structured result instead of the human banner
	// so stdout stays valid JSON (confirmPush already declines to prompt under
	// --json). Callers' do() must not print to stdout in this mode.
	if jsonOut {
		switch {
		case dryRun:
			return emitGuardedResult(action, true, false)
		case !assumeYes:
			return emitGuardedResult(action, false, false)
		default:
			if err := do(); err != nil {
				return err
			}
			return emitGuardedResult(action, false, true)
		}
	}
	w := os.Stdout
	bar := strings.Repeat("!", 72)
	fmt.Fprintln(w, bar)
	fmt.Fprintln(w, "!! LIVE SIEM change against a PRODUCTION tenant !!")
	fmt.Fprintf(w, "!! Action: %s\n", action)
	fmt.Fprintln(w, bar)
	if dryRun {
		fmt.Fprintln(w, "DRY RUN — no changes applied. Re-run with --yes to apply.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintf(w, "Refusing to %s without confirmation (pass --yes). Aborted.\n", strings.ToLower(action))
		return nil
	}
	if err := do(); err != nil {
		return err
	}
	fmt.Fprintf(w, "Done: %s.\n", action)
	return nil
}

func describeCuratedUpd(upd chronicle.CuratedDeploymentUpdate) string {
	var parts []string
	if upd.Enabled != nil {
		parts = append(parts, fmt.Sprintf("enabled=%v", *upd.Enabled))
	}
	if upd.Alerting != nil {
		parts = append(parts, fmt.Sprintf("alerting=%v", *upd.Alerting))
	}
	return strings.Join(parts, " ")
}

// lastSegment returns the trailing path segment of a slash-delimited resource
// name (e.g. ".../curatedRuleSets/<id>" -> "<id>").
func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// writeJSONValue pretty-prints any value as JSON to w.
func writeJSONValue(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
