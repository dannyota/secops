package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// query_saved_server.go is the server-side "Search Manager" saved & shared
// searches (chronicle users/me/searchQueries): list / get / run / save / share /
// delete. These replace the former local .udm pack — saved searches now live on
// the tenant and can be shared org-wide. Mutations (save/share/delete) are gated
// behind --yes.

// newQuerySavedCmd is the `query saved` command group (server-side).
func newQuerySavedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "saved",
		Short: "Manage server-side saved and shared searches (Search Manager)",
		Long: "List, run, and manage server-side saved searches (the console's Search\n" +
			"Manager). Saved searches live on the tenant and can be shared across the org\n" +
			"(--share). Running `saved` with no subcommand lists them.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runSavedList() },
	}
	cmd.AddCommand(
		newSavedListCmd(), newSavedGetCmd(), newSavedRunCmd(),
		newSavedSaveCmd(), newSavedShareCmd(false), newSavedShareCmd(true), newSavedDeleteCmd(),
	)
	return markJSON(cmd)
}

func newSavedListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List saved & shared searches",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runSavedList() },
	}
	return markJSON(cmd)
}

func runSavedList() error {
	c, err := newChronicleClient()
	if err != nil {
		return err
	}
	list, err := c.ListSavedSearches(baseContext())
	if err != nil {
		return err
	}
	if jsonOut {
		return emitJSON(list)
	}
	if len(list) == 0 {
		fmt.Println("no saved searches")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSHARED\tTYPE\tNAME")
	for _, s := range list {
		shared := "private"
		if s.Shared() {
			shared = "shared"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", lastSegment(s.Name), shared, shortQueryType(s.QueryType), s.DisplayName)
	}
	return tw.Flush()
}

func newSavedGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Show one saved search (its query, type, sharing)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			s, err := c.GetSavedSearch(baseContext(), args[0])
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(s)
			}
			fmt.Printf("Name:    %s\n", s.DisplayName)
			fmt.Printf("ID:      %s\n", lastSegment(s.Name))
			fmt.Printf("Type:    %s\n", s.QueryType)
			fmt.Printf("Shared:  %v\n", s.Shared())
			if s.Description != "" {
				fmt.Printf("Desc:    %s\n", s.Description)
			}
			fmt.Printf("\nQuery:\n%s\n", s.Query)
			return nil
		},
	}
	return markJSON(cmd)
}

// newSavedRunCmd fetches a saved UDM search and runs it (same window/output flags
// as `query udm`).
func newSavedRunCmd() *cobra.Command {
	var w queryWindowFlags
	cmd := &cobra.Command{
		Use:   "run <id>",
		Short: "Run a saved UDM search by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			s, err := c.GetSavedSearch(baseContext(), args[0])
			if err != nil {
				return err
			}
			if s.QueryType != "" && s.QueryType != chronicle.QueryTypeUDM {
				return fmt.Errorf("saved search %q is %s, not a UDM query — run it in the console or via `search stats`", lastSegment(s.Name), s.QueryType)
			}
			return runUDMQuery(s.Query, w, cmd.Flags().Changed("limit"))
		},
	}
	w.bind(cmd)
	return markJSON(cmd)
}

// newSavedSaveCmd creates a saved search (guarded: --dry-run default, --yes applies).
func newSavedSaveCmd() *cobra.Command {
	var (
		name, query, file, desc string
		share, dryRun, yes      bool
	)
	cmd := &cobra.Command{
		Use:   "save --name <name> (--query <udm> | --file <path>) [--share]",
		Short: "MUTATING (guarded): create a saved search",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("--name is required")
			}
			q := strings.TrimSpace(query)
			if q == "" && file != "" {
				loaded, err := readQueryText(file)
				if err != nil {
					return fmt.Errorf("read --file %q: %w", file, err)
				}
				q = loaded
			}
			if q == "" {
				return fmt.Errorf("a query is required (--query or --file)")
			}
			in := chronicle.SavedSearch{DisplayName: name, Query: q, Description: desc}
			vis := "private"
			if share {
				in.Metadata = &chronicle.SavedSearchMeta{SharingMode: chronicle.SharingModeSharedWithCustomer}
				vis = "shared org-wide"
			}
			action := fmt.Sprintf("create saved search %q (%s)", name, vis)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				_, err = c.CreateSavedSearch(baseContext(), in)
				return err
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "display name (required)")
	f.StringVar(&query, "query", "", "UDM query text")
	f.StringVar(&file, "file", "", "read the query from a file (or - for stdin)")
	f.StringVar(&desc, "description", "", "optional description")
	f.BoolVar(&share, "share", false, "share with the whole org (default: private)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

// newSavedShareCmd builds either `share` or `unshare` (guarded).
func newSavedShareCmd(unshare bool) *cobra.Command {
	use, short, mode := "share <id>", "MUTATING (guarded): share a saved search org-wide", chronicle.SharingModeSharedWithCustomer
	if unshare {
		use, short, mode = "unshare <id>", "MUTATING (guarded): make a shared saved search private", chronicle.SharingModePrivate
	}
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			action := fmt.Sprintf("set saved search %q sharing to %s", args[0], mode)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				_, err = c.ShareSavedSearch(baseContext(), args[0], mode)
				return err
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

func newSavedDeleteCmd() *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "MUTATING (guarded): delete a saved search",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			action := fmt.Sprintf("delete saved search %q", args[0])
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				return c.DeleteSavedSearch(baseContext(), args[0])
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

// shortQueryType strips the QUERY_TYPE_ prefix for compact display.
func shortQueryType(t chronicle.QueryType) string {
	return strings.TrimSuffix(strings.TrimPrefix(string(t), "QUERY_TYPE_"), "_QUERY")
}
