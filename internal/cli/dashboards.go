package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// The `dashboards` command holds native-dashboard operations that fall outside
// the pull/push reconcile loop. Dashboard config-as-code is
// `pull dashboards` / `push dashboards`.
func init() { rootCmd.AddCommand(newDashboardsCmd()) }

func newDashboardsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboards <verb>",
		Short: "Extra: imperative dashboard ops (charts, duplicate) — config-as-code is `pull/push dashboards`",
		Long: "Operations on native dashboards outside the reconcile loop:\n" +
			"  add-chart / edit-chart / remove-chart — author a chart's YARA-L query\n" +
			"    (the dashboard body is reference-only, so `push dashboards` can't);\n" +
			"  charts — list a dashboard's charts with their resolved queries (read-only);\n" +
			"  run-chart / verify — execute a chart (or every chart) and read the values\n" +
			"    it renders / flag empty or errored charts (read-only);\n" +
			"  duplicate — copy a dashboard to an independent one / change its immutable access;\n" +
			"  export / import — round-trip a dashboard (+ charts + queries) as portable JSON;\n" +
			"  delete — remove a whole dashboard (e.g. a stale duplicate).\n" +
			"Config-as-code for the dashboard itself is `pull dashboards` / `push dashboards`.",
	}
	cmd.AddCommand(
		newDashboardsListCmd(),
		newDashboardsAddChartCmd(),
		newDashboardsAddChartsCmd(),
		newDashboardsEditChartCmd(),
		newDashboardsRemoveChartCmd(),
		newDashboardsDeleteCmd(),
		newDashboardsChartsCmd(),
		newDashboardsRunChartCmd(),
		newDashboardsVerifyCmd(),
		newDashboardsDuplicateCmd(),
		newDashboardsExportCmd(),
		newDashboardsImportCmd(),
	)
	return cmd
}

func newDashboardsDuplicateCmd() *cobra.Command {
	var name, access, desc string
	var dryRun, yes, deepCopy bool
	cmd := &cobra.Command{
		Use:   "duplicate <dashboard-id> --name <name> --access <type>",
		Short: "Duplicate a dashboard to a new independent dashboard (guarded)",
		Long: "Copy a dashboard under a new display name and access type. By default this\n" +
			"uses the server `:duplicate` verb — the same path the web console's Duplicate\n" +
			"action takes — which creates an independent copy: the new dashboard gets its\n" +
			"own freshly-minted charts and queries (not references to the source's), so it\n" +
			"renders and deletes like any other dashboard. This is also the supported way to\n" +
			"set the otherwise-immutable `access` on the copy.\n\n" +
			"--deep-copy rebuilds the copy client-side instead: fetch every chart and\n" +
			"recreate it FRESH, without the server verb. A fallback for when the server-side\n" +
			"copy is unavailable. Guarded: dry-run by default, --yes to apply. Re-pull\n" +
			"afterwards so local mirrors live.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			how := "duplicate"
			if deepCopy {
				how = "deep-copy"
			}
			target := fmt.Sprintf("%s dashboard %s -> %q (access=%s)", how, id, name, access)
			dr, ay := soarGuard(target, dryRun, yes) // generic dry-run/--yes guard
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would %s dashboard %s as %q (access=%s): a new independent dashboard with its own charts. Re-run with --yes.\n", how, id, name, access)
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to copy without confirmation (pass --yes). Aborted.")
				return nil
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			// Both paths inherit the source's description when --description is
			// omitted. DeepCopyDashboard does this internally; the server
			// :duplicate verb does not, so fetch it here to keep the two paths
			// (and the --description help) consistent.
			if desc == "" {
				if src, gerr := c.GetDashboard(baseContext(), id, true); gerr == nil {
					var meta struct {
						Description string `json:"description"`
					}
					if json.Unmarshal(src.Raw, &meta) == nil {
						desc = meta.Description
					}
				}
			}
			// Same signature for both paths: server-side :duplicate by default,
			// client-side rebuild under --deep-copy.
			copyFn := c.DuplicateDashboard
			if deepCopy {
				copyFn = c.DeepCopyDashboard
			}
			d, err := copyFn(baseContext(), id, name, access, desc)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(d)
			}
			fmt.Printf("Copied dashboard %s as %q (access=%s) with its own charts (%s). Re-pull to mirror it locally.\n", id, name, access, how)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "display name for the copy (required)")
	cmd.Flags().StringVar(&access, "access", "", "access type for the copy: DASHBOARD_PRIVATE | DASHBOARD_PUBLIC (required)")
	cmd.Flags().StringVar(&desc, "description", "", "optional description for the copy (inherits the source's when empty)")
	cmd.Flags().BoolVar(&deepCopy, "deep-copy", false, "rebuild the copy client-side instead of using the server :duplicate verb (fallback)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("access")
	return markJSON(cmd)
}
