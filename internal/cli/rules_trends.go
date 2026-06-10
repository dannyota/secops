package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// Rule-tuning reads (Wave 54): per-rule detection trends ("which rules are
// noisy / silent"), rule count & quota stats, and the detection→events pivot —
// the data behind enable/alerting decisions in `push rules-deploy` and
// `push curated`. Read-only.

// trendRow is the table/--json row for the trends verbs.
type trendRow struct {
	RuleID        string `json:"rule_id"`
	DisplayName   string `json:"display_name,omitempty"`
	Detections    int    `json:"detections"`
	LastDetection string `json:"last_detection,omitempty"`
}

// emitTrendRows renders trend rows, noisiest first.
func emitTrendRows(trends []chronicle.RuleTrend, names map[string]string) []trendRow {
	rows := make([]trendRow, 0, len(trends))
	for i := range trends {
		t := &trends[i]
		rows = append(rows, trendRow{
			RuleID:        t.RuleID,
			DisplayName:   names[t.RuleID],
			Detections:    t.TotalDetections(),
			LastDetection: t.LastDetectionTime,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Detections != rows[j].Detections {
			return rows[i].Detections > rows[j].Detections
		}
		return rows[i].RuleID < rows[j].RuleID
	})
	return rows
}

func printTrendRows(rows []trendRow) {
	if len(rows) == 0 {
		fmt.Fprintln(os.Stdout, "no trends.")
		return
	}
	fmt.Fprintf(os.Stdout, "%-42s %-11s %-22s %s\n", "RULE", "DETECTIONS", "LAST DETECTION", "NAME")
	for _, r := range rows {
		fmt.Fprintf(os.Stdout, "%-42s %-11d %-22s %s\n",
			truncate(r.RuleID, 41), r.Detections, shortTS(r.LastDetection), truncate(orDash(r.DisplayName), 40))
	}
	fmt.Fprintf(os.Stdout, "\n%d rule(s), noisiest first.\n", len(rows))
}

func newRulesTrendsCmd() *cobra.Command {
	var (
		hours  int
		rules  string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "trends",
		Short: "Read-only: per-rule detection counts + last detection — which rules are noisy or silent",
		Long: "Detection counts (bucketed by day over the window) and the last-detection\n" +
			"timestamp per rule, noisiest first — the data behind enable/alerting\n" +
			"decisions. No --rule = every rule on the instance. Curated rules have their\n" +
			"own `curated trends`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			// ONE basic-view rule-list fetch serves both --rule resolution and the
			// name column (fail-soft for names: an error just renders dashes —
			// unless a non-id ref actually needs the list to resolve).
			list, listErr := c.ListRulesBasic(ctx)
			names := map[string]string{}
			if listErr == nil {
				for i := range list {
					names[list[i].RuleID()] = list[i].DisplayName
				}
			}
			var ids []string
			if rules != "" {
				for _, ref := range splitCSV(rules) {
					if looksLikeRuleID(ref) {
						ids = append(ids, ref)
						continue
					}
					if listErr != nil {
						return listErr
					}
					id, err := matchRuleID(list, ref)
					if err != nil {
						return err
					}
					ids = append(ids, id)
				}
			}
			start, end := timeWindow(hours)
			trends, err := c.GetRulesTrends(ctx, ids, start, end, chronicle.BucketSizeDay)
			if err != nil {
				return err
			}
			rows := emitTrendRows(trends, names)
			if asJSON {
				return emitJSON(rows)
			}
			printTrendRows(rows)
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 7*24, "look-back window in hours (default 7 days)")
	f.StringVar(&rules, "rule", "", "comma-separated rule refs (id/name/slug); empty = all rules")
	f.BoolVar(&asJSON, "json", false, jsonFlagHelp)
	return cmd
}

func newRulesCountsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "counts",
		Short: "Read-only: rule count and quota statistics for the instance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			counts, err := c.GetRuleCounts(baseContext())
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(counts)
			}
			fmt.Fprintf(os.Stdout, "Active rules:    %d\n", counts.TotalActiveCount)
			fmt.Fprintf(os.Stdout, "Archived rules:  %d\n", counts.TotalArchivedCount)
			fmt.Fprintf(os.Stdout, "Live rules:      %d (max %d)\n", counts.TotalLiveRuleCount, counts.MaxLiveRuleCount)
			fmt.Fprintf(os.Stdout, "Rules quota:     %d of %d used\n", counts.QuotaUsage, counts.QuotaLimit)
			return nil
		},
	}
	return cmd
}

func newRulesEventsCmd() *cobra.Command {
	var (
		maxEvents int
		asJSON    bool
	)
	cmd := &cobra.Command{
		Use:   "events <rule> <detection-id>",
		Short: "Read-only: the UDM events behind one detection (the evidence pivot)",
		Long: "Fetch the events a detection matched — the evidence an analyst inspects\n" +
			"before a verdict, without hand-writing a `query udm`. <rule> is a rule id,\n" +
			"display name, or slug; <detection-id> comes from `rules detections`.\n" +
			"Human output summarizes samples per event variable; --json is the full payload.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			ruleID, err := resolveRuleID(ctx, c, args[0])
			if err != nil {
				return err
			}
			raw, err := c.SearchRuleDetectionEvents(ctx, ruleID, args[1], maxEvents)
			if err != nil {
				return err
			}
			if asJSON {
				return writeRawJSON(os.Stdout, raw)
			}
			return emitDetectionEventsSummary(raw)
		},
	}
	f := cmd.Flags()
	f.IntVar(&maxEvents, "max", 0, "max events over all event variables (0 = server default)")
	f.BoolVar(&asJSON, "json", false, jsonFlagHelp)
	return cmd
}

// emitDetectionEventsSummary prints sample counts per event variable — enough
// to see what evidence exists; the payloads themselves ride --json.
func emitDetectionEventsSummary(raw json.RawMessage) error {
	var resp struct {
		ResultEvents map[string]struct {
			EventSamples []json.RawMessage `json:"eventSamples"`
		} `json:"resultEvents"`
		ResultEntities map[string]struct {
			EventSamples []json.RawMessage `json:"eventSamples"`
		} `json:"resultEntities"`
		TooManyEvents bool `json:"tooManyEvents"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("decode detection events: %w", err)
	}
	if len(resp.ResultEvents) == 0 && len(resp.ResultEntities) == 0 {
		fmt.Fprintln(os.Stdout, "no events.")
		return nil
	}
	total := 0
	for variable, list := range resp.ResultEvents {
		fmt.Fprintf(os.Stdout, "%-30s %d event sample(s)\n", variable, len(list.EventSamples))
		total += len(list.EventSamples)
	}
	for variable, list := range resp.ResultEntities {
		fmt.Fprintf(os.Stdout, "%-30s %d entity sample(s)\n", variable, len(list.EventSamples))
		total += len(list.EventSamples)
	}
	fmt.Fprintf(os.Stdout, "\n%d sample(s)", total)
	if resp.TooManyEvents {
		fmt.Fprint(os.Stdout, " (truncated — raise --max)")
	}
	fmt.Fprintln(os.Stdout, ". Full payloads with --json.")
	return nil
}
