package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// Button-widget commands: add, edit, and remove button tiles on a dashboard.
// Button tiles use tileType TILE_TYPE_BUTTON with visualization.button
// (label, hyperlink, description, newTab, color, style). No query/datasource.

// buttonVisualization builds the visualization JSON for a button tile.
func buttonVisualization(label, hyperlink, description, color, style string, newTab bool) json.RawMessage {
	type btnProps struct {
		Color       string `json:"color,omitempty"`
		ButtonStyle string `json:"buttonStyle,omitempty"`
	}
	type btn struct {
		Label       string    `json:"label"`
		Hyperlink   string    `json:"hyperlink"`
		Description string    `json:"description,omitempty"`
		NewTab      bool      `json:"newTab,omitempty"`
		Properties  *btnProps `json:"properties,omitempty"`
	}
	type vis struct {
		Button btn `json:"button"`
	}
	var props *btnProps
	if color != "" || style != "" {
		props = &btnProps{Color: color, ButtonStyle: buttonStyleToken(style)}
	}
	b, _ := json.Marshal(vis{Button: btn{
		Label:       label,
		Hyperlink:   hyperlink,
		Description: description,
		NewTab:      newTab,
		Properties:  props,
	}})
	return b
}

// buttonStyleToken normalizes the --style flag to the API enum.
func buttonStyleToken(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "filled":
		return "BUTTON_STYLE_FILLED"
	case "outlined":
		return "BUTTON_STYLE_OUTLINED"
	case "transparent":
		return "BUTTON_STYLE_TRANSPARENT"
	default:
		return s
	}
}

func newDashboardsButtonGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "button <verb>",
		Short: "Manage button widgets on a dashboard (add, edit, remove)",
	}
	cmd.AddCommand(
		newDashboardsButtonAddCmd(),
		newDashboardsButtonEditCmd(),
		newDashboardsButtonRemoveCmd(),
	)
	return cmd
}

func newDashboardsButtonAddCmd() *cobra.Command {
	var title, label, url, description, color, style, layout string
	var newTab, dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "add <dashboard-id> --title <t> --label <l> --url <u>",
		Short: "Add a button widget to a dashboard (guarded)",
		Long: "Add a button tile to a native dashboard via :addChart. Button tiles\n" +
			"render a clickable hyperlink on the dashboard grid — no query or\n" +
			"datasource. Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if label == "" {
				return fmt.Errorf("--label is required (button text)")
			}
			if url == "" {
				return fmt.Errorf("--url is required (hyperlink target)")
			}
			if style != "" {
				switch strings.ToLower(style) {
				case "filled", "outlined", "transparent":
				default:
					return fmt.Errorf("invalid --style %q (want filled | outlined | transparent)", style)
				}
			}

			target := fmt.Sprintf("add button %q to dashboard %s", title, id)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would add button %q (label=%q url=%s) to dashboard %s. Re-run with --yes.\n",
					title, label, url, id)
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to add button without confirmation (pass --yes). Aborted.")
				return nil
			}

			lay, err := rawJSONOrNil("layout", layout)
			if err != nil {
				return err
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			resp, err := c.AddChart(baseContext(), id, chronicle.AddChartInput{
				DisplayName:   title,
				TileType:      chronicle.TileTypeButton,
				ChartLayout:   lay,
				Visualization: buttonVisualization(label, url, description, color, style, newTab),
			})
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			if cid := lastSegment(nestedString(resp, "dashboardChart", "name")); cid != "" {
				fmt.Printf("Added button %q (id %s) to dashboard %s. Re-pull to mirror it locally.\n", title, cid, id)
			} else {
				fmt.Printf("Added button %q to dashboard %s. Re-pull to mirror it locally.\n", title, id)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "widget display name (required)")
	cmd.Flags().StringVar(&label, "label", "", "button text (required)")
	cmd.Flags().StringVar(&url, "url", "", "hyperlink target (required)")
	cmd.Flags().StringVar(&description, "description", "", "optional button description")
	cmd.Flags().BoolVar(&newTab, "new-tab", true, "open link in a new tab (default true)")
	cmd.Flags().StringVar(&color, "color", "", "button color (e.g. #4285f4)")
	cmd.Flags().StringVar(&style, "style", "", "button style: filled | outlined | transparent")
	cmd.Flags().StringVar(&layout, "layout", `{"startX":0,"spanX":24,"startY":0,"spanY":8}`, "widget layout on the 96-column grid (JSON)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("title")
	return markJSON(cmd)
}

func newDashboardsButtonEditCmd() *cobra.Command {
	var chartID, label, url, description, color, style, layout string
	var newTab bool
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "edit <dashboard-id> --chart-id <id>",
		Short: "Edit a button widget's label, url, style, or layout (guarded)",
		Long: "Edit an existing button tile. Change any of --label, --url, --description,\n" +
			"--new-tab, --color, --style, --layout. At least one is required.\n" +
			"Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			layRaw, err := rawJSONOrNil("layout", layout)
			if err != nil {
				return err
			}
			hasLabel := cmd.Flags().Changed("label")
			hasURL := cmd.Flags().Changed("url")
			hasDesc := cmd.Flags().Changed("description")
			hasNewTab := cmd.Flags().Changed("new-tab")
			hasColor := cmd.Flags().Changed("color")
			hasStyle := cmd.Flags().Changed("style")
			changingButton := hasLabel || hasURL || hasDesc || hasNewTab || hasColor || hasStyle
			if !changingButton && layRaw == nil {
				return fmt.Errorf("nothing to edit: pass --label, --url, --description, --new-tab, --color, --style, and/or --layout")
			}
			if hasStyle {
				switch strings.ToLower(style) {
				case "filled", "outlined", "transparent", "":
				default:
					return fmt.Errorf("invalid --style %q (want filled | outlined | transparent)", style)
				}
			}

			target := fmt.Sprintf("edit button %s in dashboard %s", chartID, id)
			return guardedSIEMMutation(target, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				ctx := baseContext()

				chart, gerr := c.GetChart(ctx, chartID)
				if gerr != nil {
					return gerr
				}
				curLabel := nestedString(chart, "visualization", "button", "label")
				curURL := nestedString(chart, "visualization", "button", "hyperlink")
				curDesc := nestedString(chart, "visualization", "button", "description")
				curColor := nestedString(chart, "visualization", "button", "properties", "color")
				curStyle := nestedString(chart, "visualization", "button", "properties", "buttonStyle")
				var curNewTab bool
				if v := extractRaw(extractRaw(chart, "visualization"), "button"); len(v) > 0 {
					var b struct {
						NewTab bool `json:"newTab"`
					}
					_ = json.Unmarshal(v, &b)
					curNewTab = b.NewTab
				}
				if hasLabel {
					curLabel = label
				}
				if hasURL {
					curURL = url
				}
				if hasDesc {
					curDesc = description
				}
				if hasNewTab {
					curNewTab = newTab
				}
				if hasColor {
					curColor = color
				}
				if hasStyle {
					curStyle = style
				}
				curLayout, lerr := currentChartLayout(ctx, c, id, chartID)
				if lerr != nil {
					return lerr
				}
				origLayout := curLayout
				if layRaw != nil {
					curLayout = layRaw
				}
				if _, rerr := c.RemoveChart(ctx, id, chartID); rerr != nil {
					return rerr
				}
				_, aerr := c.AddChart(ctx, id, chronicle.AddChartInput{
					DisplayName:   nestedString(chart, "displayName"),
					TileType:      chronicle.TileTypeButton,
					ChartLayout:   curLayout,
					Visualization: buttonVisualization(curLabel, curURL, curDesc, curColor, curStyle, curNewTab),
				})
				if aerr != nil {
					return restoreRemovedChart(ctx, c, id, chart, origLayout, aerr)
				}
				if !jsonOut {
					fmt.Printf("Edited button %s in dashboard %s. Re-pull to mirror it locally.\n", chartID, id)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&chartID, "chart-id", "", "id of the button widget to edit (required)")
	cmd.Flags().StringVar(&label, "label", "", "new button text")
	cmd.Flags().StringVar(&url, "url", "", "new hyperlink target")
	cmd.Flags().StringVar(&description, "description", "", "new button description")
	cmd.Flags().BoolVar(&newTab, "new-tab", true, "open link in a new tab")
	cmd.Flags().StringVar(&color, "color", "", "new button color")
	cmd.Flags().StringVar(&style, "style", "", "new button style: filled | outlined | transparent")
	cmd.Flags().StringVar(&layout, "layout", "", "new grid position (JSON)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("chart-id")
	return markJSON(cmd)
}

func newDashboardsButtonRemoveCmd() *cobra.Command {
	var chartID string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "remove <dashboard-id> --chart-id <id>",
		Short: "Remove a button widget from a dashboard (guarded)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			target := fmt.Sprintf("remove button %s from dashboard %s", chartID, id)
			return guardedSIEMMutation(target, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				if _, err := c.RemoveChart(baseContext(), id, chartID); err != nil {
					return err
				}
				if !jsonOut {
					fmt.Printf("Removed button %s from dashboard %s. Re-pull to mirror it locally.\n", chartID, id)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&chartID, "chart-id", "", "id of the button widget to remove (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("chart-id")
	return markJSON(cmd)
}
