package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

type enumEntry struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

type enumGroup struct {
	Enum    string      `json:"enum"`
	Source  string      `json:"source"`
	Entries []enumEntry `json:"entries"`
}

func newEnumsCmd() *cobra.Command {
	var fetchLive bool
	cmd := &cobra.Command{
		Use:   "enums",
		Short: "List known SOAR enum values (case priority, close reason, SLA, block-list, stages, categories)",
		Long: "Print every SOAR enum the SDK knows about — the integer-to-name\n" +
			"mappings the legacy API uses. With --live, also fetches dynamic\n" +
			"values from the instance (case stages, playbook categories).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			groups := staticEnums()

			if fetchLive {
				liveGroups, err := liveEnums()
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: could not fetch live enums: %v\n", err)
				} else {
					groups = append(groups, liveGroups...)
				}
			}

			if jsonOut {
				return emitJSON(groups)
			}
			printEnumGroups(cmd.OutOrStdout(), groups)
			return nil
		},
	}
	cmd.Flags().BoolVar(&fetchLive, "live", false, "also fetch dynamic values from the instance (stages, categories)")
	return markJSON(cmd)
}

func staticEnums() []enumGroup {
	return []enumGroup{
		{Enum: "CasePriority", Source: "sdk", Entries: []enumEntry{
			{"Informative", -1}, {"Low", 40}, {"Medium", 60}, {"High", 80}, {"Critical", 100},
		}},
		{Enum: "CloseReason", Source: "sdk", Entries: []enumEntry{
			{"Malicious", 0}, {"NotMalicious", 1}, {"Maintenance", 2}, {"Inconclusive", 3}, {"Unknown", 4},
		}},
		{Enum: "SlaProviderType", Source: "sdk", Entries: []enumEntry{
			{"AlertRuleGenerator", 2}, {"CaseStage", 3}, {"CasePriority", 4}, {"AlertPriority", 5},
		}},
		{Enum: "SlaPeriodType", Source: "sdk", Entries: []enumEntry{
			{"Minutes", 0}, {"Hours", 1}, {"Days", 2}, {"Seconds", 3},
		}},
		{Enum: "SlaAlertType", Source: "sdk", Entries: []enumEntry{
			{"AllAlerts", 0}, {"SpecificAlerts", 1},
		}},
		{Enum: "BlockListItemType", Source: "sdk", Entries: []enumEntry{
			{"HostName", 0},
			{"UserUniqName", 1},
			{"IP", 2},
			{"EventProduct", 3},
			{"EventVendor", 4},
			{"EventSignature", 5},
			{"MacAddress", 6},
			{"Entity", 7},
			{"Event", 8},
		}},
		{Enum: "BlockListScope", Source: "sdk", Entries: []enumEntry{
			{"All", 0}, {"ForAggregationOnly", 1}, {"ForGroupOnly", 2}, {"ForModel", 3}, {"ForCreationAlert", 4},
		}},
		{Enum: "WorkflowsStatus", Source: "sdk", Entries: []enumEntry{
			{"Faulted", 0},
			{"InProgress", 1},
			{"Completed", 2},
			{"PendingUserInput", 3},
			{"PendingPreviousSteps", 4},
			{"Started", 5},
			{"FaultedAndSkipped", 6},
			{"HandledTimedout", 7},
		}},
	}
}

func liveEnums() ([]enumGroup, error) {
	var groups []enumGroup

	lc, err := newSOARLegacyClient()
	if err != nil {
		return nil, err
	}
	ctx := baseContext()

	raw, err := lc.GetMetadata(ctx)
	if err == nil {
		var meta struct {
			Stages []string `json:"stages"`
		}
		if json.Unmarshal(raw, &meta) == nil && len(meta.Stages) > 0 {
			entries := make([]enumEntry, len(meta.Stages))
			for i, s := range meta.Stages {
				entries[i] = enumEntry{s, i}
			}
			groups = append(groups, enumGroup{Enum: "CaseStage", Source: "live", Entries: entries})
		}
	}

	raw, err = lc.ListWorkflowCategories(ctx)
	if err == nil {
		var cats []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &cats) == nil && len(cats) > 0 {
			entries := make([]enumEntry, len(cats))
			for i, c := range cats {
				entries[i] = enumEntry{c.Name, c.ID}
			}
			groups = append(groups, enumGroup{Enum: "PlaybookCategory", Source: "live", Entries: entries})
		}
	}

	return groups, nil
}

func printEnumGroups(w io.Writer, groups []enumGroup) {
	for i, g := range groups {
		if i > 0 {
			fmt.Fprintln(w)
		}
		src := ""
		if g.Source == "live" {
			src = " (live)"
		}
		fmt.Fprintf(w, "%s%s\n", g.Enum, src)
		fmt.Fprintf(w, "%s\n", strings.Repeat("─", len(g.Enum)+len(src)))
		for _, e := range g.Entries {
			fmt.Fprintf(w, "  %-25s %v\n", e.Name, e.Value)
		}
	}
}
