package cli

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/internal/mirror"
)

// newSurfacesCmd is registered under `status` (status.go) → `status surfaces`.
func newSurfacesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "surfaces",
		Short: "List every API surface family and how it's operated (read-only, offline)",
		Long: "Print the surface-family registry: each API family's plane (host + auth), API\n" +
			"version, lane (reconcile / imperative / raw / operational), build status, and —\n" +
			"for reconcile surfaces — whether `--prune` can delete it. The map of what is\n" +
			"reconcilable vs read-only. Reads nothing live; --json for scripting.",
		Args: cobra.NoArgs,
		RunE: runSurfaces,
	}
	return markJSON(cmd)
}

// surfaceRow is one registry family's row. PruneEligible/NoDelete are pointers so
// they are present only for reconcile surfaces (omitted for non-engine families).
type surfaceRow struct {
	Name          string `json:"name"`
	Area          string `json:"area"`
	Plane         string `json:"plane"`
	Host          string `json:"host"`
	Auth          string `json:"auth"`
	Generation    string `json:"generation"`
	APIVersion    string `json:"api_version,omitempty"`
	Lane          string `json:"lane"`
	Status        string `json:"status"`
	SDK           string `json:"sdk"`
	PruneEligible *bool  `json:"prune_eligible,omitempty"`
	NoDelete      *bool  `json:"no_delete,omitempty"`
}

func surfaceRows() []surfaceRow {
	rows := make([]surfaceRow, 0, len(mirror.SurfaceFamilies))
	for _, f := range mirror.SurfaceFamilies {
		r := surfaceRow{
			Name: f.Name, Area: string(f.Area), Plane: string(f.Plane),
			Host: string(f.Host), Auth: string(f.Auth), Generation: string(f.Generation),
			APIVersion: f.APIVersion, Lane: string(f.Lane), Status: string(f.Status),
			SDK: f.SDKLocation,
		}
		// Reconcile-engine surfaces expose delete/prune capabilities. Read them with
		// a nil client — Caps are set statically at build time; the client is only
		// dereferenced by the List/Create/Update closures at run time.
		if s, ok := mirror.BuildSIEMSurface(f.Name, nil); ok {
			pe, nd := s.Caps.PruneEligible, s.Caps.NoDelete
			r.PruneEligible, r.NoDelete = &pe, &nd
		} else if s, ok := mirror.BuildSOARSurface(f.Name, nil); ok {
			pe, nd := s.Caps.PruneEligible, s.Caps.NoDelete
			r.PruneEligible, r.NoDelete = &pe, &nd
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Area != rows[j].Area {
			return rows[i].Area < rows[j].Area
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}

func runSurfaces(cmd *cobra.Command, args []string) error {
	rows := surfaceRows()
	if jsonOut {
		return emitJSON(rows)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "SURFACE\tAREA\tPLANE\tAUTH\tVER\tLANE\tSTATUS\tPRUNE")
	for _, r := range rows {
		var prune string
		switch {
		case r.PruneEligible == nil:
			prune = "-" // not a reconcile surface
		case *r.PruneEligible:
			prune = "yes"
		case r.NoDelete != nil && *r.NoDelete:
			prune = "no (NoDelete)"
		default:
			prune = "no"
		}
		ver := r.APIVersion
		if ver == "" {
			ver = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Name, r.Area, r.Plane, r.Auth, ver, r.Lane, r.Status, prune)
	}
	return tw.Flush()
}
