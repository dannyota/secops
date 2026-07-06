package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type playbookHealthRow struct {
	Name       string  `json:"name"`
	Identifier string  `json:"identifier"`
	Enabled    bool    `json:"enabled"`
	Steps      int     `json:"steps"`
	Flows      int     `json:"flows"`
	FailRate   float64 `json:"failRate"`
}

func newSOARPlaybookHealthCmd() *cobra.Command {
	var hours int
	cmd := &cobra.Command{
		Use:   "health [--hours N]",
		Short: "Show fleet health: per-playbook run stats sorted by failure rate",
		Long: "Fetch run statistics for every enabled playbook across a time window\n" +
			"and rank by failure rate (worst first). Playbooks with no runs in the\n" +
			"window are omitted. Uses the same stats API as `playbooks stats`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			cards, err := lc.ListPlaybooks(ctx, nil)
			if err != nil {
				return err
			}

			end := time.Now().UTC()
			from := end.Add(-time.Duration(hours) * time.Hour)

			var rows []playbookHealthRow
			for _, card := range cards {
				if !card.IsEnabled {
					continue
				}
				body := map[string]any{
					"fromUnixTimeMs":             from.UnixMilli(),
					"toUnixTimeMs":               end.UnixMilli(),
					"originalWorkflowIdentifier": card.Identifier,
				}
				var raw json.RawMessage
				raw, err = lc.GetPlaybookStats(ctx, body)
				if err != nil {
					raw, err = lc.PlaybookXGetStatsMap(ctx, body)
					if err != nil {
						continue
					}
				}
				var probe struct {
					Steps map[string]json.RawMessage `json:"steps"`
					Flows map[string]json.RawMessage `json:"flows"`
				}
				if json.Unmarshal(raw, &probe) != nil {
					continue
				}
				if len(probe.Steps) == 0 && len(probe.Flows) == 0 {
					continue
				}
				failRate := computeFlowFailRate(probe.Flows)
				rows = append(rows, playbookHealthRow{
					Name:       card.Name,
					Identifier: card.Identifier,
					Enabled:    card.IsEnabled,
					Steps:      len(probe.Steps),
					Flows:      len(probe.Flows),
					FailRate:   failRate,
				})
			}

			sort.Slice(rows, func(i, j int) bool {
				return rows[i].FailRate > rows[j].FailRate
			})

			if jsonOut {
				return emitJSON(rows)
			}
			if len(rows) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No playbooks with run data in the last %dh.\n", hours)
				return nil
			}
			printPlaybookHealthRows(cmd.OutOrStdout(), rows, hours)
			return nil
		},
	}
	cmd.Flags().IntVar(&hours, "hours", 7*24, "look-back window in hours (default 7 days)")
	return markJSON(cmd)
}

func computeFlowFailRate(flows map[string]json.RawMessage) float64 {
	total := 0
	failed := 0
	for _, raw := range flows {
		var entry struct {
			Count  int `json:"count"`
			Status any `json:"status"`
		}
		if json.Unmarshal(raw, &entry) != nil {
			continue
		}
		total += entry.Count
		status := strings.ToLower(displayJSONScalar(entry.Status))
		if strings.Contains(status, "fail") || strings.Contains(status, "fault") || strings.Contains(status, "error") {
			failed += entry.Count
		}
	}
	if total == 0 {
		return 0
	}
	return float64(failed) / float64(total) * 100
}

func printPlaybookHealthRows(w io.Writer, rows []playbookHealthRow, hours int) {
	fmt.Fprintf(w, "Playbook health (last %dh, %d with runs, worst first):\n\n", hours, len(rows))
	fmt.Fprintln(w, "FAIL%\tSTEPS\tFLOWS\tNAME")
	for _, r := range rows {
		fmt.Fprintf(w, "%.1f%%\t%d\t%d\t%s\n", r.FailRate, r.Steps, r.Flows, r.Name)
	}
}
