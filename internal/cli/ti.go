package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// newTICmd builds the Threat Intelligence command group — a read-only operational
// surface over the SIEM-plane threatCollections (Mandiant campaigns/reports/etc.).
func newTICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ti",
		Short: "Threat Intelligence (read-only): browse Mandiant threat collections",
		Long: "Read the Google/Mandiant threat-intelligence the tenant is matched against\n" +
			"(campaigns, reports, actors, malware, vulnerabilities). Read-only.",
	}
	cmd.AddCommand(newTICollectionsCmd(), newTICollectionCmd(), newTIRelatedCmd())
	return cmd
}

func newTICollectionsCmd() *cobra.Command {
	var (
		types []string
		order string
		limit int
	)
	cmd := &cobra.Command{
		Use:   "collections",
		Short: "List threat collections (campaigns/reports/…)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			tcs, err := c.ListThreatCollections(baseContext(), chronicle.ThreatCollectionQuery{
				Types:    types,
				OrderBy:  order,
				PageSize: limit,
				MaxPages: 1,
			})
			if err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(tcs)
			}
			for i, t := range tcs {
				fmt.Fprintf(os.Stdout, "%4d  %-12s %-22s %s\n", i+1, t.Type, t.ID, truncate(t.DisplayName, 80))
			}
			fmt.Fprintf(os.Stdout, "\n%d threat collection(s)\n", len(tcs))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&types, "types", []string{chronicle.CollectionCampaign, chronicle.CollectionReport},
		"collection types to include (campaign,report,actor,malware,vulnerability); empty for all")
	f.StringVar(&order, "order", "last_modification_date-", "server order key (trailing - sorts descending)")
	f.IntVar(&limit, "limit", 25, "maximum number of collections to return")
	return cmd
}

func newTICollectionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collection <id>",
		Short: "Show one threat collection by id (e.g. report--26-10031441)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			tc, err := c.GetThreatCollection(baseContext(), args[0])
			if err != nil {
				return err
			}
			if jsonOut {
				// Emit the full server object verbatim.
				return json.NewEncoder(os.Stdout).Encode(tc.Raw)
			}
			fmt.Fprintf(os.Stdout, "%s  [%s]\n%s\n\n%s\n", tc.DisplayName, tc.Type, tc.ID, tc.Description)
			return nil
		},
	}
	return cmd
}

func newTIRelatedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "related <collection-alt-name-or-id>...",
		Short: "Show IoC match counts for threat collections",
		Long: "Show IoC match counts for one or more threat collections. Pass GTI/Mandiant\n" +
			"alt names such as CAMP.22.147, or a threat collection resource id from\n" +
			"`ti collections --json` / `iocs related --json`.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			alts, err := threatCollectionAltNames(c, args)
			if err != nil {
				return err
			}
			meta, err := c.FetchIocMatchMetadata(baseContext(), alts...)
			if err != nil {
				return err
			}
			return emitIocMatchMetadata(os.Stdout, meta)
		},
	}
	return cmd
}

func threatCollectionAltNames(c *chronicle.Client, args []string) ([]string, error) {
	var out []string
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if strings.Contains(arg, "/") || strings.Contains(arg, "--") {
			tc, err := c.GetThreatCollection(baseContext(), arg)
			if err != nil {
				return nil, err
			}
			if len(tc.AltNames) == 0 {
				return nil, fmt.Errorf("threat collection %q has no altNames for match metadata", arg)
			}
			out = append(out, tc.AltNames...)
			continue
		}
		out = append(out, arg)
	}
	return out, nil
}

func emitThreatCollections(w io.Writer, tcs []chronicle.ThreatCollection) error {
	if jsonOut {
		return json.NewEncoder(w).Encode(tcs)
	}
	if len(tcs) == 0 {
		fmt.Fprintln(w, "no related threat collections matched.")
		return nil
	}
	fmt.Fprintf(w, "%-10s %-22s %-18s %s\n", "TYPE", "ID", "ALT_NAMES", "DISPLAY_NAME")
	for i := range tcs {
		tc := &tcs[i]
		fmt.Fprintf(w, "%-10s %-22s %-18s %s\n",
			orDash(tc.Type),
			orDash(tc.ID),
			orDash(strings.Join(tc.AltNames, ",")),
			truncate(tc.DisplayName, 80),
		)
	}
	fmt.Fprintf(w, "\n%d threat collection(s) — `--json` for full records\n", len(tcs))
	return nil
}

func emitIocMatchMetadata(w io.Writer, meta []chronicle.IocMatchMetadata) error {
	if jsonOut {
		return json.NewEncoder(w).Encode(meta)
	}
	if len(meta) == 0 {
		fmt.Fprintln(w, "no IoC match metadata returned.")
		return nil
	}
	fmt.Fprintf(w, "%-24s %s\n", "COLLECTION", "IOC_MATCHES")
	for i := range meta {
		fmt.Fprintf(w, "%-24s %d\n", meta[i].ThreatCollection, meta[i].IocMatchesCount)
	}
	fmt.Fprintf(w, "\n%d collection(s)\n", len(meta))
	return nil
}

func init() { rootCmd.AddCommand(newTICmd()) }
