package cli

import (
	"encoding/json"
	"fmt"
	"os"

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
	cmd.AddCommand(newTICollectionsCmd(), newTICollectionCmd())
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

func init() { rootCmd.AddCommand(newTICmd()) }
