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
		Short: "Discover the playbook-authoring palette: integrations, actions, flow functions, triggers, blocks",
		Long: "Discover live SecOps components that can be used while authoring playbooks —\n" +
			"the designer's Step Selection palette as read-only catalogs: integrations,\n" +
			"actions (all integrations, or one in detail), flow functions/operators,\n" +
			"trigger vocabulary, blocks, jobs, and connectors. Local output is a planning\n" +
			"aid before SecOps validates and saves/runs the final workflow.",
	}
	cmd.AddCommand(
		newSOARPlaybookComponentsIntegrationsCmd(),
		newSOARPlaybookComponentsActionsCmd(),
		newSOARPlaybookComponentsJobsCmd(),
		newSOARPlaybookComponentsConnectorsCmd(),
		newSOARPlaybookComponentsUsageCmd(),
		newSOARPlaybookComponentsFlowCmd(),
		newSOARPlaybookComponentsTriggersCmd(),
		newSOARPlaybookComponentsBlocksCmd(),
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
		Use:   "actions [--integration <key>]",
		Short: "List playbook actions — the full cross-integration catalog, or one integration in detail",
		Long: "Without --integration, list the WHOLE action palette in one call (the\n" +
			"`integrations/-/actions` wildcard catalog): every action across every\n" +
			"integration with its numeric id — the id `components usage` keys on. With\n" +
			"--integration, list that integration's actions in detail (parameter counts,\n" +
			"JSON/script-result flags, async status). Neither prints Python script bodies.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			if strings.TrimSpace(integration) == "" {
				defs, err := c.ListAllActions(baseContext())
				if err != nil {
					return err
				}
				rows := actionCatalogRows(defs, grep)
				if jsonOut {
					return emitJSON(rows)
				}
				printActionCatalogRows(cmd.OutOrStdout(), rows)
				return nil
			}
			lc, err := newSOARLegacyClient()
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
	f.StringVar(&integration, "integration", "", "integration key/identifier/display name (omit for the all-integration catalog)")
	f.StringVar(&grep, "grep", "", "case-insensitive filter over action name/description")
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

// actionCatalogRow is one entry of the cross-integration action catalog
// (`integrations/-/actions`): the summary columns plus the numeric id.
type actionCatalogRow struct {
	ID          string `json:"id"`
	Integration string `json:"integration"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
	Async       bool   `json:"async"`
	Custom      bool   `json:"custom"`
}

func actionCatalogRows(defs []soar.ActionDef, grep string) []actionCatalogRow {
	rows := make([]actionCatalogRow, 0, len(defs))
	for i := range defs {
		d := &defs[i]
		row := actionCatalogRow{
			ID:          d.PathID(),
			Integration: d.Integration,
			Name:        d.DisplayName,
			Description: d.Description,
			Enabled:     d.Enabled,
			Async:       d.Async,
			Custom:      d.Custom,
		}
		if matchesAny(grep, row.ID, row.Integration, row.Name, row.Description) {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if a, b := strings.ToLower(rows[i].Integration), strings.ToLower(rows[j].Integration); a != b {
			return a < b
		}
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	return rows
}

func printActionCatalogRows(w io.Writer, rows []actionCatalogRow) {
	fmt.Fprintln(w, "ID\tINTEGRATION\tNAME\tENABLED\tASYNC\tCUSTOM")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%t\t%t\t%t\n", row.ID, row.Integration, row.Name, row.Enabled, row.Async, row.Custom)
	}
	fmt.Fprintf(w, "\n%d action(s) across all integrations\n", len(rows))
}

// flowFunctionRow is one Flow palette entry: a transformer (value function)
// or a logical operator (condition predicate).
type flowFunctionRow struct {
	Kind           string `json:"kind"` // "function" | "operator"
	ID             string `json:"id,omitempty"`
	Integration    string `json:"integration,omitempty"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	ExpectedInput  string `json:"expected_input,omitempty"`
	ExpectedOutput string `json:"expected_output,omitempty"`
	UsageExample   string `json:"usage_example,omitempty"`
	Enabled        bool   `json:"enabled"`
}

func newSOARPlaybookComponentsFlowCmd() *cobra.Command {
	var (
		kind string
		grep string
	)
	cmd := &cobra.Command{
		Use:   "flow [--kind functions|operators|all]",
		Short: "List Flow palette utilities: transformers (functions) and logical operators (condition predicates)",
		Long: "List the Flow building blocks usable inside playbook expressions and\n" +
			"conditions: transformers (value-shaping functions, e.g. trimChars) and\n" +
			"logical operators (condition predicates, e.g. Empty / Not Empty) — the\n" +
			"`integrations/-/{transformers,logicalOperators}` wildcard catalogs.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			kind = strings.ToLower(strings.TrimSpace(kind))
			switch kind {
			case "", "all", "functions", "operators":
			default:
				return fmt.Errorf("--kind must be functions, operators, or all")
			}
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			var rows []flowFunctionRow
			if kind == "" || kind == "all" || kind == "functions" {
				fns, err := c.ListTransformers(ctx)
				if err != nil {
					return err
				}
				rows = append(rows, flowFunctionRows("function", fns, grep)...)
			}
			if kind == "" || kind == "all" || kind == "operators" {
				ops, err := c.ListLogicalOperators(ctx)
				if err != nil {
					return err
				}
				rows = append(rows, flowFunctionRows("operator", ops, grep)...)
			}
			if jsonOut {
				return emitJSON(rows)
			}
			printFlowFunctionRows(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&kind, "kind", "all", "which palette to list: functions (transformers), operators (logical operators), or all")
	f.StringVar(&grep, "grep", "", "case-insensitive filter over name/description/example")
	return cmd
}

func flowFunctionRows(kind string, fns []soar.FlowFunction, grep string) []flowFunctionRow {
	rows := make([]flowFunctionRow, 0, len(fns))
	for i := range fns {
		fn := &fns[i]
		row := flowFunctionRow{
			Kind:           kind,
			ID:             fn.ID.String(),
			Integration:    fn.Integration,
			Name:           fn.DisplayName,
			Description:    fn.Description,
			ExpectedInput:  fn.ExpectedInput,
			ExpectedOutput: fn.ExpectedOutput,
			UsageExample:   fn.UsageExample,
			Enabled:        fn.Enabled,
		}
		if matchesAny(grep, row.Name, row.Description, row.UsageExample, row.Integration) {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name) })
	return rows
}

func printFlowFunctionRows(w io.Writer, rows []flowFunctionRow) {
	fmt.Fprintln(w, "KIND\tNAME\tINTEGRATION\tEXAMPLE\tDESCRIPTION")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", row.Kind, row.Name, row.Integration,
			defaultString(row.UsageExample, "-"), truncateForRow(row.Description, 80))
	}
	fmt.Fprintf(w, "\n%d flow utilit(ies)\n", len(rows))
}

// truncateForRow collapses whitespace and trims a long free-text value for
// one-line table output (rune-safe via the shared truncate helper).
func truncateForRow(s string, n int) string {
	return truncate(strings.Join(strings.Fields(s), " "), n)
}

// The playbook trigger vocabulary. There is no API catalog for triggers — the
// designer's trigger kinds are condition presets over the alert/case that
// fires the playbook, and the saved trigger record's `type` is one of a small
// set of tokens. The mapping below reflects live playbook definitions.
type triggerVocabularyRow struct {
	Token    string `json:"token"`
	Designer string `json:"designer_kinds"`
	Meaning  string `json:"meaning"`
}

var triggerVocabulary = []triggerVocabularyRow{
	{
		Token:    "ALL",
		Designer: "All",
		Meaning:  "fire for every new alert (empty condition set)",
	},
	{
		Token:    "CASE_DATA",
		Designer: "Alert Trigger Value, Alert Type, Custom List, Custom Trigger, Network Name, Product Name, Tag Name",
		Meaning:  "conditional trigger: AND/OR condition groups over alert/case/event fields (each designer kind is a condition preset; the saved record carries the conditions)",
	},
	{
		Token:    "GET_INPUTS",
		Designer: "(blocks)",
		Meaning:  "block-input trigger — the entry point of a playbook BLOCK, fed by the parent playbook's inputs",
	},
}

func newSOARPlaybookComponentsTriggersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "triggers",
		Short: "The playbook trigger vocabulary (offline — no API catalog exists for triggers)",
		Long: "Print the playbook trigger vocabulary: the designer's trigger kinds and the\n" +
			"`type` tokens saved on playbook definitions. Triggers have no list API — the\n" +
			"kinds are condition presets; inspect a pulled playbook's `trigger` record\n" +
			"(`soar pull playbooks`) for the exact conditions a kind produces. Offline.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut {
				return emitJSON(triggerVocabulary)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintln(w, "TOKEN\tDESIGNER_KINDS\tMEANING")
			for _, row := range triggerVocabulary {
				fmt.Fprintf(w, "%s\t%s\t%s\n", row.Token, row.Designer, row.Meaning)
			}
			fmt.Fprintln(w, "\nLoops are a designer construct (scoped step groups), not a trigger or a catalog resource.")
			return nil
		},
	}
	return cmd
}

func newSOARPlaybookComponentsBlocksCmd() *cobra.Command {
	var enabledOnly bool
	cmd := &cobra.Command{
		Use:   "blocks",
		Short: "List playbook BLOCKS (reusable nested playbooks callable as steps)",
		Long: "List the tenant's playbook blocks — reusable nested playbooks (playbookType\n" +
			"NESTED) that other playbooks call as steps. The same data as\n" +
			"`soar playbook list --type block`, surfaced here as part of the authoring\n" +
			"palette.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			cards, err := lc.ListPlaybooks(baseContext(), []string{"NESTED"})
			if err != nil {
				return err
			}
			rows := make([]soarPlaybookListRow, 0, len(cards))
			for _, card := range cards {
				if enabledOnly && !card.IsEnabled {
					continue
				}
				rows = append(rows, soarPlaybookListRow{
					Name:       card.Name,
					Enabled:    card.IsEnabled,
					Category:   card.CategoryName,
					ID:         card.ID.String(),
					Identifier: card.Identifier,
				})
			}
			sort.Slice(rows, func(i, j int) bool {
				return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
			})
			if jsonOut {
				return emitJSON(rows)
			}
			printSOARPlaybookRows(cmd.OutOrStdout(), rows)
			fmt.Fprintf(cmd.OutOrStdout(), "\n%d block(s)\n", len(rows))
			return nil
		},
	}
	cmd.Flags().BoolVar(&enabledOnly, "enabled", false, "only enabled blocks")
	return cmd
}
