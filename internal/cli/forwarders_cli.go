package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// The `forwarders` command surfaces the on-prem forwarders and their collectors
// — the ingestion endpoints a platform engineer manages. Read-only.
func init() { rootCmd.AddCommand(newForwardersCmd()) }

func newForwardersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forwarders <verb>",
		Short: "List forwarders and their collectors (read-only)",
	}
	cmd.AddCommand(newForwardersListCmd(), newForwardersGetCmd(), newForwardersCollectorsCmd())
	return cmd
}

func newForwardersListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Read-only: list forwarders (id + display name)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			fws, err := c.ListForwarders(baseContext())
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(fws)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "FORWARDER ID\tDISPLAY NAME")
			for i := range fws {
				fmt.Fprintf(tw, "%s\t%s\n", fws[i].ForwarderID(), fws[i].DisplayName)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "\n%d forwarder(s).\n", len(fws))
			return nil
		},
	}
	return markJSON(cmd)
}

func newForwardersGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <forwarder-id>",
		Short: "Read-only: get one forwarder",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			f, err := c.GetForwarder(baseContext(), args[0])
			if err != nil {
				return err
			}
			return emitJSON(f)
		},
	}
	return markJSON(cmd)
}

func newForwardersCollectorsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collectors <verb>",
		Short: "List/get the collectors on a forwarder (read-only)",
	}
	list := &cobra.Command{
		Use:   "list <forwarder-id>",
		Short: "Read-only: list a forwarder's collectors",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			cols, err := c.ListCollectors(baseContext(), args[0])
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(cols)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "COLLECTOR\tDISPLAY NAME")
			for i := range cols {
				fmt.Fprintf(tw, "%s\t%s\n", lastSegment(cols[i].Name), cols[i].DisplayName)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "\n%d collector(s).\n", len(cols))
			return nil
		},
	}
	get := &cobra.Command{
		Use:   "get <forwarder-id> <collector-id>",
		Short: "Read-only: get one collector",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			col, err := c.GetCollector(baseContext(), args[0], args[1])
			if err != nil {
				return err
			}
			return emitJSON(col)
		},
	}
	cmd.AddCommand(markJSON(list), markJSON(get))
	return cmd
}
