package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

func newTICoverageCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "coverage [<collection-id>...]",
		Short: "Show rule coverage for threat collections",
		Long: "List detection rule coverage for the given threat-collection ids\n" +
			"(e.g. campaign--<uuid>, report--26-NNNNN). If no ids are given,\n" +
			"lists all coverage details (unfiltered). Each row maps a threat\n" +
			"collection to a rule.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			var details []chronicle.CoverageDetail

			if len(args) > 0 {
				details, err = c.ListCoverageDetailsFiltered(ctx, args, limit)
			} else {
				// Unfiltered — use the existing raw path and decode.
				raw, rerr := c.ListCoverageDetails(ctx, limit)
				if rerr != nil {
					return rerr
				}
				for _, r := range raw {
					var d chronicle.CoverageDetail
					if uerr := json.Unmarshal(r, &d); uerr != nil {
						return uerr
					}
					details = append(details, d)
				}
			}
			if err != nil {
				return err
			}

			if jsonOut {
				return emitJSON(details)
			}
			if len(details) == 0 {
				fmt.Fprintln(os.Stdout, "no coverage details returned.")
				return nil
			}
			fmt.Fprintf(os.Stdout, "%-50s %s\n", "COLLECTION_ID", "RULE_ID")
			for i := range details {
				d := &details[i]
				fmt.Fprintf(os.Stdout, "%-50s %s\n",
					lastResourceSegment(d.ThreatCollection),
					lastResourceSegment(d.Rule),
				)
			}
			fmt.Fprintf(os.Stdout, "\n%d coverage detail(s)\n", len(details))
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 1000, "page size per request")
	return markJSON(cmd)
}

func newTIFiltersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "filters",
		Short: "Show threat-collection filter set (JSON-only)",
		Long: "Fetch the threat-collection filter-set metadata — the facets the\n" +
			"Emerging Threats console uses. JSON-only (the response shape is\n" +
			"not fully documented).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			raw, err := c.GetThreatCollectionFilterSet(baseContext())
			if err != nil {
				return err
			}
			if len(raw) == 0 {
				fmt.Fprintln(os.Stdout, "no filter set data returned.")
				return nil
			}
			return emitJSON(raw)
		},
	}
	return markJSON(cmd)
}

// lastResourceSegment returns the final "/"-separated segment of s, or s
// unchanged if it has none.
func lastResourceSegment(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return s[i+1:]
		}
	}
	return s
}
