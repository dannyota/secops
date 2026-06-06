package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
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
		newRulesDetectionsCmd(),
		newRulesErrorsCmd(),
		newRulesAlertsCmd(),
		newRulesRetrohuntCmd(),
	)
	return cmd
}

// timeWindow returns [now-hours, now] in UTC (default 24h when hours <= 0).
func timeWindow(hours int) (time.Time, time.Time) {
	if hours <= 0 {
		hours = 24
	}
	end := time.Now().UTC()
	return end.Add(-time.Duration(hours) * time.Hour), end
}

func newRulesDetectionsCmd() *cobra.Command {
	var (
		hours, limit int
		state        string
		asJSON       bool
	)
	cmd := &cobra.Command{
		Use:   "detections <rule-id>",
		Short: "Read-only: list detections a rule produced in a time window",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			start, end := timeWindow(hours)
			dets, err := c.ListDetections(baseContext(), args[0], start, end, state, limit)
			if err != nil {
				return err
			}
			if asJSON {
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
	f.BoolVar(&asJSON, "json", false, "emit raw JSON")
	return cmd
}

func newRulesErrorsCmd() *cobra.Command {
	var (
		hours  int
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "errors <rule-id>",
		Short: "Read-only: list execution errors a rule produced in a time window",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			start, end := timeWindow(hours)
			errs, err := c.ListErrors(baseContext(), args[0], start, end)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSONValue(os.Stdout, errs)
			}
			if len(errs) == 0 {
				fmt.Fprintln(os.Stdout, "no errors.")
				return nil
			}
			for i := range errs {
				e := &errs[i]
				fmt.Fprintf(os.Stdout, "%-22s %-26s %s\n", orDash(e.ErrorTime), orDash(e.Category),
					truncate(orFirst(e.Error, e.Text), 80))
			}
			fmt.Fprintf(os.Stdout, "\n%d error(s).\n", len(errs))
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 24, "look-back window in hours")
	f.BoolVar(&asJSON, "json", false, "emit raw JSON")
	return cmd
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
			res, err := c.SearchRuleAlerts(baseContext(), args[0], start, end)
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
	return cmd
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
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list <rule-id>",
		Short: "Read-only: list a rule's retrohunts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			rhs, err := c.ListRetrohunts(baseContext(), args[0])
			if err != nil {
				return err
			}
			if asJSON {
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
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit raw JSON")
	return cmd
}

func newRetrohuntGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get <rule-id> <retrohunt-id>",
		Short: "Read-only: get one retrohunt's status",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			rh, err := c.GetRetrohunt(baseContext(), args[0], args[1])
			if err != nil {
				return err
			}
			if asJSON {
				return writeRawJSON(os.Stdout, rh.Raw)
			}
			fmt.Fprintf(os.Stdout, "retrohunt %s\n  state:    %s\n  progress: %.1f%%\n",
				lastSegment(rh.Name), orDash(rh.State), rh.ProgressPercentage)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit raw JSON")
	return cmd
}

func newRetrohuntCreateCmd() *cobra.Command {
	var (
		hours       int
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "create <rule-id>",
		Short: "MUTATING (guarded): start a retrohunt over the last --hours of data",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ruleID := args[0]
			start, end := timeWindow(hours)
			action := fmt.Sprintf("retrohunt %s over the last %dh", ruleID, hoursOrDefault(hours))
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				rh, err := c.CreateRetrohunt(baseContext(), ruleID, start, end)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stdout, "started retrohunt %s (state %s)\n", lastSegment(rh.Name), orDash(rh.State))
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 168, "look-back window in hours (default 7d)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
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
