package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// The `alerts` command reads SIEM detection alerts (the Chronicle legacy alert
// snapshot view, legacyFetchAlertsView). Read-only here — the alerts SDK also
// supports feedback updates, gated for a later act layer. The reliable analyst
// view of a case's alerts is the SOAR lane (`soar case get`); this surfaces the
// Chronicle alert snapshot directly. See docs/SIEM-DESIGN.md.

func init() { rootCmd.AddCommand(newAlertsCmd()) }

func newAlertsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alerts <verb>",
		Short: "Read SIEM detection alerts (read-only)",
		Long: "Query Chronicle detection alerts over a time window (a snapshot view), or\n" +
			"fetch one by id. Read-only.",
	}
	cmd.AddCommand(newAlertsListCmd(), newAlertsGetCmd())
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
		Short: "Get one alert by id",
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
			return emitAlerts(os.Stdout, &chronicle.AlertsSnapshot{Alerts: []chronicle.Alert{*a}})
		},
	}
	cmd.Flags().BoolVar(&detections, "detections", false, "include detection details (in --json)")
	return cmd
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
