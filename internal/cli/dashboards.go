package cli

import (
	"context"
	"encoding/json"
	"fmt"
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
		Short: "Dashboard ops: create, get, edit, charts, markdown, button, layout, filters",
		Long: "Operations on native dashboards outside the reconcile loop:\n" +
			"  list — list all dashboards (id, type, title);\n" +
			"  create — create an empty dashboard (guarded);\n" +
			"  get <id> — dashboard summary (metadata, filters, chart count);\n" +
			"  edit <id> — edit name, description, or access (guarded);\n" +
			"  inspect — raw chart debugging (visualization, query, layout, datasource);\n" +
			"  lint — static analysis (none-legend, long-labels, time-desync, overlap);\n" +
			"  fix — auto-fix lint findings (--strip-domain, --no-legend, --sync-time);\n" +
			"  charts — chart subcommands (list, get, add, batch, edit, remove, run);\n" +
			"  markdown — markdown widget subcommands (add, edit, remove);\n" +
			"  button — button widget subcommands (add, edit, remove);\n" +
			"  layout — widget layout subcommands (show, move);\n" +
			"  filters — dashboard filter subcommands (show, set);\n" +
			"  verify — execute every chart and flag empty/errored ones;\n" +
			"  duplicate — copy a dashboard (server-side or --deep-copy);\n" +
			"  export / import — round-trip as portable JSON;\n" +
			"  delete — remove a dashboard.\n" +
			"Config-as-code for the dashboard itself is `pull dashboards` / `push dashboards`.",
	}
	cmd.AddCommand(
		newDashboardsListCmd(),
		newDashboardsCreateCmd(),
		newDashboardsGetCmd(),
		newDashboardsEditCmd(),
		newDashboardsChartsGroupCmd(),
		newDashboardsMarkdownGroupCmd(),
		newDashboardsButtonGroupCmd(),
		newDashboardsLayoutGroupCmd(),
		newDashboardsFiltersGroupCmd(),
		newDashboardsDeleteCmd(),
		newDashboardsVerifyCmd(),
		newDashboardsDuplicateCmd(),
		newDashboardsExportCmd(),
		newDashboardsImportCmd(),
	)
	addQualityCommands(cmd)
	return cmd
}

// ── get ──────────────────────────────────────────────────────────────

// dashboardSummary is the structured output for `dashboards get`.
type dashboardSummary struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type"`
	Access      string `json:"access"`
	Etag        string `json:"etag"`
	ChartCount  int    `json:"chartCount"`
	Filter      string `json:"filter,omitempty"`
	CreateTime  string `json:"createTime,omitempty"`
	UpdateTime  string `json:"updateTime,omitempty"`
}

func newDashboardsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <dashboard-id>",
		Short: "Show a dashboard's metadata, filters, and chart count (read-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			d, err := c.GetDashboard(baseContext(), args[0], true)
			if err != nil {
				return err
			}

			var meta struct {
				Description string `json:"description"`
				Access      string `json:"access"`
				Etag        string `json:"etag"`
				CreateTime  string `json:"createTime"`
				UpdateTime  string `json:"updateTime"`
				Definition  struct {
					Charts []json.RawMessage `json:"charts"`
				} `json:"definition"`
			}
			_ = json.Unmarshal(d.Raw, &meta)

			s := dashboardSummary{
				ID:          lastSegment(d.Name),
				DisplayName: d.DisplayName,
				Description: meta.Description,
				Type:        d.Type,
				Access:      meta.Access,
				Etag:        meta.Etag,
				ChartCount:  len(meta.Definition.Charts),
				Filter:      dashboardGlobalTimeFilter(d.Raw),
				CreateTime:  meta.CreateTime,
				UpdateTime:  meta.UpdateTime,
			}
			if jsonOut {
				return emitJSON(s)
			}
			fmt.Printf("%-14s %s\n", "ID:", s.ID)
			fmt.Printf("%-14s %s\n", "Name:", s.DisplayName)
			if s.Description != "" {
				fmt.Printf("%-14s %s\n", "Description:", s.Description)
			}
			fmt.Printf("%-14s %s\n", "Type:", s.Type)
			fmt.Printf("%-14s %s\n", "Access:", s.Access)
			fmt.Printf("%-14s %d\n", "Charts:", s.ChartCount)
			if s.Filter != "" {
				fmt.Printf("%-14s %s\n", "Time filter:", s.Filter)
			}
			if s.CreateTime != "" {
				fmt.Printf("%-14s %s\n", "Created:", s.CreateTime)
			}
			if s.UpdateTime != "" {
				fmt.Printf("%-14s %s\n", "Updated:", s.UpdateTime)
			}
			fmt.Printf("%-14s %s\n", "Etag:", s.Etag)
			return nil
		},
	}
	return markJSON(cmd)
}

// ── edit ─────────────────────────────────────────────────────────────

func newDashboardsEditCmd() *cobra.Command {
	var name, desc, access string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "edit <dashboard-id>",
		Short: "Edit a dashboard's name, description, or access type (guarded)",
		Long: "Patch a dashboard's metadata in place. At least one of --name, --description,\n" +
			"or --access is required. Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if name == "" && desc == "" && access == "" {
				return fmt.Errorf("nothing to edit: pass --name, --description, and/or --access")
			}
			if access != "" {
				switch a := strings.ToUpper(access); a {
				case chronicle.DashboardPublic, chronicle.DashboardPrivate:
					access = a
				case "PUBLIC":
					access = chronicle.DashboardPublic
				case "PRIVATE":
					access = chronicle.DashboardPrivate
				default:
					return fmt.Errorf("invalid --access %q (want public | private)", access)
				}
			}

			changes := []string{}
			if name != "" {
				changes = append(changes, "name="+name)
			}
			if desc != "" {
				changes = append(changes, "description="+desc)
			}
			if access != "" {
				changes = append(changes, "access="+access)
			}
			target := fmt.Sprintf("edit dashboard %s (%s)", id, strings.Join(changes, ", "))

			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would edit dashboard %s: %s. Re-run with --yes.\n",
					id, strings.Join(changes, ", "))
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to edit without confirmation (pass --yes). Aborted.")
				return nil
			}

			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			upd := chronicle.DashboardUpdate{}
			if name != "" {
				upd.DisplayName = &name
			}
			if desc != "" {
				upd.Description = &desc
			}
			if access != "" {
				upd.Access = &access
			}
			d, err := c.UpdateDashboard(baseContext(), id, upd)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(d)
			}
			fmt.Printf("Edited dashboard %s. Re-pull to mirror changes locally.\n", id)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "new display name")
	cmd.Flags().StringVar(&desc, "description", "", "new description")
	cmd.Flags().StringVar(&access, "access", "", "new access type: public | private")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

// ── create ──────────────────────────────────────────────────────────

func newDashboardsCreateCmd() *cobra.Command {
	var name, desc, access string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "create --name <name> --access <type>",
		Short: "Create an empty dashboard (guarded)",
		Long: "Create a new CUSTOM native dashboard. The dashboard starts empty — add\n" +
			"charts, markdown, or buttons afterwards. Guarded: dry-run by default,\n" +
			"--yes to apply. Re-pull afterwards so local mirrors live.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch a := strings.ToUpper(access); a {
			case chronicle.DashboardPublic, chronicle.DashboardPrivate:
				access = a
			case "PUBLIC":
				access = chronicle.DashboardPublic
			case "PRIVATE":
				access = chronicle.DashboardPrivate
			default:
				return fmt.Errorf("invalid --access %q (want public | private)", access)
			}

			target := fmt.Sprintf("create dashboard %q (access=%s)", name, access)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would create dashboard %q (access=%s). Re-run with --yes.\n", name, access)
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to create without confirmation (pass --yes). Aborted.")
				return nil
			}

			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			d, err := c.CreateDashboard(baseContext(), name, desc, access, nil, nil)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(d)
			}
			fmt.Printf("Created dashboard %q (id %s, access=%s). Re-pull to mirror it locally.\n",
				name, lastSegment(d.Name), access)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "display name (required)")
	cmd.Flags().StringVar(&access, "access", "", "access type: public | private (required)")
	cmd.Flags().StringVar(&desc, "description", "", "optional description")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("access")
	return markJSON(cmd)
}

// ── charts subgroup ──────────────────────────────────────────────────

func newDashboardsChartsGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "charts <verb>",
		Short: "Chart operations: list, get, add, edit, remove, run",
	}
	cmd.AddCommand(
		newDashboardsChartsCmd(),
		newDashboardsChartGetCmd(),
		newDashboardsAddChartCmd(),
		newDashboardsAddChartsCmd(),
		newDashboardsEditChartCmd(),
		newDashboardsRemoveChartCmd(),
		newDashboardsRunChartCmd(),
	)
	return cmd
}

// extractDashboardID extracts the parent dashboard ID from a chart's resource
// name (projects/.../nativeDashboards/<dashId>/dashboardCharts/<chartId>).
func extractDashboardID(chartName string) string {
	const marker = "nativeDashboards/"
	_, after, found := strings.Cut(chartName, marker)
	if !found {
		return ""
	}
	if id, _, ok := strings.Cut(after, "/"); ok && id != "" {
		return id
	}
	return ""
}

// chartDefinitionEntry holds the definition-level fields for a chart (from the
// dashboard's definition.charts[] array, NOT from the chart API object).
type chartDefinitionEntry struct {
	FiltersIds  []string        `json:"filtersIds,omitempty"`
	ChartLayout json.RawMessage `json:"chartLayout,omitempty"`
}

// lookupChartDefinition reads the dashboard definition and returns the
// definition-level fields (filtersIds, chartLayout) for a specific chart.
func lookupChartDefinition(ctx context.Context, c *chronicle.Client, dashboardID, chartID string) (*chartDefinitionEntry, error) {
	full, err := c.GetDashboard(ctx, dashboardID, true)
	if err != nil {
		return nil, err
	}
	var def struct {
		Definition struct {
			Charts []struct {
				DashboardChart string          `json:"dashboardChart"`
				FiltersIds     []string        `json:"filtersIds"`
				ChartLayout    json.RawMessage `json:"chartLayout"`
			} `json:"charts"`
		} `json:"definition"`
	}
	if err := json.Unmarshal(full.Raw, &def); err != nil {
		return nil, err
	}
	want := lastSegment(chartID)
	for _, ch := range def.Definition.Charts {
		if lastSegment(ch.DashboardChart) == want {
			return &chartDefinitionEntry{
				FiltersIds:  ch.FiltersIds,
				ChartLayout: ch.ChartLayout,
			}, nil
		}
	}
	return nil, nil
}

// newDashboardsChartGetCmd shows a single chart's full detail.
func newDashboardsChartGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <chart-id>",
		Short: "Show a single chart's full detail: visualization, query, layout, filters (read-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			raw, err := c.GetChart(ctx, args[0])
			if err != nil {
				return err
			}

			chartName := nestedString(raw, "name")
			cid := lastSegment(chartName)

			// Look up definition-level fields (filtersIds, chartLayout) from
			// the parent dashboard.
			var defEntry *chartDefinitionEntry
			if dashID := extractDashboardID(chartName); dashID != "" {
				defEntry, _ = lookupChartDefinition(ctx, c, dashID, cid)
			}

			if jsonOut {
				var merged map[string]json.RawMessage
				if err := json.Unmarshal(raw, &merged); err != nil {
					return emitJSON(raw)
				}
				if defEntry != nil {
					if b, err := json.Marshal(defEntry.FiltersIds); err == nil {
						merged["filtersIds"] = b
					}
					if len(defEntry.ChartLayout) > 0 {
						merged["chartLayout"] = defEntry.ChartLayout
					}
				}
				return emitJSON(merged)
			}
			title, tileType, dataSources, viz := parseChartFields(raw)
			fmt.Printf("%-14s %s\n", "ID:", cid)
			fmt.Printf("%-14s %s\n", "Title:", title)
			fmt.Printf("%-14s %s\n", "Tile type:", tileType)
			if len(dataSources) > 0 {
				fmt.Printf("%-14s %s\n", "Datasources:", strings.Join(dataSources, ", "))
			}
			if defEntry != nil {
				if len(defEntry.FiltersIds) > 0 {
					fmt.Printf("%-14s %s\n", "Filters:", strings.Join(defEntry.FiltersIds, ", "))
				}
				if len(defEntry.ChartLayout) > 0 {
					fmt.Printf("%-14s %s\n", "Layout:", string(defEntry.ChartLayout))
				}
			}
			if len(viz) > 0 && string(viz) != "{}" {
				fmt.Printf("Visualization:\n%s\n", indentJSONPrefixed(viz, "  "))
			}
			qRef := nestedString(raw, "chartDatasource", "dashboardQuery")
			if qRef != "" {
				fmt.Printf("%-14s %s\n", "Query ID:", lastSegment(qRef))
				if qraw, qerr := c.GetQuery(ctx, qRef); qerr == nil {
					q := nestedString(qraw, "query")
					if q != "" {
						fmt.Printf("Query:\n%s\n", indentLines(q, "  "))
					}
					if input := extractRaw(qraw, "input"); len(input) > 0 {
						fmt.Printf("%-14s %s\n", "Input:", string(input))
					}
				}
			}
			return nil
		},
	}
	return markJSON(cmd)
}

// ── duplicate ────────────────────────────────────────────────────────

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
			dr, ay := soarGuard(target, dryRun, yes)
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
