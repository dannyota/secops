package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	Integration         string                `json:"integration,omitempty"`
	ID                  string                `json:"id,omitempty"`
	Name                string                `json:"name"`
	Description         string                `json:"description,omitempty"`
	Enabled             *bool                 `json:"enabled,omitempty"`
	ParameterCount      int                   `json:"parameter_count"`
	MandatoryParameters []string              `json:"mandatory_parameters,omitempty"`
	Parameters          []playbookActionParam `json:"parameters,omitempty"`
	HasJSONResult       *bool                 `json:"has_json_result,omitempty"`
	ScriptResultName    string                `json:"script_result_name,omitempty"`
	ActionType          string                `json:"action_type,omitempty"`
	Async               *bool                 `json:"async,omitempty"`
}

// playbookActionParam is one input parameter of an action — the schema an author
// needs to fill a step in (surfaced in --json; the table shows only the count).
type playbookActionParam struct {
	Name           string   `json:"name"`
	Type           string   `json:"type,omitempty"`
	Mandatory      bool     `json:"mandatory"`
	DefaultValue   string   `json:"default_value,omitempty"`
	OptionalValues []string `json:"optional_values,omitempty"`
	Description    string   `json:"description,omitempty"`
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
	return markJSON(cmd)
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
			"--integration, list that integration's actions in detail — `--json` carries\n" +
			"each action's full PARAMETER SCHEMA (name/type/mandatory/default/options), the\n" +
			"detail needed to author a step; the table shows counts, JSON/script-result\n" +
			"flags, and async status. Neither prints Python script bodies.",
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
			resolved, err := resolvePlaybookComponentIntegration(baseContext(), c, integration)
			if err != nil {
				return err
			}
			// Each action's parameter schema is only returned by the per-action GET —
			// the actions LIST omits parameters regardless of field mask (a server
			// quirk). So list the actions, then GET each one to capture its parameters,
			// and feed the full bodies to the summarizer.
			key := componentIntegrationIdentifier(resolved)
			defs, err := c.ListActions(baseContext(), key)
			if err != nil {
				return err
			}
			raws := make([]json.RawMessage, 0, len(defs))
			for i := range defs {
				full, gerr := c.GetActionDef(baseContext(), key, defs[i].PathID())
				if gerr != nil {
					fmt.Fprintf(os.Stderr, "warning: action %q: %v\n", defs[i].DisplayName, gerr)
					continue
				}
				raws = append(raws, full)
			}
			rows := filterActionRows(summarizeIntegrationActions(key, wrapActionsEnvelope(raws)), grep)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
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
	return markJSON(cmd)
}
