package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// Markdown-widget commands: add, edit, and remove markdown tiles on a
// dashboard. Markdown tiles use tileType TILE_TYPE_MARKDOWN with
// visualization.markdown.content (no query/datasource).

// markdownVisualization builds the visualization JSON for a markdown tile.
func markdownVisualization(content, bgColor string) json.RawMessage {
	type mdProps struct {
		BackgroundColor string `json:"backgroundColor,omitempty"`
	}
	type md struct {
		Content    string   `json:"content"`
		Properties *mdProps `json:"properties,omitempty"`
	}
	type vis struct {
		Markdown md `json:"markdown"`
	}
	var props *mdProps
	if bgColor != "" {
		props = &mdProps{BackgroundColor: bgColor}
	}
	b, _ := json.Marshal(vis{Markdown: md{Content: content, Properties: props}})
	return b
}

// readMarkdownText returns the markdown content from --text (inline) or
// --text-file. At least one must be non-empty.
func readMarkdownText(text, textFile string) (string, error) {
	if textFile != "" {
		b, err := os.ReadFile(textFile)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return text, nil
}

func newDashboardsMarkdownGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "markdown <verb>",
		Short: "Markdown widget operations: add, edit, remove",
	}
	cmd.AddCommand(
		newDashboardsMarkdownAddCmd(),
		newDashboardsMarkdownEditCmd(),
		newDashboardsMarkdownRemoveCmd(),
	)
	return cmd
}

func newDashboardsMarkdownAddCmd() *cobra.Command {
	var title, text, textFile, bgColor, layout string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "add <dashboard-id> --title <t> (--text <md> | --text-file <f>)",
		Short: "Add a markdown widget to a dashboard (guarded)",
		Long: "Add a markdown tile to a native dashboard via :addChart. Markdown tiles\n" +
			"render static text (headings, lists, links) on the dashboard grid — no\n" +
			"query or datasource. The content comes from --text (inline) or --text-file.\n" +
			"Guarded: dry-run by default, --yes to apply. Re-pull afterwards.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			content, err := readMarkdownText(text, textFile)
			if err != nil {
				return err
			}
			if strings.TrimSpace(content) == "" {
				return fmt.Errorf("markdown content is empty: pass --text or --text-file")
			}

			target := fmt.Sprintf("add markdown %q to dashboard %s", title, id)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would add markdown %q to dashboard %s:\n%s\nRe-run with --yes.\n",
					title, id, content)
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to add markdown without confirmation (pass --yes). Aborted.")
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
				TileType:      chronicle.TileTypeMarkdown,
				ChartLayout:   lay,
				Visualization: markdownVisualization(content, bgColor),
			})
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(resp)
			}
			if cid := lastSegment(nestedString(resp, "dashboardChart", "name")); cid != "" {
				fmt.Printf("Added markdown %q (id %s) to dashboard %s. Re-pull to mirror it locally.\n", title, cid, id)
			} else {
				fmt.Printf("Added markdown %q to dashboard %s. Re-pull to mirror it locally.\n", title, id)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "widget display name (required)")
	cmd.Flags().StringVar(&text, "text", "", "inline markdown content")
	cmd.Flags().StringVar(&textFile, "text-file", "", "read markdown content from a file")
	cmd.Flags().StringVar(&bgColor, "background-color", "", "optional background color (e.g. #f0f0f0)")
	cmd.Flags().StringVar(&layout, "layout", `{"startX":0,"spanX":96,"startY":0,"spanY":8}`, "widget layout on the 96-column grid (JSON)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	cmd.MarkFlagsMutuallyExclusive("text", "text-file")
	_ = cmd.MarkFlagRequired("title")
	return markJSON(cmd)
}

func newDashboardsMarkdownEditCmd() *cobra.Command {
	var chartID, text, textFile, bgColor, layout string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "edit <dashboard-id> --chart-id <id>",
		Short: "Edit a markdown widget's content, color, or layout (guarded)",
		Long: "Edit an existing markdown tile. Change any of:\n" +
			"  --text / --text-file     the markdown content;\n" +
			"  --background-color       the tile background;\n" +
			"  --layout <json>          the grid position (startX/startY/spanX/spanY).\n" +
			"At least one is required. Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			content, err := readMarkdownText(text, textFile)
			if err != nil {
				return err
			}
			layRaw, err := rawJSONOrNil("layout", layout)
			if err != nil {
				return err
			}
			changingContent := content != "" || bgColor != ""
			if !changingContent && layRaw == nil {
				return fmt.Errorf("nothing to edit: pass --text/--text-file, --background-color, and/or --layout")
			}

			target := fmt.Sprintf("edit markdown %s in dashboard %s", chartID, id)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would edit markdown %s in dashboard %s (content=%v color=%v layout=%v). Re-run with --yes.\n",
					chartID, id, content != "", bgColor != "", layout != "")
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to edit markdown without confirmation (pass --yes). Aborted.")
				return nil
			}

			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			chart, gerr := c.GetChart(ctx, chartID)
			if gerr != nil {
				return gerr
			}
			if content == "" {
				content = nestedString(chart, "visualization", "markdown", "content")
			}
			if bgColor == "" {
				bgColor = nestedString(chart, "visualization", "markdown", "properties", "backgroundColor")
			}
			curLayout, lerr := currentChartLayout(ctx, c, id, chartID)
			if lerr != nil {
				return lerr
			}
			if layRaw != nil {
				curLayout = layRaw
			}
			if _, rerr := c.RemoveChart(ctx, id, chartID); rerr != nil {
				return rerr
			}
			resp, aerr := c.AddChart(ctx, id, chronicle.AddChartInput{
				DisplayName:   nestedString(chart, "displayName"),
				TileType:      chronicle.TileTypeMarkdown,
				ChartLayout:   curLayout,
				Visualization: markdownVisualization(content, bgColor),
			})
			if aerr != nil {
				return aerr
			}
			_ = resp

			if jsonOut {
				return emitGuardedResult(target, false, true)
			}
			fmt.Printf("Edited markdown %s in dashboard %s. Re-pull to mirror it locally.\n", chartID, id)
			return nil
		},
	}
	cmd.Flags().StringVar(&chartID, "chart-id", "", "id of the markdown widget to edit (required)")
	cmd.Flags().StringVar(&text, "text", "", "new markdown content")
	cmd.Flags().StringVar(&textFile, "text-file", "", "read new markdown content from a file")
	cmd.Flags().StringVar(&bgColor, "background-color", "", "new background color")
	cmd.Flags().StringVar(&layout, "layout", "", "new grid position (JSON: startX/startY/spanX/spanY)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	cmd.MarkFlagsMutuallyExclusive("text", "text-file")
	_ = cmd.MarkFlagRequired("chart-id")
	return markJSON(cmd)
}

func newDashboardsMarkdownRemoveCmd() *cobra.Command {
	var chartID string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "remove <dashboard-id> --chart-id <id>",
		Short: "Remove a markdown widget from a dashboard (guarded)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			target := fmt.Sprintf("remove markdown %s from dashboard %s", chartID, id)
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would remove markdown %s from dashboard %s. Re-run with --yes.\n", chartID, id)
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to remove markdown without confirmation (pass --yes). Aborted.")
				return nil
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			if _, err := c.RemoveChart(baseContext(), id, chartID); err != nil {
				return err
			}
			if jsonOut {
				return emitGuardedResult(target, false, true)
			}
			fmt.Printf("Removed markdown %s from dashboard %s. Re-pull to mirror it locally.\n", chartID, id)
			return nil
		},
	}
	cmd.Flags().StringVar(&chartID, "chart-id", "", "id of the markdown widget to remove (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("chart-id")
	return markJSON(cmd)
}
