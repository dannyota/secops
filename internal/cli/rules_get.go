package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// newRulesGetCmd shows the CURRENT (latest) version of one rule — its running
// state (is it live/alerting/archived, does it compile) plus the YARA-L — without
// having to address a revision. <rule> is an id, display name, or slug. Read-only.
func newRulesGetCmd() *cobra.Command {
	var textOnly bool
	cmd := &cobra.Command{
		Use:   "get <rule>",
		Short: "Read-only: show a rule's current state + YARA-L (is it running, and what it says)",
		Long: "Fetch the latest version of a rule and show whether it is running (enabled/\n" +
			"alerting/archived, compile + execution state, severity, MITRE) followed by its\n" +
			"YARA-L. <rule> is an id, display name, or slug. --text prints just the YARA-L\n" +
			"(raw, for piping into `rules promote` or a diff); --json emits the full rule +\n" +
			"deployment.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			ruleID, err := resolveRuleID(ctx, c, args[0])
			if err != nil {
				return err
			}
			rule, err := c.GetRule(ctx, ruleID)
			if err != nil {
				return err
			}
			// Deployment carries archived / execution state / run frequency (not on
			// the rule); best-effort so a rule without one still shows its content.
			dep, _ := c.GetRuleDeployment(ctx, ruleID)

			if textOnly {
				fmt.Fprint(os.Stdout, rule.Text)
				return nil
			}
			if jsonOut {
				return emitJSON(map[string]any{"rule": rule, "deployment": dep})
			}

			running := rule.LiveModeEnabled
			archived := false
			if dep != nil {
				running = running || dep.Enabled
				archived = dep.Archived
			}
			state := "NOT running (disabled)"
			switch {
			case archived:
				state = "NOT running (archived)"
			case running:
				state = "RUNNING (enabled)"
			}

			w := os.Stdout
			fmt.Fprintf(w, "%s (%s)\n", orDash(rule.DisplayName), ruleID)
			fmt.Fprintf(w, "  status:    %s\n", state)
			fmt.Fprintf(w, "  alerting:  %v\n", rule.AlertingEnabled || (dep != nil && dep.Alerting))
			if rule.Severity != nil && rule.Severity.DisplayName != "" {
				fmt.Fprintf(w, "  severity:  %s\n", rule.Severity.DisplayName)
			}
			fmt.Fprintf(w, "  compile:   %s\n", orDash(rule.CompilationState))
			if dep != nil {
				fmt.Fprintf(w, "  exec:      %s\n", orDash(dep.ExecutionState))
				fmt.Fprintf(w, "  frequency: %s\n", orDash(orFirst(dep.RunFrequency, rule.RunFrequency)))
			}
			if rule.Author != "" {
				fmt.Fprintf(w, "  author:    %s\n", rule.Author)
			}
			if mitre := mitreLine(rule.MitreTactics(), rule.MitreTechniques()); mitre != "" {
				fmt.Fprintf(w, "  mitre:     %s\n", mitre)
			}
			fmt.Fprintf(w, "  revision:  %s\n", orFirst(rule.RevisionID, revisionToken(rule.Name)))
			fmt.Fprintln(w, strings.Repeat("-", 60))
			fmt.Fprint(w, rule.Text)
			if !strings.HasSuffix(rule.Text, "\n") {
				fmt.Fprintln(w)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&textOnly, "text", false, "print only the YARA-L text (raw, pipe-friendly)")
	return markJSON(cmd)
}

// mitreLine renders tactics / techniques as "TA…, TA… / T…, T…" (or "").
func mitreLine(tactics, techniques []string) string {
	switch {
	case len(tactics) == 0 && len(techniques) == 0:
		return ""
	case len(techniques) == 0:
		return strings.Join(tactics, ", ")
	case len(tactics) == 0:
		return strings.Join(techniques, ", ")
	default:
		return strings.Join(tactics, ", ") + " / " + strings.Join(techniques, ", ")
	}
}
