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

// The `curated` command is the imperative lane for Google-managed curated content:
// the catalog is fixed (no create/delete), so the only writes are toggling a
// deployment's `enabled` and `alerting` booleans per (category, rule set,
// precision). `list` reads; `set` is a guarded production mutation (dry-run by
// default, --yes to apply) — every toggle changes live detection/alerting.

func init() {
	rootCmd.AddCommand(newCuratedCmd())
}

func newCuratedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "curated <verb>",
		Short: "Read and toggle Google-managed curated rule-set deployments (enable/alerting)",
		Long: "Curated content is Google-managed: you cannot create or delete rule sets,\n" +
			"only toggle each deployment's `enabled` and `alerting` per precision\n" +
			"(precise|broad). `list` reads; `set` is a guarded live toggle.",
	}
	cmd.AddCommand(newCuratedListCmd(), newCuratedSetCmd(), newCuratedRulesCmd())
	return cmd
}

// newCuratedRulesCmd lists the individual Google-managed curated rules (distinct
// from the rule SETS that `list` shows). Read-only.
func newCuratedRulesCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Read-only: list the individual curated (Google-managed) rules",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			rules, err := c.ListCuratedRules(baseContext())
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSONValue(os.Stdout, rules)
			}
			for i, r := range rules {
				sev := ""
				if r.Severity != nil {
					sev = r.Severity.DisplayName
				}
				fmt.Fprintf(os.Stdout, "%4d  %-28s %-10s %s\n", i+1, r.ID, sev, r.DisplayName)
			}
			fmt.Fprintf(os.Stdout, "\n%d curated rule(s)\n", len(rules))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, jsonFlagHelp)
	return cmd
}

// curatedRow is a flattened deployment for display/JSON: identity ids + the two
// toggles + the rule-set display name.
type curatedRow struct {
	Category  string `json:"category"`
	RuleSet   string `json:"ruleSet"`
	Precision string `json:"precision"`
	Enabled   bool   `json:"enabled"`
	Alerting  bool   `json:"alerting"`
	Display   string `json:"display"`
}

func newCuratedListCmd() *cobra.Command {
	var (
		filter string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Read-only: list curated rule-set deployments (with their enable/alerting state)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			rows, err := curatedRows(baseContext(), c)
			if err != nil {
				return err
			}
			if filter != "" {
				rows = filterCuratedRows(rows, filter)
			}
			if asJSON {
				return writeJSONValue(os.Stdout, rows)
			}
			return emitCuratedRows(os.Stdout, rows)
		},
	}
	f := cmd.Flags()
	f.StringVar(&filter, "filter", "", "case-insensitive substring filter on the rule-set display name")
	f.BoolVar(&asJSON, "json", false, jsonFlagHelp)
	return cmd
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
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			action := fmt.Sprintf("curated set %s/%s/%s -> %s", category, ruleSet, precision, describeCuratedUpd(upd))
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				_, err = c.UpdateCuratedRuleSetDeployment(baseContext(), category, ruleSet, precision, upd)
				return err
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&category, "category", "", "curated rule-set category id (required)")
	f.StringVar(&ruleSet, "ruleset", "", "curated rule-set id (required)")
	f.StringVar(&precision, "precision", "", "precise|broad (required)")
	f.BoolVar(&enabled, "enabled", false, "set the deployment enabled state (only applied if the flag is present)")
	f.BoolVar(&alerting, "alerting", false, "set the deployment alerting state (only applied if the flag is present)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("category")
	_ = cmd.MarkFlagRequired("ruleset")
	_ = cmd.MarkFlagRequired("precision")
	return cmd
}

// curatedRows lists every deployment and joins it with category/rule-set display
// names for a readable view.
func curatedRows(ctx context.Context, c *chronicle.Client) ([]curatedRow, error) {
	cats, err := c.ListCuratedRuleSetCategories(ctx)
	if err != nil {
		return nil, err
	}
	display := map[string]string{} // ruleSetID -> display name
	for _, cat := range cats {
		catID := lastSegment(cat.Name)
		sets, serr := c.ListCuratedRuleSets(ctx, catID)
		if serr != nil {
			fmt.Fprintf(os.Stderr, "warning: list rule sets for %s: %v\n", cat.DisplayName, serr)
			continue
		}
		for _, rs := range sets {
			display[lastSegment(rs.Name)] = rs.DisplayName
		}
	}

	deps, err := c.ListCuratedRuleSetDeployments(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]curatedRow, 0, len(deps))
	for _, d := range deps {
		cat, rs, prec, perr := chronicle.ParseCuratedDeploymentName(d.Name)
		if perr != nil {
			continue
		}
		rows = append(rows, curatedRow{
			Category: cat, RuleSet: rs, Precision: prec,
			Enabled: d.Enabled, Alerting: d.Alerting, Display: display[rs],
		})
	}
	return rows, nil
}

func filterCuratedRows(rows []curatedRow, sub string) []curatedRow {
	sub = strings.ToLower(sub)
	var out []curatedRow
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.Display), sub) {
			out = append(out, r)
		}
	}
	return out
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
		fmt.Fprintln(w, "DRY RUN -- no API call made. Re-run with --yes to apply.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintln(w, "Refusing to act without confirmation (pass --yes). Aborted.")
		return nil
	}
	if err := do(); err != nil {
		return err
	}
	fmt.Fprintf(w, "Done: %s.\n", action)
	return nil
}

func emitCuratedRows(w io.Writer, rows []curatedRow) error {
	if len(rows) == 0 {
		fmt.Fprintln(w, "no curated deployments.")
		return nil
	}
	for i := range rows {
		r := &rows[i]
		fmt.Fprintf(w, "en=%-5v al=%-5v %-8s %s\n", r.Enabled, r.Alerting, r.Precision, orDash(r.Display))
		fmt.Fprintf(w, "    --category %s --ruleset %s --precision %s\n", r.Category, r.RuleSet, r.Precision)
	}
	fmt.Fprintf(w, "\n%d deployment(s).\n", len(rows))
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
