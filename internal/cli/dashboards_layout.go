package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// Dashboard layout commands: show the grid map of all widgets, and move/resize
// any widget (chart, markdown, or button) on the 96-column dashboard grid.

// widgetLayout is one widget's position on the grid, resolved for display.
type widgetLayout struct {
	WidgetID string `json:"widgetId"`
	Title    string `json:"title"`
	TileType string `json:"tileType"`
	StartX   int    `json:"startX"`
	StartY   int    `json:"startY"`
	SpanX    int    `json:"spanX"`
	SpanY    int    `json:"spanY"`
}

// chartRefLayout is the shape of a definition.charts[] entry.
type chartRefLayout struct {
	DashboardChart string `json:"dashboardChart"`
	ChartLayout    struct {
		StartX int `json:"startX"`
		StartY int `json:"startY"`
		SpanX  int `json:"spanX"`
		SpanY  int `json:"spanY"`
	} `json:"chartLayout"`
}

// resolveWidgetLayouts reads a dashboard's definition.charts and resolves each
// widget's title+tileType from the chart bodies.
func resolveWidgetLayouts(c *chronicle.Client, dashboardID string) ([]widgetLayout, error) {
	full, err := c.GetDashboard(baseContext(), dashboardID, true)
	if err != nil {
		return nil, err
	}
	var def struct {
		Definition struct {
			Charts []chartRefLayout `json:"charts"`
		} `json:"definition"`
	}
	if err := json.Unmarshal(full.Raw, &def); err != nil {
		return nil, err
	}
	refs := make([]string, 0, len(def.Definition.Charts))
	for _, cr := range def.Definition.Charts {
		if cr.DashboardChart != "" {
			refs = append(refs, cr.DashboardChart)
		}
	}
	charts := c.ChartsByID(baseContext(), refs)
	out := make([]widgetLayout, 0, len(def.Definition.Charts))
	for _, cr := range def.Definition.Charts {
		wid := lastSegment(cr.DashboardChart)
		wl := widgetLayout{
			WidgetID: wid,
			StartX:   cr.ChartLayout.StartX,
			StartY:   cr.ChartLayout.StartY,
			SpanX:    cr.ChartLayout.SpanX,
			SpanY:    cr.ChartLayout.SpanY,
		}
		if body, ok := charts[wid]; ok {
			wl.Title = nestedString(body, "displayName")
			wl.TileType = nestedString(body, "tileType")
		}
		out = append(out, wl)
	}
	return out, nil
}

func newDashboardsLayoutGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "layout <verb>",
		Short: "Show or reposition widgets on the dashboard grid",
	}
	cmd.AddCommand(
		newDashboardsLayoutShowCmd(),
		newDashboardsLayoutMoveCmd(),
	)
	return cmd
}

func newDashboardsLayoutShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <dashboard-id>",
		Short: "Show all widgets' positions on the 96-column grid (read-only)",
		Long: "Display every widget (chart, markdown, button) on the dashboard with its\n" +
			"grid coordinates: startX, startY, spanX (width), spanY (height). The\n" +
			"dashboard grid is 96 units wide. Sorted by Y then X (top-to-bottom,\n" +
			"left-to-right). Use --json for the full machine-readable list.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			widgets, err := resolveWidgetLayouts(c, args[0])
			if err != nil {
				return err
			}
			sort.Slice(widgets, func(i, j int) bool {
				if widgets[i].StartY != widgets[j].StartY {
					return widgets[i].StartY < widgets[j].StartY
				}
				return widgets[i].StartX < widgets[j].StartX
			})
			if jsonOut {
				return emitJSON(widgets)
			}
			if len(widgets) == 0 {
				fmt.Printf("Dashboard %s has no widgets.\n", args[0])
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "WIDGET-ID\tTYPE\tX\tY\tW\tH\tTITLE")
			for _, w := range widgets {
				fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t%s\n",
					w.WidgetID, shortTileType(w.TileType),
					w.StartX, w.StartY, w.SpanX, w.SpanY,
					truncate(w.Title, 40))
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Printf("\n%d widget(s) on a 96-column grid.\n", len(widgets))
			return nil
		},
	}
	return markJSON(cmd)
}

// shortTileType returns a compact label for display.
func shortTileType(t string) string {
	switch t {
	case chronicle.TileTypeVisualization:
		return "CHART"
	case chronicle.TileTypeButton:
		return "BUTTON"
	case chronicle.TileTypeMarkdown:
		return "MARKDOWN"
	default:
		return t
	}
}

func newDashboardsLayoutMoveCmd() *cobra.Command {
	var widgetID string
	var x, y, spanX, spanY int
	var setX, setY, setSpanX, setSpanY bool
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "move <dashboard-id> --widget-id <id>",
		Short: "Move or resize a widget on the dashboard grid (guarded)",
		Long: "Reposition or resize a widget (chart, markdown, or button) on the 96-column\n" +
			"grid. Only the flags you pass are changed; the others keep their current\n" +
			"values. At least one of --x, --y, --span-x, --span-y is required.\n" +
			"Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			setX = cmd.Flags().Changed("x")
			setY = cmd.Flags().Changed("y")
			setSpanX = cmd.Flags().Changed("span-x")
			setSpanY = cmd.Flags().Changed("span-y")
			if !setX && !setY && !setSpanX && !setSpanY {
				return fmt.Errorf("nothing to move: pass --x, --y, --span-x, and/or --span-y")
			}

			changes := []string{}
			if setX {
				changes = append(changes, fmt.Sprintf("x=%d", x))
			}
			if setY {
				changes = append(changes, fmt.Sprintf("y=%d", y))
			}
			if setSpanX {
				changes = append(changes, fmt.Sprintf("w=%d", spanX))
			}
			if setSpanY {
				changes = append(changes, fmt.Sprintf("h=%d", spanY))
			}

			target := fmt.Sprintf("move widget %s in dashboard %s (%s)", widgetID, id, joinComma(changes))
			return guardedSIEMMutation(target, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				ctx := baseContext()

				full, err := c.GetDashboard(ctx, id, true)
				if err != nil {
					return err
				}
				var def struct {
					Definition struct {
						Charts []json.RawMessage `json:"charts"`
					} `json:"definition"`
				}
				if err := json.Unmarshal(full.Raw, &def); err != nil {
					return err
				}

				want := lastSegment(widgetID)
				found := false
				for i, raw := range def.Definition.Charts {
					ref := nestedString(raw, "dashboardChart")
					if ref == "" || lastSegment(ref) != want {
						continue
					}
					var m map[string]json.RawMessage
					if err := json.Unmarshal(raw, &m); err != nil {
						return err
					}
					var cur struct {
						StartX int `json:"startX"`
						StartY int `json:"startY"`
						SpanX  int `json:"spanX"`
						SpanY  int `json:"spanY"`
					}
					if layRaw, ok := m["chartLayout"]; ok {
						_ = json.Unmarshal(layRaw, &cur)
					}
					if setX {
						cur.StartX = x
					}
					if setY {
						cur.StartY = y
					}
					if setSpanX {
						cur.SpanX = spanX
					}
					if setSpanY {
						cur.SpanY = spanY
					}
					nb, err := json.Marshal(cur)
					if err != nil {
						return err
					}
					m["chartLayout"] = nb
					rewritten, err := json.Marshal(m)
					if err != nil {
						return err
					}
					def.Definition.Charts[i] = rewritten
					found = true
					break
				}
				if !found {
					return fmt.Errorf("widget %s not found on dashboard %s", widgetID, id)
				}

				if _, err := c.UpdateDashboard(ctx, id, chronicle.DashboardUpdate{Charts: def.Definition.Charts}); err != nil {
					return err
				}
				if !jsonOut {
					fmt.Printf("Moved widget %s in dashboard %s: %s. Re-pull to mirror it locally.\n",
						widgetID, id, joinComma(changes))
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&widgetID, "widget-id", "", "id of the widget to move (required)")
	cmd.Flags().IntVar(&x, "x", 0, "new startX (0–96)")
	cmd.Flags().IntVar(&y, "y", 0, "new startY")
	cmd.Flags().IntVar(&spanX, "span-x", 0, "new width (1–96)")
	cmd.Flags().IntVar(&spanY, "span-y", 0, "new height")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("widget-id")
	return markJSON(cmd)
}

// joinComma joins strings with ", ".
func joinComma(ss []string) string {
	return strings.Join(ss, ", ")
}

// currentChartLayout reads the chartLayout of a chart from the dashboard's
// definition.charts array. Used by the remove+add edit pattern for non-VISUALIZATION
// tiles (the :editChart API doesn't support visualization edits on markdown/button).
func currentChartLayout(ctx context.Context, c *chronicle.Client, dashboardID, chartID string) (json.RawMessage, error) {
	full, err := c.GetDashboard(ctx, dashboardID, true)
	if err != nil {
		return nil, err
	}
	type chartRef struct {
		DashboardChart string          `json:"dashboardChart"`
		ChartLayout    json.RawMessage `json:"chartLayout"`
	}
	var def struct {
		Definition struct {
			Charts []chartRef `json:"charts"`
		} `json:"definition"`
	}
	if err := json.Unmarshal(full.Raw, &def); err != nil {
		return nil, err
	}
	want := lastSegment(chartID)
	for _, ch := range def.Definition.Charts {
		if lastSegment(ch.DashboardChart) == want {
			return ch.ChartLayout, nil
		}
	}
	return nil, fmt.Errorf("chart %s not found on dashboard %s", chartID, dashboardID)
}
