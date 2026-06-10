package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
)

type playbookComponentIntegrationRow struct {
	Identifier           string `json:"identifier"`
	DisplayName          string `json:"display_name,omitempty"`
	ProductionIdentifier string `json:"production_identifier,omitempty"`
	Custom               bool   `json:"custom"`
	Certified            bool   `json:"certified"`
	Internal             bool   `json:"internal"`
}

type playbookActionRow struct {
	Integration         string   `json:"integration,omitempty"`
	ID                  string   `json:"id,omitempty"`
	Name                string   `json:"name"`
	Description         string   `json:"description,omitempty"`
	Enabled             *bool    `json:"enabled,omitempty"`
	ParameterCount      int      `json:"parameter_count"`
	MandatoryParameters []string `json:"mandatory_parameters,omitempty"`
	HasJSONResult       *bool    `json:"has_json_result,omitempty"`
	ScriptResultName    string   `json:"script_result_name,omitempty"`
	ActionType          string   `json:"action_type,omitempty"`
	Async               *bool    `json:"async,omitempty"`
}

type playbookConnectorComponentRow struct {
	Integration string `json:"integration"`
	ID          string `json:"id,omitempty"`
	Identifier  string `json:"identifier,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Custom      bool   `json:"custom"`
}

type playbookJobComponentRow struct {
	Integration string `json:"integration"`
	ID          string `json:"id,omitempty"`
	Identifier  string `json:"identifier,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

func newSOARPlaybookComponentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "components",
		Short: "Discover integrations, actions, jobs, and connectors for playbook authoring",
		Long: "Discover live SecOps components that can be used while authoring playbooks.\n" +
			"These commands are read-only catalogs; local output is a planning aid before\n" +
			"SecOps validates and saves/runs the final workflow.",
	}
	cmd.AddCommand(
		newSOARPlaybookComponentsIntegrationsCmd(),
		newSOARPlaybookComponentsActionsCmd(),
		newSOARPlaybookComponentsJobsCmd(),
		newSOARPlaybookComponentsConnectorsCmd(),
		newSOARPlaybookComponentsUsageCmd(),
	)
	return cmd
}

func newSOARPlaybookComponentsIntegrationsCmd() *cobra.Command {
	var (
		grep string
		all  bool
	)
	cmd := &cobra.Command{
		Use:   "integrations",
		Short: "List installed integrations usable as playbook component sources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			ints, err := c.ListIntegrations(baseContext())
			if err != nil {
				return err
			}
			rows := playbookIntegrationRows(ints, grep, all)
			if jsonOut {
				return emitJSON(rows)
			}
			printPlaybookIntegrationRows(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&grep, "grep", "", "case-insensitive filter over identifier/name")
	f.BoolVar(&all, "all", false, "include internal platform integrations")
	return cmd
}

func newSOARPlaybookComponentsActionsCmd() *cobra.Command {
	var (
		integration string
		grep        string
	)
	cmd := &cobra.Command{
		Use:   "actions --integration <key>",
		Short: "List playbook actions exposed by one integration",
		Long: "List action definitions from SecOps integration full details. Human output\n" +
			"prints action names, parameter counts, JSON/script-result flags, and async\n" +
			"status; it does not print Python script bodies.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			resolved, err := resolvePlaybookComponentIntegration(baseContext(), c, integration)
			if err != nil {
				return err
			}
			raw, err := lc.GetStoreIntegrationFullDetails(baseContext(), integrationFullDetailsBody(resolved))
			if err != nil {
				return err
			}
			rows := filterActionRows(summarizeIntegrationActions(storeIntegrationIdentifier(resolved), raw), grep)
			if jsonOut {
				return emitJSON(rows)
			}
			printPlaybookActionRows(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration key/identifier/display name (required)")
	f.StringVar(&grep, "grep", "", "case-insensitive filter over action name/description")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}

func newSOARPlaybookComponentsJobsCmd() *cobra.Command {
	var (
		integration string
		grep        string
	)
	cmd := &cobra.Command{
		Use:   "jobs --integration <key>",
		Short: "List job definitions inside one integration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			resolved, err := resolvePlaybookComponentIntegration(baseContext(), c, integration)
			if err != nil {
				return err
			}
			key := componentIntegrationIdentifier(resolved)
			jobs, err := c.ListJobs(baseContext(), key)
			if err != nil {
				return err
			}
			rows := make([]playbookJobComponentRow, 0, len(jobs))
			for _, job := range jobs {
				row := playbookJobComponentRow{
					Integration: key,
					ID:          job.ID.String(),
					Identifier:  job.Identifier,
					DisplayName: job.DisplayName,
				}
				if matchesAny(grep, row.ID, row.Identifier, row.DisplayName) {
					rows = append(rows, row)
				}
			}
			sort.Slice(rows, func(i, j int) bool {
				return strings.ToLower(rows[i].DisplayName) < strings.ToLower(rows[j].DisplayName)
			})
			if jsonOut {
				return emitJSON(rows)
			}
			printPlaybookJobRows(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration key/identifier/display name (required)")
	f.StringVar(&grep, "grep", "", "case-insensitive filter over job id/name")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}

func newSOARPlaybookComponentsConnectorsCmd() *cobra.Command {
	var (
		integration string
		grep        string
	)
	cmd := &cobra.Command{
		Use:   "connectors --integration <key>",
		Short: "List connector definitions inside one integration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			resolved, err := resolvePlaybookComponentIntegration(baseContext(), c, integration)
			if err != nil {
				return err
			}
			key := componentIntegrationIdentifier(resolved)
			connectors, err := c.ListConnectors(baseContext(), key)
			if err != nil {
				return err
			}
			rows := make([]playbookConnectorComponentRow, 0, len(connectors))
			for _, conn := range connectors {
				row := playbookConnectorComponentRow{
					Integration: key,
					ID:          conn.ID.String(),
					Identifier:  conn.Identifier,
					DisplayName: conn.DisplayName,
					Custom:      conn.Custom,
				}
				if matchesAny(grep, row.ID, row.Identifier, row.DisplayName) {
					rows = append(rows, row)
				}
			}
			sort.Slice(rows, func(i, j int) bool {
				return strings.ToLower(rows[i].DisplayName) < strings.ToLower(rows[j].DisplayName)
			})
			if jsonOut {
				return emitJSON(rows)
			}
			printPlaybookConnectorRows(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration key/identifier/display name (required)")
	f.StringVar(&grep, "grep", "", "case-insensitive filter over connector id/name")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}

func playbookIntegrationRows(ints []soar.Integration, grep string, includeInternal bool) []playbookComponentIntegrationRow {
	rows := make([]playbookComponentIntegrationRow, 0, len(ints))
	for _, in := range ints {
		if in.Internal && !includeInternal {
			continue
		}
		row := playbookComponentIntegrationRow{
			Identifier:           in.Identifier,
			DisplayName:          in.DisplayName,
			ProductionIdentifier: in.ProdIdentifier,
			Custom:               in.Custom,
			Certified:            in.Certified,
			Internal:             in.Internal,
		}
		if matchesAny(grep, row.Identifier, row.DisplayName, row.ProductionIdentifier) {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i].Identifier) < strings.ToLower(rows[j].Identifier) })
	return rows
}

func resolvePlaybookComponentIntegration(ctx context.Context, c *soar.Client, key string) (soar.Integration, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return soar.Integration{}, fmt.Errorf("--integration is required")
	}
	ints, err := c.ListIntegrations(ctx)
	if err != nil {
		return soar.Integration{}, err
	}
	var matches []soar.Integration
	for _, in := range ints {
		if in.Identifier == key || in.DisplayName == key || in.Name == key || in.ProdIdentifier == key {
			matches = append(matches, in)
		}
	}
	if len(matches) == 0 {
		return soar.Integration{Identifier: key}, nil
	}
	var idMatches []soar.Integration
	for _, in := range matches {
		if in.Identifier == key {
			idMatches = append(idMatches, in)
		}
	}
	if len(idMatches) == 1 {
		return idMatches[0], nil
	}
	if len(idMatches) > 1 {
		return soar.Integration{}, fmt.Errorf("%q is ambiguous (%d integrations); use the unique identifier", key, len(idMatches))
	}
	var prodMatches []soar.Integration
	for _, in := range matches {
		if in.ProdIdentifier == key {
			prodMatches = append(prodMatches, in)
		}
	}
	if len(prodMatches) == 1 {
		return prodMatches[0], nil
	}
	if len(prodMatches) > 1 {
		return soar.Integration{}, fmt.Errorf("%q is ambiguous (%d integrations); use the installed integration identifier", key, len(prodMatches))
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return soar.Integration{}, fmt.Errorf("%q is ambiguous (%d integrations); use the unique identifier", key, len(matches))
}

func componentIntegrationIdentifier(in soar.Integration) string {
	return strings.TrimSpace(in.Identifier)
}

func storeIntegrationIdentifier(in soar.Integration) string {
	if strings.TrimSpace(in.ProdIdentifier) != "" {
		return strings.TrimSpace(in.ProdIdentifier)
	}
	return componentIntegrationIdentifier(in)
}

func integrationFullDetailsBody(in soar.Integration) map[string]any {
	return map[string]any{
		"integrationIdentifier": storeIntegrationIdentifier(in),
		"isCustom":              in.Custom,
		"isCertified":           in.Certified,
	}
}

func summarizeIntegrationActions(integration string, raw json.RawMessage) []playbookActionRow {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var rows []playbookActionRow
	collectActionRows(integration, "", root, seen, &rows)
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	return rows
}

func collectActionRows(integration, key string, v any, seen map[string]struct{}, rows *[]playbookActionRow) {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			if m, ok := item.(map[string]any); ok && actionArrayKey(key) {
				if row, ok := actionRowFromMap(integration, m, true); ok {
					addActionRow(row, seen, rows)
				}
			}
			collectActionRows(integration, key, item, seen, rows)
		}
	case map[string]any:
		if row, ok := actionRowFromMap(integration, x, false); ok {
			addActionRow(row, seen, rows)
		}
		for childKey, child := range x {
			collectActionRows(integration, childKey, child, seen, rows)
		}
	}
}

func actionArrayKey(key string) bool {
	switch strings.ToLower(key) {
	case "actions", "integrationactions", "supportedactions", "integrationsupportedactions":
		return true
	default:
		return false
	}
}

func actionRowFromMap(integration string, m map[string]any, fromActionArray bool) (playbookActionRow, bool) {
	name := stringAnyField(m, "name")
	if name == "" {
		name = stringAnyField(m, "displayName")
	}
	if name == "" {
		return playbookActionRow{}, false
	}
	if !fromActionArray && !looksLikeActionDefinition(m) {
		return playbookActionRow{}, false
	}
	row := playbookActionRow{
		Integration:         integration,
		ID:                  scalarAnyString(m["id"]),
		Name:                name,
		Description:         stringAnyField(m, "description"),
		ParameterCount:      arrayAnyLen(m["parameters"]),
		MandatoryParameters: mandatoryActionParameters(m["parameters"]),
		ScriptResultName:    stringAnyField(m, "scriptResultName"),
		ActionType:          scalarAnyString(m["actionType"]),
	}
	if b, ok := boolAnyField(m, "isEnabled"); ok {
		row.Enabled = &b
	}
	if b, ok := boolAnyField(m, "hasJsonResult"); ok {
		row.HasJSONResult = &b
	}
	if b, ok := boolAnyField(m, "isAsync"); ok {
		row.Async = &b
	}
	return row, true
}

func looksLikeActionDefinition(m map[string]any) bool {
	for _, key := range []string{
		"scriptResultName",
		"dynamicResultsMetadata",
		"hasJsonResult",
		"actionType",
		"isAsync",
		"actionWidgetTemplateIdentifier",
		"defaultResultValue",
		"timeoutSeconds",
	} {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func addActionRow(row playbookActionRow, seen map[string]struct{}, rows *[]playbookActionRow) {
	key := row.Integration + "\x00" + row.ID + "\x00" + row.Name
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*rows = append(*rows, row)
}

func filterActionRows(rows []playbookActionRow, grep string) []playbookActionRow {
	if strings.TrimSpace(grep) == "" {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		if matchesAny(grep, row.Name, row.Description, row.ID, row.ActionType) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func mandatoryActionParameters(v any) []string {
	params, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, raw := range params {
		param, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		mandatory, ok := boolAnyField(param, "isMandatory")
		if !ok || !mandatory {
			continue
		}
		if name := stringAnyField(param, "name"); name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func printPlaybookIntegrationRows(w io.Writer, rows []playbookComponentIntegrationRow) {
	fmt.Fprintln(w, "IDENTIFIER\tDISPLAY_NAME\tPRODUCTION\tCUSTOM\tCERTIFIED")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%t\t%t\n", row.Identifier, row.DisplayName, row.ProductionIdentifier, row.Custom, row.Certified)
	}
	fmt.Fprintf(w, "\n%d integration(s)\n", len(rows))
}

func printPlaybookActionRows(w io.Writer, rows []playbookActionRow) {
	fmt.Fprintln(w, "ENABLED\tNAME\tPARAMS\tMANDATORY\tJSON_RESULT\tSCRIPT_RESULT\tASYNC\tTYPE")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n",
			boolPtrString(row.Enabled),
			row.Name,
			row.ParameterCount,
			strings.Join(row.MandatoryParameters, ","),
			boolPtrString(row.HasJSONResult),
			defaultString(row.ScriptResultName, "-"),
			boolPtrString(row.Async),
			defaultString(row.ActionType, "-"))
	}
	fmt.Fprintf(w, "\n%d action(s)\n", len(rows))
}

func printPlaybookConnectorRows(w io.Writer, rows []playbookConnectorComponentRow) {
	fmt.Fprintln(w, "ID\tIDENTIFIER\tDISPLAY_NAME\tCUSTOM")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%t\n", row.ID, row.Identifier, row.DisplayName, row.Custom)
	}
	fmt.Fprintf(w, "\n%d connector definition(s)\n", len(rows))
}

func printPlaybookJobRows(w io.Writer, rows []playbookJobComponentRow) {
	fmt.Fprintln(w, "ID\tIDENTIFIER\tDISPLAY_NAME")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\n", row.ID, row.Identifier, row.DisplayName)
	}
	fmt.Fprintf(w, "\n%d job definition(s)\n", len(rows))
}

func matchesAny(needle string, values ...string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func boolPtrString(b *bool) string {
	if b == nil {
		return "-"
	}
	if *b {
		return "true"
	}
	return "false"
}

func stringAnyField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func boolAnyField(m map[string]any, key string) (bool, bool) {
	b, ok := m[key].(bool)
	return b, ok
}

func arrayAnyLen(v any) int {
	arr, ok := v.([]any)
	if !ok {
		return 0
	}
	return len(arr)
}

func scalarAnyString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return fmt.Sprintf("%g", x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", x)
	}
}
