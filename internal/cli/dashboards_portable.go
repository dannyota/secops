package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// Dashboard portability: export a dashboard to a self-contained JSON document
// (the dashboard plus its charts and queries) and import one back in a single
// call. Together they make a dashboard portable — export, edit/version locally,
// re-import — and a faster build path than `duplicate` + per-chart `charts add`.

// extractExportPayload reduces a `nativeDashboards:export` response
// ({"inlineDestination":{"dashboards":[obj,...]}}) to the single import-shaped
// dashboard object. It is lenient: a document that is already that object
// (carries one of chronicle.ValidImportKeys) passes through unchanged, so
// `import` accepts both the raw export response and an extracted payload.
func extractExportPayload(raw json.RawMessage) (json.RawMessage, error) {
	var resp struct {
		InlineDestination *struct {
			Dashboards []json.RawMessage `json:"dashboards"`
		} `json:"inlineDestination"`
	}
	if err := json.Unmarshal(raw, &resp); err == nil && resp.InlineDestination != nil {
		if len(resp.InlineDestination.Dashboards) == 0 {
			return nil, fmt.Errorf("export returned no dashboards (the dashboard may not be exportable, e.g. a CURATED one)")
		}
		return resp.InlineDestination.Dashboards[0], nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("not a JSON object: %w", err)
	}
	for _, k := range chronicle.ValidImportKeys {
		if _, ok := probe[k]; ok {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("unexpected shape: expected inlineDestination.dashboards or one of %v", chronicle.ValidImportKeys)
}

// payloadSummary returns a short human description of an import-shaped payload
// (display name + chart/query counts) for the preview line.
func payloadSummary(payload json.RawMessage) string {
	var p struct {
		Dashboard struct {
			DisplayName string `json:"displayName"`
		} `json:"dashboard"`
		DashboardCharts  []json.RawMessage `json:"dashboardCharts"`
		DashboardQueries []json.RawMessage `json:"dashboardQueries"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return "(unparseable payload)"
	}
	name := p.Dashboard.DisplayName
	if name == "" {
		name = "(unnamed)"
	}
	return fmt.Sprintf("%q — %d chart(s), %d query(ies)", name, len(p.DashboardCharts), len(p.DashboardQueries))
}

// newDashboardsExportCmd writes a dashboard's self-contained export JSON
// (dashboard + charts + queries) to a file or stdout, ready for `import`.
func newDashboardsExportCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "export <dashboard-id>",
		Short: "Export a dashboard (with its charts + queries) to import-ready JSON (read-only)",
		Long: "Export a CUSTOM dashboard and everything it needs — the dashboard, its charts,\n" +
			"and their queries — as one self-contained JSON document, ready to re-create on\n" +
			"any instance with `dashboards import`. Writes to --out (or stdout). Read-only.\n" +
			"CURATED (Google-managed) dashboards are not exportable.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			raw, err := c.ExportDashboard(baseContext(), args[0])
			if err != nil {
				return err
			}
			payload, err := extractExportPayload(raw)
			if err != nil {
				return fmt.Errorf("parse export of %s: %w", args[0], err)
			}
			var buf bytes.Buffer
			if err := json.Indent(&buf, payload, "", "  "); err != nil {
				return err
			}
			data := append(buf.Bytes(), '\n')
			if out == "" || out == "-" {
				fmt.Print(string(data))
				return nil
			}
			if err := os.WriteFile(out, data, 0o644); err != nil {
				return err
			}
			fmt.Printf("Exported dashboard %s (%s) to %s.\n", args[0], payloadSummary(payload), out)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "write to this file (default: stdout)")
	return markJSON(cmd) // output is always a JSON document
}

// newDashboardsImportCmd creates a dashboard from an export JSON file in one
// call. Guarded: dry-run by default, --yes to apply.
func newDashboardsImportCmd() *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Import a dashboard from export JSON in one call (guarded)",
		Long: "Create a dashboard from a JSON document produced by `dashboards export` (or the\n" +
			"console's Export as JSON): the dashboard, its charts, and their queries are\n" +
			"created together in ONE call. The server mints fresh ids, so importing onto the\n" +
			"same instance yields an independent copy. Guarded: dry-run by default, --yes to\n" +
			"apply. Re-pull afterwards so local mirrors live.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			payload, err := extractExportPayload(json.RawMessage(data))
			if err != nil {
				return fmt.Errorf("parse %s: %w", args[0], err)
			}
			target := fmt.Sprintf("import dashboard %s from %s", payloadSummary(payload), args[0])
			dr, ay := soarGuard(target, dryRun, yes)
			if dr {
				if jsonOut {
					return emitGuardedResult(target, true, false)
				}
				fmt.Printf("DRY RUN — would import dashboard %s. Re-run with --yes.\n", payloadSummary(payload))
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult(target, false, false)
				}
				fmt.Println("Refusing to import without confirmation (pass --yes). Aborted.")
				return nil
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			res, err := c.ImportDashboard(baseContext(), payload)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(res)
			}
			fmt.Printf("Imported dashboard %s. Re-pull to mirror it locally.\n", payloadSummary(payload))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}
