package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// The `feeds` command exposes feed schema discovery — the reference for authoring
// a feed file before `push feeds` — and a guarded one-off feed delete. Feed
// config-as-code lives in `pull feeds` / `push feeds`.
func init() { rootCmd.AddCommand(newFeedsCmd()) }

func newFeedsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feeds <verb>",
		Short: "Feed schema discovery (read-only) and feed deletion (guarded)",
		Long: "Discover the feed source types and their log types — the field reference for\n" +
			"authoring a feed YAML before `push feeds` — and delete a feed by id. Feed\n" +
			"config-as-code is `pull feeds` / `push feeds`; deleting is a guarded one-off\n" +
			"(feeds are deliberately not prune-eligible, since a delete stops ingestion).",
	}
	cmd.AddCommand(newFeedsSchemasCmd(), newFeedsDeleteCmd())
	return cmd
}

func newFeedsDeleteCmd() *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "MUTATING (guarded): delete a feed by id (stops its ingestion)",
		Long: "Delete a single ingestion feed by id — the feed UUID, or its full resource\n" +
			"name. Deleting a feed STOPS its ingestion, so feeds are deliberately not\n" +
			"prune-eligible via `push feeds`; this is the explicit, one-off path.\n" +
			"Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("feed id is required")
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			// Resolve the feed first: the preview/confirmation names what goes away,
			// and a wrong id fails cleanly (404) before any mutation is attempted.
			f, err := c.GetFeed(ctx, id)
			if err != nil {
				return err
			}
			name := f.DisplayName
			if name == "" {
				name = id
			}
			action := fmt.Sprintf("feeds delete %q (%s, state %s)", name, id, orDash(f.State))
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				return c.DeleteFeed(ctx, id)
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
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
