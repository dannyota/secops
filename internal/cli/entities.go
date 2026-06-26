package cli

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Findings-graph pivot commands, registered under the existing `entities` group
// (see entity.go). `graph` seeds the graph from a detection; `graph explore`
// expands a node.

// newEntitiesGraphCmd seeds a findings graph from a detection — the pivot the
// console's graph-investigation view uses. Node ids in the response are tied to
// the --hours range, so `graph explore` must reuse the same window.
func newEntitiesGraphCmd() *cobra.Command {
	var hours int
	cmd := &cobra.Command{
		Use:   "graph <detection-id>",
		Short: "Read-only: seed a findings graph from a detection (entities, edges, lateral movement)",
		Long: "Initialize the findings graph from a detection id over the last --hours: the\n" +
			"root node plus the first page of connected nodes and edges — the entity-to-\n" +
			"entity pivot behind the console's graph investigation view. Expand a node with\n" +
			"`entities graph explore` using the node ids from this response (same window).",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := checkHours(hours); err != nil {
				return err
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			start, end := timeWindow(hours)
			raw, err := c.InitializeFindingsGraph(baseContext(), args[0], start, end)
			if err != nil {
				return err
			}
			return writeRawJSON(os.Stdout, raw)
		},
	}
	cmd.Flags().IntVar(&hours, "hours", 168, "look-back window in hours (node ids are tied to this range)")
	cmd.AddCommand(newEntitiesGraphExploreCmd())
	return markJSON(cmd)
}

// newEntitiesGraphExploreCmd expands one node of an initialized graph. The exact
// node + time-range query params come from the `entities graph` response, so
// they pass through verbatim via --param.
func newEntitiesGraphExploreCmd() *cobra.Command {
	var params []string
	cmd := &cobra.Command{
		Use:   "explore --param key=value ...",
		Short: "Read-only: expand a node of an initialized findings graph",
		Long: "Expand one node (or unpack a group node) of a graph seeded by `entities graph`.\n" +
			"The node ids and the exact query-param names come from that response and must\n" +
			"use the SAME time range; pass them verbatim with repeatable --param key=value.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			q := url.Values{}
			for _, p := range params {
				k, v, ok := strings.Cut(p, "=")
				if !ok {
					return fmt.Errorf("--param must be key=value, got %q", p)
				}
				q.Add(k, v)
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			raw, err := c.ExploreFindingsGraphNode(baseContext(), q)
			if err != nil {
				return err
			}
			return writeRawJSON(os.Stdout, raw)
		},
	}
	cmd.Flags().StringArrayVar(&params, "param", nil, "query parameter key=value (repeatable; from the `entities graph` response)")
	return markJSON(cmd)
}

// newEntitiesRiskScoresCmd queries per-entity behavioral risk scores — the
// normalized signal for prioritizing hosts/users during triage or a hunt.
func newEntitiesRiskScoresCmd() *cobra.Command {
	var filter, orderBy string
	var limit int
	cmd := &cobra.Command{
		Use:   "risk-scores [--filter EXPR] [--order-by FIELD] [--limit N]",
		Short: "Read-only: per-entity behavioral risk scores (filter + sort)",
		Long: "Query entityRiskScores — a normalized behavioral risk score per entity, the\n" +
			"signal for prioritizing which hosts/users to look at first. --filter and\n" +
			"--order-by pass through to the API (e.g. --order-by 'riskScore desc'); --limit\n" +
			"caps the page. JSON output.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			rows, err := c.QueryEntityRiskScores(baseContext(), filter, orderBy, limit)
			if err != nil {
				return err
			}
			return emitJSON(rows)
		},
	}
	cmd.Flags().StringVar(&filter, "filter", "", "server-side filter expression")
	cmd.Flags().StringVar(&orderBy, "order-by", "", "order-by field, e.g. \"riskScore desc\"")
	cmd.Flags().IntVar(&limit, "limit", 100, "max rows to return")
	return markJSON(cmd)
}
