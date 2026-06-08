package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// The `entity` command holds operational entity reads. summarize aggregates an
// entity's alerts, related entities, and prevalence over a window — a primary
// hunting move. Read-only.
func init() { rootCmd.AddCommand(newEntityCmd()) }

func newEntityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entity <verb>",
		Short: "Entity operational reads (summarize)",
		Long: "Read-only entity intelligence. `summarize` aggregates an entity's alerts,\n" +
			"related entities, and prevalence over a time window.",
	}
	cmd.AddCommand(newEntitySummarizeCmd())
	return cmd
}

func newEntitySummarizeCmd() *cobra.Command {
	var hours int
	cmd := &cobra.Command{
		Use:   "summarize <entity-type> <value>",
		Short: "Read-only: summarize an entity (alerts, related, prevalence) over a window",
		Long: "Summarize an entity over a time window. <entity-type> e.g. ASSET / USER /\n" +
			"DOMAIN_NAME / IP_ADDRESS / FILE; <value> the identifier (hostname, user,\n" +
			"domain, IP, or hash). --json for the full summary.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			start, end := timeWindow(hours)
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			sum, err := c.SummarizeEntity(baseContext(), args[0], args[1], start, end)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(sum)
			}
			fmt.Printf("Entity: %s %s\n", args[0], args[1])
			if len(sum.AlertCounts) > 0 {
				fmt.Println("\nAlerts by rule:")
				tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
				for _, a := range sum.AlertCounts {
					fmt.Fprintf(tw, "  %s\t%d\n", a.Rule, a.Count)
				}
				_ = tw.Flush()
			} else {
				fmt.Println("\nNo alerts in the window.")
			}
			fmt.Printf("\nRelated entities: %d\nPrevalence points: %d\n", len(sum.RelatedEntities), len(sum.Prevalence))
			fmt.Println("\n(--json for the full summary)")
			return nil
		},
	}
	cmd.Flags().IntVar(&hours, "hours", 168, "look-back window in hours (default 7d)")
	return cmd
}
