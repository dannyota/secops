package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror"
)

// The `rules` command is the operational + lifecycle read surface for detection
// rules: detections / errors / alerts a rule produced, and retrohunt management.
// Reads are free; retrohunt-create is a guarded production action (it runs the
// rule over historical data). Rule config-as-code lives under `pull rules` /
// `push rules-*`.

func init() {
	rootCmd.AddCommand(newRulesCmd())
}

func newRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules <verb>",
		Short: "Inspect rule output (detections/errors/alerts) and manage retrohunts",
		Long: "Operational reads over a deployed rule plus retrohunt management. Rule\n" +
			"config-as-code is `pull rules` / `push rules-create|update|deploy|disable`.",
	}
	cmd.AddCommand(
		newRulesListCmd(),
		newRulesValidateCmd(),
		newRulesTestCmd(),
		newRulesVersionsCmd(),
		newRulesDetectionsCmd(),
		newRulesErrorsCmd(),
		newRulesAlertsCmd(),
		newRulesRetrohuntCmd(),
		newRulesTrendsCmd(),
		newRulesCountsCmd(),
		newRulesEventsCmd(),
		newRulesPromoteCmd(),
	)
	return cmd
}

// newRulesListCmd lists detection rules with the ids the inspect verbs need —
// mapping a display name / slug back to the ru_ rule id without opening files.
func newRulesListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Read-only: list detection rules (rule id, display name, slug)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			rules, err := c.ListRules(baseContext())
			if err != nil {
				return err
			}
			type row struct {
				RuleID      string `json:"rule_id"`
				DisplayName string `json:"display_name"`
				Slug        string `json:"slug"`
				Type        string `json:"type,omitempty"`
			}
			rows := make([]row, 0, len(rules))
			for i := range rules {
				r := &rules[i]
				rows = append(rows, row{RuleID: r.RuleID(), DisplayName: r.DisplayName, Slug: mirror.Slugify(r.DisplayName), Type: r.Type})
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].DisplayName < rows[j].DisplayName })
			if jsonOut {
				return emitJSON(rows)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "RULE ID\tDISPLAY NAME\tSLUG\tTYPE")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.RuleID, r.DisplayName, r.Slug, r.Type)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Printf("\n%d rule(s).\n", len(rows))
			return nil
		},
	}
	return markJSON(cmd)
}

// newRulesValidateCmd validates a local YARA-L file against the API without
// creating or changing anything — a fast pre-push syntax check.
func newRulesValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <file.yaral>",
		Short: "Validate a YARA-L file against the API (no mutation)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			res, err := c.ValidateRule(baseContext(), string(text))
			if err != nil {
				return err
			}
			ok := res != nil && res.Success
			msg := ""
			if res != nil {
				msg = res.Message
			}
			if jsonOut {
				if jerr := emitJSON(struct {
					File    string `json:"file"`
					Valid   bool   `json:"valid"`
					Message string `json:"message,omitempty"`
				}{File: args[0], Valid: ok, Message: msg}); jerr != nil {
					return jerr
				}
				if !ok {
					return fmt.Errorf("rule is invalid")
				}
				return nil
			}
			if ok {
				fmt.Printf("OK: %s is valid YARA-L.\n", args[0])
				return nil
			}
			return fmt.Errorf("invalid: %s", msg)
		},
	}
	return markJSON(cmd)
}

// timeWindow returns [now-hours, now] in UTC (default 24h when hours <= 0).
func timeWindow(hours int) (time.Time, time.Time) {
	if hours <= 0 {
		hours = 24
	}
	end := time.Now().UTC()
	return end.Add(-time.Duration(hours) * time.Hour), end
}

// checkHours rejects a non-positive --hours look-back before any work, so a
// typo (`--hours 0`, `--hours -1`) fails fast with a clear message instead of
// silently falling back to the 24h default.
func checkHours(hours int) error {
	if hours <= 0 {
		return fmt.Errorf("--hours must be a positive number of hours")
	}
	return nil
}

func newRulesDetectionsCmd() *cobra.Command {
	var (
		hours, limit int
		state        string
	)
	cmd := &cobra.Command{
		Use:   "detections <rule>",
		Short: "Read-only: list detections a rule produced in a time window",
		Long: "List the detections a rule produced over the last --hours.\n" +
			"<rule> is a rule id, display name, or slug as shown by `rules list`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkHours(hours); err != nil {
				return err
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			start, end := timeWindow(hours)
			ruleID, err := resolveRuleID(baseContext(), c, args[0])
			if err != nil {
				return err
			}
			dets, err := c.ListDetections(baseContext(), ruleID, start, end, state, limit)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSONValue(os.Stdout, dets)
			}
			if len(dets) == 0 {
				fmt.Fprintln(os.Stdout, "no detections.")
				return nil
			}
			fmt.Fprintf(os.Stdout, "%-34s %-22s %s\n", "ID", "DETECTION-TIME", "TYPE")
			for i := range dets {
				d := &dets[i]
				fmt.Fprintf(os.Stdout, "%-34s %-22s %s\n", truncate(orDash(d.ID), 34), orDash(d.DetectionTime), orDash(d.Type))
			}
			fmt.Fprintf(os.Stdout, "\n%d detection(s).\n", len(dets))
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 24, "look-back window in hours")
	f.IntVar(&limit, "limit", 100, "max detections (page size)")
	f.StringVar(&state, "state", "", "filter by alert state (e.g. ALERTING)")
	return markJSON(cmd)
}

// resolveRuleID maps a rule reference — the full `ru_<uuid>` id, a short `ru_`
// prefix, the display name, or the slug — to the full rule id the inspection APIs
// require, by matching the live rule list. It mirrors what `push rules-deploy
// --rule` accepts, so an operator can pass whatever `rules list` / `pull rules`
// show. A full-id form is returned even if it is not in the list (e.g. an archived
// rule); an unknown reference yields a clean client-side error instead of the
// API's opaque "invalid rule name in filter" 400.
func resolveRuleID(ctx context.Context, c *chronicle.Client, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("a rule id, display name, or slug is required")
	}
	// A full ru_<uuid> id is unambiguous — pass it through without fetching the
	// rule list (the API validates it anyway, and an archived rule may not be
	// listed at all).
	if looksLikeRuleID(ref) {
		return ref, nil
	}
	// Name/slug resolution needs the list; BASIC view — the matcher needs no text.
	rules, err := c.ListRulesBasic(ctx)
	if err != nil {
		return "", err
	}
	return matchRuleID(rules, ref)
}

// matchRuleID resolves a non-empty rule reference against a known rule set: an
// exact id/display-name/slug match, then a unique short `ru_` id prefix, then a
// full-id-shape passthrough for an unlisted (e.g. archived) rule. Pure, so the
// matching is unit-tested without a live client.
func matchRuleID(rules []chronicle.Rule, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	low := strings.ToLower(ref)
	// Exact match on id / display name / slug. Collect DISTINCT rule ids so a
	// display name shared by two rules is reported as ambiguous, not silently
	// resolved to whichever came first.
	var exact []string
	seen := map[string]bool{}
	for i := range rules {
		r := &rules[i]
		for _, cand := range []string{r.RuleID(), r.DisplayName, mirror.Slugify(r.DisplayName)} {
			if cand != "" && strings.ToLower(strings.TrimSpace(cand)) == low {
				if id := r.RuleID(); id != "" && !seen[id] {
					seen[id] = true
					exact = append(exact, id)
				}
				break
			}
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return "", fmt.Errorf("%q matches %d rules — use the full id (see `rules list`)", ref, len(exact))
	}
	// A short `ru_` prefix (e.g. a truncated id) resolves if exactly one id matches.
	if strings.HasPrefix(low, "ru_") {
		var matches []string
		for i := range rules {
			if id := rules[i].RuleID(); strings.HasPrefix(strings.ToLower(id), low) {
				matches = append(matches, id)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return "", fmt.Errorf("rule id prefix %q is ambiguous (%d matches) — use the full id (see `rules list`)", ref, len(matches))
		}
	}
	if looksLikeRuleID(ref) {
		return ref, nil // valid id shape, just not listed (e.g. archived) — let the API try
	}
	return "", fmt.Errorf("no rule matches %q (see `secopsctl rules list`)", ref)
}

// looksLikeRuleID reports whether s has the full rule-id shape ru_<uuid>.
func looksLikeRuleID(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(strings.ToLower(s), "ru_") && len(s) == 39 && strings.Count(s, "-") == 4
}

func newRulesErrorsCmd() *cobra.Command {
	var hours int
	cmd := &cobra.Command{
		Use:   "errors <rule>",
		Short: "Read-only: list execution errors a rule produced in a time window",
		Long: "List the execution errors a rule produced over the last --hours.\n" +
			"<rule> is a rule id, display name, or slug as shown by `rules list`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkHours(hours); err != nil {
				return err
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			start, end := timeWindow(hours)
			ruleID, err := resolveRuleID(baseContext(), c, args[0])
			if err != nil {
				return err
			}
			errs, err := c.ListErrors(baseContext(), ruleID, start, end)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSONValue(os.Stdout, errs)
			}
			if len(errs) == 0 {
				fmt.Fprintln(os.Stdout, "no errors.")
				return nil
			}
			for i := range errs {
				e := &errs[i]
				fmt.Fprintf(os.Stdout, "%-22s %-26s %s\n", orDash(e.ErrorTime), orDash(e.Category),
					truncate(e.Message(), 80))
			}
			fmt.Fprintf(os.Stdout, "\n%d error(s).\n", len(errs))
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 24, "look-back window in hours")
	return markJSON(cmd)
}

func newRulesAlertsCmd() *cobra.Command {
	var hours int
	cmd := &cobra.Command{
		Use:   "alerts <rule-id>",
		Short: "Read-only: search alerts a rule generated (raw, rule-dependent shape)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			start, end := timeWindow(hours)
			ruleID, err := resolveRuleID(baseContext(), c, args[0])
			if err != nil {
				return err
			}
			res, err := c.SearchRuleAlerts(baseContext(), ruleID, start, end)
			if err != nil {
				return err
			}
			if res.TooManyAlerts {
				fmt.Fprintln(os.Stderr, "warning: too many alerts — narrow the window with --hours")
			}
			// Alert bodies are deeply nested and rule-dependent — emit raw.
			return writeRulesAlerts(os.Stdout, res.RuleAlerts)
		},
	}
	cmd.Flags().IntVar(&hours, "hours", 24, "look-back window in hours")
	// Output is ALWAYS raw JSON regardless of --json (rule-dependent alert shape).
	return markJSON(cmd)
}

func newRulesRetrohuntCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retrohunt <verb>",
		Short: "Manage retrohunts (run a rule over historical data)",
	}
	cmd.AddCommand(newRetrohuntListCmd(), newRetrohuntGetCmd(), newRetrohuntCreateCmd())
	return cmd
}

func newRetrohuntListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <rule>",
		Short: "Read-only: list a rule's retrohunts (accepts id, display name, or slug)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ruleID, err := resolveRuleID(baseContext(), c, args[0])
			if err != nil {
				return err
			}
			rhs, err := c.ListRetrohunts(baseContext(), ruleID)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSONValue(os.Stdout, rhs)
			}
			if len(rhs) == 0 {
				fmt.Fprintln(os.Stdout, "no retrohunts.")
				return nil
			}
			fmt.Fprintf(os.Stdout, "%-40s %-14s %s\n", "RETROHUNT", "STATE", "PROGRESS")
			for i := range rhs {
				r := &rhs[i]
				fmt.Fprintf(os.Stdout, "%-40s %-14s %.1f%%\n", lastSegment(r.Name), orDash(r.State), r.ProgressPercentage)
			}
			return nil
		},
	}
	return markJSON(cmd)
}

func newRetrohuntGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <rule> <retrohunt-id>",
		Short: "Read-only: get one retrohunt's status (rule accepts id, name, or slug)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ruleID, err := resolveRuleID(baseContext(), c, args[0])
			if err != nil {
				return err
			}
			rh, err := c.GetRetrohunt(baseContext(), ruleID, args[1])
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, rh.Raw)
			}
			fmt.Fprintf(os.Stdout, "retrohunt %s\n  state:    %s\n  progress: %.1f%%\n",
				lastSegment(rh.Name), orDash(rh.State), rh.ProgressPercentage)
			return nil
		},
	}
	return markJSON(cmd)
}

func newRetrohuntCreateCmd() *cobra.Command {
	var (
		hours       int
		wait        bool
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "create <rule>",
		Short: "MUTATING (guarded): start a retrohunt over the last --hours of data (rule accepts id, name, or slug)",
		Long: "Re-run a rule over the last --hours of already-stored data to surface\n" +
			"detections it would have produced. The retrohunt runs asynchronously —\n" +
			"poll its progress with `rules retrohunt get`/`rules retrohunt list`.\n" +
			"<rule> is a rule id, display name, or slug as shown by `rules list`.\n" +
			"Guarded: dry-run by default, --yes to apply against the live tenant.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkHours(hours); err != nil {
				return err
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ruleID, err := resolveRuleID(baseContext(), c, args[0])
			if err != nil {
				return err
			}
			start, end := timeWindow(hours)
			action := fmt.Sprintf("retrohunt %s over the last %dh", ruleID, hoursOrDefault(hours))
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				rh, err := c.CreateRetrohunt(baseContext(), ruleID, start, end)
				if err != nil {
					return err
				}
				rhID := lastSegment(rh.Name)
				if !jsonOut {
					fmt.Fprintf(os.Stdout, "started retrohunt %s (state %s)\n", rhID, orDash(rh.State))
				}
				if !wait {
					return nil
				}
				// Poll to completion, then print the final record (detections are
				// then readable with `rules detections`). Bounded so a stuck run
				// fails fast rather than hanging.
				const maxPolls, interval = 120, 5 * time.Second
				for i := range maxPolls {
					got, gerr := c.GetRetrohunt(baseContext(), ruleID, rhID)
					if gerr != nil {
						return gerr
					}
					if got.State != "RUNNING" && got.State != "STATE_UNSPECIFIED" && got.State != "" {
						if jsonOut {
							return emitJSON(got)
						}
						fmt.Fprintf(os.Stdout, "retrohunt %s finished: state %s — read matches with `rules detections %s --hours %d`\n",
							rhID, got.State, ruleID, hoursOrDefault(hours))
						return nil
					}
					if i > 0 && i%6 == 0 {
						fmt.Fprintf(os.Stderr, "still running… (%s elapsed)\n", time.Duration(i)*interval)
					}
					time.Sleep(interval)
				}
				return fmt.Errorf("retrohunt %s did not finish after %s — poll with `rules retrohunt get`", rhID, maxPolls*interval)
			})
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 168, "look-back window in hours (default 7d)")
	f.BoolVar(&wait, "wait", false, "poll until the retrohunt finishes, then report its final state")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

// --- helpers ----------------------------------------------------------------

func hoursOrDefault(h int) int {
	if h <= 0 {
		return 24
	}
	return h
}

// orFirst returns the first non-empty string.
func orFirst(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// writeRulesAlerts emits the raw rule-alert array as a JSON array.
func writeRulesAlerts(w io.Writer, alerts []json.RawMessage) error {
	if len(alerts) == 0 {
		fmt.Fprintln(w, "no alerts.")
		return nil
	}
	b, err := json.MarshalIndent(alerts, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
