package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// Curated-rule tuning reads (Wave 54): detections produced by a Google-managed
// curated rule, per-curated-rule trends, and the event behind one curated
// detection — the data behind `push curated` enable/alerting decisions, which
// `rules detections` (user rules only) cannot serve. Read-only.

// curatedTrendsChunk caps the ids per trends request (each id is a query param;
// an --all sweep over a large curated set must not build an oversized URL).
const curatedTrendsChunk = 40

func newCuratedDetectionsCmd() *cobra.Command {
	var (
		hours, limit int
		state        string
	)
	cmd := &cobra.Command{
		Use:   "detections <curated-rule-id>",
		Short: "Read-only: list detections a CURATED rule produced in a time window",
		Long: "Detections from a Google-managed curated rule (aggregated across rule\n" +
			"versions). The `ur_…` id comes from `curated rules`. The user-rule twin is\n" +
			"`rules detections`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			start, end := timeWindow(hours)
			dets, err := c.SearchCuratedDetections(baseContext(), args[0], start, end, state, limit)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(dets)
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
			fmt.Fprintf(os.Stdout, "\n%d detection(s). Events behind one: curated events <id>.\n", len(dets))
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 24, "look-back window in hours")
	f.IntVar(&limit, "limit", 100, "max detections (page size)")
	f.StringVar(&state, "state", "", "filter by alert state (e.g. ALERTING)")
	return markJSON(cmd)
}

func newCuratedTrendsCmd() *cobra.Command {
	var (
		hours int
		rules string
		all   bool
	)
	cmd := &cobra.Command{
		Use:   "trends (--rule ur_a,ur_b | --all)",
		Short: "Read-only: per-curated-rule detection counts + last detection",
		Long: "Detection counts (bucketed by day over the window) and the last-detection\n" +
			"timestamp per CURATED rule, noisiest first — the data behind `push curated`\n" +
			"enable/alerting decisions. --all sweeps every curated rule on the instance\n" +
			"(chunked requests). The user-rule twin is `rules trends`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			var ids []string
			names := map[string]string{}
			switch {
			case all:
				curated, err := c.ListCuratedRules(ctx)
				if err != nil {
					return err
				}
				for i := range curated {
					id := curated[i].ID
					ids = append(ids, id)
					names[id] = curated[i].DisplayName
				}
			case rules != "":
				ids = splitCSV(rules)
			default:
				return fmt.Errorf("pass --rule ur_a,ur_b or --all (list ids with `curated rules`)")
			}
			start, end := timeWindow(hours)
			var trends []chronicle.RuleTrend
			// Chunks run sequentially by design: ~5 requests at today's curated-set
			// size, and a serial read sweep cannot trip API throttling.
			for chunk := range slicesChunk(ids, curatedTrendsChunk) {
				part, err := c.GetCuratedRulesTrends(ctx, chunk, start, end, chronicle.BucketSizeDay)
				if err != nil {
					return err
				}
				trends = append(trends, part...)
			}
			rows := emitTrendRows(trends, names)
			if jsonOut {
				return emitJSON(rows)
			}
			printTrendRows(rows)
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&hours, "hours", 7*24, "look-back window in hours (default 7 days)")
	f.StringVar(&rules, "rule", "", "comma-separated curated rule ids (ur_…)")
	f.BoolVar(&all, "all", false, "sweep every curated rule on the instance")
	cmd.MarkFlagsMutuallyExclusive("rule", "all")
	return markJSON(cmd)
}

// slicesChunk yields ids in chunks of at most n.
func slicesChunk(ids []string, n int) func(yield func([]string) bool) {
	return func(yield func([]string) bool) {
		for start := 0; start < len(ids); start += n {
			end := min(start+n, len(ids))
			if !yield(ids[start:end]) {
				return
			}
		}
	}
}

func newCuratedEventsCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "events <detection-id>",
		Short: "Read-only: the event(s) and rationale behind one CURATED detection",
		Long: "Fetch the prioritized event(s), entities, and rationale behind a curated\n" +
			"detection (the id comes from `curated detections`). Human output shows the\n" +
			"rationale and counts; --json is the full payload. The user-rule twin is\n" +
			"`rules events`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			raw, err := c.GetEventForDetection(baseContext(), args[0], limit)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			var resp struct {
				Rationale     []string          `json:"rationale"`
				Conclusion    string            `json:"conclusion"`
				Event         []json.RawMessage `json:"event"`
				Entities      []json.RawMessage `json:"entities"`
				DetectionTime string            `json:"detectionTime"`
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				return fmt.Errorf("decode detection event: %w", err)
			}
			fmt.Fprintf(os.Stdout, "Detection time: %s\n", orDash(resp.DetectionTime))
			fmt.Fprintf(os.Stdout, "Conclusion:     %s\n", orDash(resp.Conclusion))
			for _, r := range resp.Rationale {
				fmt.Fprintf(os.Stdout, "  - %s\n", r)
			}
			fmt.Fprintf(os.Stdout, "\n%d event(s), %d entit(ies). Full payloads with --json.\n", len(resp.Event), len(resp.Entities))
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "events per page (0 = server default)")
	return markJSON(cmd)
}
