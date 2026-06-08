package cli

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// The `feeds` command exposes feed schema discovery — the reference for authoring
// a feed file before `push feeds`. Feed config-as-code lives in
// `pull feeds` / `push feeds`.
func init() { rootCmd.AddCommand(newFeedsCmd()) }

func newFeedsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feeds <verb>",
		Short: "Feed schema discovery (read-only)",
		Long: "Discover the feed source types and their log types — the field reference for\n" +
			"authoring a feed YAML before `push feeds`. Feed config-as-code is\n" +
			"`pull feeds` / `push feeds`.",
	}
	cmd.AddCommand(newFeedsSchemasCmd())
	return cmd
}

func newFeedsSchemasCmd() *cobra.Command {
	var sourceType string
	cmd := &cobra.Command{
		Use:   "schemas [--source-type <type>]",
		Short: "Read-only: list feed source types, or one source type's log types",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			if sourceType != "" {
				schemas, err := c.ListLogTypeSchemas(ctx, sourceType)
				if err != nil {
					return err
				}
				type row struct {
					LogType     string `json:"log_type"`
					DisplayName string `json:"display_name"`
					ReadOnly    bool   `json:"read_only"`
				}
				rows := make([]row, 0, len(schemas))
				for _, s := range schemas {
					rows = append(rows, row{LogType: s.LogType, DisplayName: s.DisplayName, ReadOnly: s.ReadOnly})
				}
				sort.Slice(rows, func(i, j int) bool { return rows[i].LogType < rows[j].LogType })
				if jsonOut {
					return emitJSON(rows)
				}
				tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
				fmt.Fprintln(tw, "LOG TYPE\tDISPLAY NAME\tREAD-ONLY")
				for _, r := range rows {
					fmt.Fprintf(tw, "%s\t%s\t%v\n", r.LogType, r.DisplayName, r.ReadOnly)
				}
				return tw.Flush()
			}

			schemas, err := c.ListFeedSourceTypeSchemas(ctx)
			if err != nil {
				return err
			}
			type row struct {
				FeedSourceType string `json:"feed_source_type"`
				DisplayName    string `json:"display_name"`
				ReadOnly       bool   `json:"read_only"`
				LogTypes       int    `json:"log_types"`
			}
			rows := make([]row, 0, len(schemas))
			for _, s := range schemas {
				rows = append(rows, row{FeedSourceType: s.FeedSourceType, DisplayName: s.DisplayName, ReadOnly: s.ReadOnly, LogTypes: len(s.LogTypeSchemas)})
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].FeedSourceType < rows[j].FeedSourceType })
			if jsonOut {
				return emitJSON(rows)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "FEED SOURCE TYPE\tDISPLAY NAME\tREAD-ONLY\tLOG TYPES")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%v\t%d\n", r.FeedSourceType, r.DisplayName, r.ReadOnly, r.LogTypes)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Println("\nPass --source-type <type> to list that source's log types.")
			return nil
		},
	}
	cmd.Flags().StringVar(&sourceType, "source-type", "", "list the log-type schemas for this feed source type")
	return cmd
}
