package cli

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
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
			"  duplicate — the supported way to change a dashboard's immutable access.\n" +
			"Config-as-code for the dashboard itself is `pull dashboards` / `push dashboards`.",
	}
	cmd.AddCommand(
		newDashboardsAddChartCmd(),
		newDashboardsAddChartsCmd(),
		newDashboardsEditChartCmd(),
		newDashboardsRemoveChartCmd(),
		newDashboardsChartsCmd(),
		newDashboardsRunChartCmd(),
		newDashboardsVerifyCmd(),
		newDashboardsDuplicateCmd(),
	)
	return cmd
}

func newDashboardsDuplicateCmd() *cobra.Command {
	var name, access, desc string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "duplicate <dashboard-id> --name <name> --access <type>",
		Short: "Copy a dashboard with a new name and access (guarded)",
		Long: "Duplicate a dashboard under a new display name and access type — the supported\n" +
			"way to change the otherwise-immutable `access` field. Guarded: dry-run by\n" +
			"default, --yes to apply. Re-pull afterwards so local mirrors live.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			target := fmt.Sprintf("duplicate dashboard %s -> %q (access=%s)", id, name, access)
			dr, ay := soarGuard(target, dryRun, yes) // generic dry-run/--yes guard
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would duplicate dashboard %s as %q (access=%s). Re-run with --yes.\n", id, name, access)
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to duplicate without confirmation (pass --yes). Aborted.")
				return nil
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			d, err := c.DuplicateDashboard(baseContext(), id, name, access, desc)
			if err != nil {
				return hintDuplicateDashboard(err)
			}
			if jsonOut {
				return emitJSON(d)
			}
			fmt.Printf("Duplicated dashboard %s as %q (access=%s). Re-pull to mirror it locally.\n", id, name, access)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "display name for the copy (required)")
	cmd.Flags().StringVar(&access, "access", "", "access type for the copy: DASHBOARD_PRIVATE | DASHBOARD_PUBLIC (required)")
	cmd.Flags().StringVar(&desc, "description", "", "optional description for the copy")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("access")
	return markJSON(cmd)
}

// hintDuplicateDashboard augments the server's generic 500 on the native-dashboard
// :duplicate endpoint — a known backend issue on some instances — with an
// actionable alternative, instead of surfacing only "INTERNAL".
func hintDuplicateDashboard(err error) error {
	var ae *chronicle.APIError
	if errors.As(err, &ae) && ae.Status == http.StatusInternalServerError && strings.Contains(strings.ToLower(ae.Body), "duplicat") {
		return fmt.Errorf("%w\nhint: the :duplicate endpoint returns a server-side 500 on some instances. "+
			"To change a dashboard's access, recreate it instead: `pull dashboards`, copy the <slug>.json under a new "+
			"name + access (drop the `_server` block), then `push dashboards`; add charts with `dashboards add-chart`", err)
	}
	return err
}
