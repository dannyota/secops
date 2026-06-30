package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

func newLogTypesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log-types <verb>",
		Short: "Manage the instance's log types (list, get, create)",
	}
	cmd.AddCommand(
		newLogTypesListCmd(),
		newLogTypesGetCmd(),
		newLogTypesCreateCmd(),
	)
	return cmd
}

func newLogTypesListCmd() *cobra.Command {
	var (
		search string
		limit  int
		all    bool
		sortBy string
	)
	cmd := &cobra.Command{
		Use:   "list [--search <pattern>]",
		Short: "List log types, by default only those with active feeds",
		Long: "List the instance's log types. By default shows only log types with\n" +
			"feedCount > 0 (actively ingesting). Use --all for the full catalog.\n\n" +
			"Sort with --sort: name (default), feeds, collection.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			lts, err := c.ListLogTypes(baseContext(), search, limit)
			if err != nil {
				return err
			}
			if !all {
				lts = slices.DeleteFunc(lts, func(lt chronicle.LogType) bool {
					return lt.FeedCount <= 0
				})
			}
			sortLogTypes(lts, sortBy)
			if jsonOut {
				return emitJSON(lts)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "LOG TYPE\tDISPLAY NAME\tFEEDS\tCUSTOM\tLAST COLLECTION")
			for _, lt := range lts {
				custom := ""
				if lt.IsCustom {
					custom = "yes"
				}
				collection := orDash(lt.CollectionTime)
				if len(collection) > 10 {
					collection = collection[:10]
				}
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n",
					lastSegment(lt.Name), lt.DisplayName,
					lt.FeedCount, custom, collection)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			suffix := ""
			if !all {
				suffix = " with active feeds (use --all for full catalog)"
			}
			fmt.Fprintf(os.Stdout, "\n%d log type(s)%s.\n", len(lts), suffix)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&search, "search", "", "case-insensitive substring filter on id/display name")
	f.IntVar(&limit, "limit", 5000, "max log types to scan")
	f.BoolVar(&all, "all", false, "show all log types, including those with no feeds")
	f.StringVar(&sortBy, "sort", "name", "sort by: name, feeds, collection")
	return markJSON(cmd)
}

func sortLogTypes(lts []chronicle.LogType, by string) {
	switch strings.ToLower(by) {
	case "feeds":
		slices.SortStableFunc(lts, func(a, b chronicle.LogType) int {
			if a.FeedCount != b.FeedCount {
				return b.FeedCount - a.FeedCount
			}
			return strings.Compare(a.LogTypeID(), b.LogTypeID())
		})
	case "collection":
		slices.SortStableFunc(lts, func(a, b chronicle.LogType) int {
			return strings.Compare(b.CollectionTime, a.CollectionTime)
		})
	default:
		slices.SortStableFunc(lts, func(a, b chronicle.LogType) int {
			return strings.Compare(a.LogTypeID(), b.LogTypeID())
		})
	}
}

func newLogTypesGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <log-type>",
		Short: "Show the description of one log type",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			desc, err := c.GetLogTypeDescription(baseContext(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, desc)
			return nil
		},
	}
	return cmd
}

func newLogTypesCreateCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "create <log-type-id> <display-name>",
		Short: "Create a custom log type (permanent — no API delete)",
		Long: "Create a custom log type with the given ID and display name.\n\n" +
			"The ID is uppercased with a _CUSTOM suffix appended automatically.\n" +
			"The display name gets a ' Custom' suffix.\n\n" +
			"WARNING: custom log types cannot be deleted or renamed — the API\n" +
			"has no delete or patch endpoint, and the console offers neither.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			logTypeID := strings.TrimSpace(args[0])
			displayName := strings.TrimSpace(args[1])

			fmt.Fprintf(os.Stdout, "Create custom log type:\n")
			fmt.Fprintf(os.Stdout, "  id:      %s\n", logTypeID)
			fmt.Fprintf(os.Stdout, "  name:    %s\n", displayName)

			return guardedSIEMMutation("log-types create "+logTypeID, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				lt, err := c.CreateLogType(baseContext(), logTypeID, displayName)
				if err != nil {
					return err
				}
				if jsonOut {
					return emitJSON(lt)
				}
				fmt.Fprintf(os.Stdout, "\nCreated %s (%s).\n", lastSegment(lt.Name), lt.DisplayName)
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply the change")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}
