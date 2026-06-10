package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// The `alerts` command operates SIEM detection alerts: reads (the Chronicle
// legacy alert snapshot view, legacyFetchAlertsView) plus the guarded `update`
// feedback verb (status/verdict/priority — the SIEM-side alert disposition).
// The reliable analyst view of a case's alerts is the SOAR lane
// (`soar case get`); this surfaces the Chronicle alert snapshot directly.
// `get` also bridges to the SOAR case id (the id `soar case` verbs need).
// See docs/design/siem.md.

func init() { rootCmd.AddCommand(newAlertsCmd()) }

func newAlertsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alerts <verb>",
		Short: "SIEM detection alerts: list/get (read-only) + guarded feedback update",
		Long: "Query Chronicle detection alerts over a time window (a snapshot view), fetch\n" +
			"one by id, or set triage feedback (status / verdict / priority / comment) on\n" +
			"one or more alerts. `list` and `get` read only; `update` is a guarded live\n" +
			"mutation (dry-run by default, --yes to apply).",
	}
	cmd.AddCommand(newAlertsListCmd(), newAlertsGetCmd(), newAlertsUpdateCmd())
	return cmd
}

func newAlertsListCmd() *cobra.Command {
	var (
		hours    int
		from, to string
		query    string
		limit    int
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List alerts over a time window (snapshot view)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			start, end := timeWindow(hours)
			if from != "" {
				if start, err = parseQueryTS(from); err != nil {
					return err
				}
			}
			if to != "" {
				if end, err = parseQueryTS(to); err != nil {
					return err
				}
			}
			snap, err := c.GetAlerts(baseContext(), start, end, limit, query, "", nil)
			if err != nil {
				return err
			}
			return emitAlerts(os.Stdout, snap)
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 24, "look-back window in hours when --from is not given")
	f.StringVar(&from, "from", "", "explicit start (RFC3339 / ISO-8601); overrides --hours")
	f.StringVar(&to, "to", "", "explicit end (RFC3339 / ISO-8601); default now")
	f.StringVar(&query, "query", "", `snapshot filter (default: feedback_summary.status != "CLOSED")`)
	f.IntVar(&limit, "limit", 100, "max alerts to return (0 = server default)")
	return cmd
}

func newAlertsGetCmd() *cobra.Command {
	var detections bool
	cmd := &cobra.Command{
		Use:   "get <alert-id>",
		Short: "Get one alert by id (resolves its SOAR case id when the alert is cased)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			a, err := c.GetAlert(baseContext(), args[0], detections)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, a.Raw)
			}
			if err := emitAlerts(os.Stdout, &chronicle.AlertsSnapshot{Alerts: []chronicle.Alert{*a}}); err != nil {
				return err
			}
			printAlertCaseBridge(c, a.CaseName)
			return nil
		},
	}
	cmd.Flags().BoolVar(&detections, "detections", false, "include detection details (in --json)")
	return cmd
}

// printAlertCaseBridge resolves the alert's SIEM case uuid to its SOAR integer
// case id (legacyBatchGetCases) and prints the pivot into `soar case`. Fail-soft:
// the bridge is a convenience read, so a resolution error becomes a stderr note
// rather than failing the get.
func printAlertCaseBridge(c *chronicle.Client, caseUUID string) {
	if caseUUID == "" {
		return
	}
	fmt.Fprintf(os.Stdout, "\nCase (SIEM uuid): %s\n", caseUUID)
	resp, err := c.BatchGetCases(baseContext(), []string{caseUUID})
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: SOAR case id not resolved (%v); try `cases soar-id %s`.\n", err, caseUUID)
		return
	}
	// Reuse the uuid-keyed pairing from `cases soar-id` so both bridges resolve
	// identically (and a response carrying extra cases can't mis-attribute).
	rows := soarIDRows([]string{caseUUID}, resp.Cases)
	if id := rows[0].SOARCaseID; id != "" {
		fmt.Fprintf(os.Stdout, "SOAR case id:     %s   (inspect: soar case get %s)\n", id, id)
		return
	}
	fmt.Fprintln(os.Stderr, "note: the case has no SOAR platform linkage in legacyBatchGetCases.")
}

// expandAlertEnum normalizes a human-friendly enum flag value to the wire token:
// trim, uppercase, hyphens to underscores, and ensure the given prefix (e.g.
// "high" -> "PRIORITY_HIGH"). "informative" maps to the wire's PRIORITY_INFO so
// the priority vocabulary matches the sibling SOAR verbs. Empty stays empty
// (flag unset); final membership validation is the SDK's.
func expandAlertEnum(v, prefix string) string {
	v = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(v), "-", "_"))
	if prefix == "PRIORITY_" && v == "INFORMATIVE" {
		v = "INFO"
	}
	if v == "" || strings.HasPrefix(v, prefix) {
		return v
	}
	return prefix + v
}

func newAlertsUpdateCmd() *cobra.Command {
	var (
		status, verdict, priority, reason, reputation string
		comment, rootCause                            string
		severity, confidence, riskScore               int
		disregarded                                   bool
		dryRun, yes                                   bool
	)
	cmd := &cobra.Command{
		Use:   "update <alert-id> [<alert-id>...]",
		Short: "MUTATING (guarded): set triage feedback on one or more alerts",
		Long: "Set alert feedback — the SIEM-side disposition: status, verdict, priority,\n" +
			"reason, reputation, scores, comment, root cause. Enum flags accept short\n" +
			"lower-case values (closed, false-positive, high, not-malicious, …) or the\n" +
			"full wire tokens. Several alert ids fan out the same update per id.\n" +
			"Guarded: dry-run by default, --yes to apply.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			u := chronicle.AlertUpdate{
				Status:     expandAlertEnum(status, ""),
				Verdict:    expandAlertEnum(verdict, ""),
				Priority:   expandAlertEnum(priority, "PRIORITY_"),
				Reason:     expandAlertEnum(reason, "REASON_"),
				Reputation: expandAlertEnum(reputation, ""),
			}
			f := cmd.Flags()
			if f.Changed("severity") {
				u.Severity = &severity
			}
			if f.Changed("confidence") {
				u.ConfidenceScore = &confidence
			}
			if f.Changed("risk-score") {
				u.RiskScore = &riskScore
			}
			if f.Changed("disregarded") {
				u.Disregarded = &disregarded
			}
			if f.Changed("comment") {
				u.Comment = &comment
			}
			if f.Changed("root-cause") {
				u.RootCause = &rootCause
			}
			// Fail fast on a bad enum/range or an empty update — before the guard,
			// so a dry run already reports it.
			if err := u.Validate(); err != nil {
				return err
			}
			ids := make([]string, 0, len(args))
			for _, a := range args {
				if a = strings.TrimSpace(a); a != "" {
					ids = append(ids, a)
				}
			}
			if len(ids) == 0 {
				return fmt.Errorf("at least one non-empty alert id is required")
			}
			action := fmt.Sprintf("alerts update %s (%s)", strings.Join(ids, ","), describeAlertUpdate(u))
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				_, err = c.BulkUpdateAlerts(baseContext(), ids, u)
				return err
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&status, "status", "", "alert status: new | reviewed | closed | open")
	f.StringVar(&verdict, "verdict", "", "alert verdict: true-positive | false-positive")
	f.StringVar(&priority, "priority", "", "alert priority: info | low | medium | high | critical")
	f.StringVar(&reason, "reason", "", "close reason: not-malicious | malicious | maintenance")
	f.StringVar(&reputation, "reputation", "", "alert reputation: useful | not-useful")
	f.IntVar(&severity, "severity", 0, "severity score 0-100")
	f.IntVar(&confidence, "confidence", 0, "confidence score 0-100")
	f.IntVar(&riskScore, "risk-score", 0, "risk score 0-100")
	f.BoolVar(&disregarded, "disregarded", false, "mark the alert disregarded (=false to clear)")
	f.StringVar(&comment, "comment", "", "analyst comment (an explicit empty string clears it)")
	f.StringVar(&rootCause, "root-cause", "", "root cause (an explicit empty string clears it)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}

// describeAlertUpdate renders the set fields of an update for the guard preview.
func describeAlertUpdate(u chronicle.AlertUpdate) string {
	var parts []string
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	add("status", u.Status)
	add("verdict", u.Verdict)
	add("priority", u.Priority)
	add("reason", u.Reason)
	add("reputation", u.Reputation)
	if u.Severity != nil {
		parts = append(parts, fmt.Sprintf("severity=%d", *u.Severity))
	}
	if u.ConfidenceScore != nil {
		parts = append(parts, fmt.Sprintf("confidence=%d", *u.ConfidenceScore))
	}
	if u.RiskScore != nil {
		parts = append(parts, fmt.Sprintf("risk_score=%d", *u.RiskScore))
	}
	if u.Disregarded != nil {
		parts = append(parts, fmt.Sprintf("disregarded=%v", *u.Disregarded))
	}
	if u.Comment != nil {
		parts = append(parts, "comment")
	}
	if u.RootCause != nil {
		parts = append(parts, "root_cause")
	}
	return strings.Join(parts, " ")
}

// emitAlerts renders an alert snapshot as a compact table, or the raw alert
// objects as a JSON array under --json.
func emitAlerts(w io.Writer, snap *chronicle.AlertsSnapshot) error {
	if jsonOut {
		parts := make([]json.RawMessage, 0, len(snap.Alerts))
		for i := range snap.Alerts {
			if len(snap.Alerts[i].Raw) > 0 {
				parts = append(parts, snap.Alerts[i].Raw)
			}
		}
		b, err := json.Marshal(parts)
		if err != nil {
			return err
		}
		return writeRawJSON(w, b)
	}
	if len(snap.Alerts) == 0 {
		fmt.Fprintln(w, "no alerts.")
		return nil
	}
	fmt.Fprintf(w, "%-30s %-12s %-10s %-17s %s\n", "ID", "STATUS", "PRIORITY", "CREATED", "TYPE")
	for i := range snap.Alerts {
		a := &snap.Alerts[i]
		status, priority := "-", "-"
		if a.FeedbackSummary != nil {
			status = orDash(a.FeedbackSummary.Status)
			priority = trimPriority(chronicle.CasePriority(a.FeedbackSummary.Priority))
		}
		fmt.Fprintf(w, "%-30s %-12s %-10s %-17s %s\n",
			truncate(a.ID, 29), status, priority, shortTS(alertCreated(a)), truncate(orDash(a.Type), 28))
	}
	fmt.Fprintf(w, "\n%d alert(s)", len(snap.Alerts))
	if snap.FilteredAlertsCount > len(snap.Alerts) {
		fmt.Fprintf(w, " (of %d filtered)", snap.FilteredAlertsCount)
	}
	fmt.Fprintln(w, ".")
	return nil
}

// alertCreated returns the alert's best available creation time — the legacy
// payload populates one of these depending on the alert kind.
func alertCreated(a *chronicle.Alert) string {
	for _, t := range []string{a.CreateTime, a.AlertCreateTime, a.DetectionTime} {
		if t != "" {
			return t
		}
	}
	return ""
}

// shortTS trims an RFC3339 timestamp to minute precision (YYYY-MM-DDTHH:MM), or
// returns "-" when empty.
func shortTS(s string) string {
	if len(s) >= 16 {
		return s[:16]
	}
	return orDash(s)
}
