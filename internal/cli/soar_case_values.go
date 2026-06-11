package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

// soarValueAllRecords is the paged-list selector that asks for every record (mirrors
// the engine's allRecordsSelector).
var soarValueAllRecords = map[string]any{"searchTerm": "", "requestedPage": 0, "pageSize": 10000}

// newCaseValuesCmd lists the valid values for the --tag / --stage / --root-cause
// flags, so an operator can discover them in-tool instead of pulling a whole surface.
func newCaseValuesCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "values <tags|stages|root-causes>",
		Short: "Read-only: list valid values for --tag / --stage / --root-cause",
		Long: "List the configured values an operator can pass to the case verbs:\n" +
			"  tags         → `case tag`/`untag --tag`\n" +
			"  stages       → `case stage --stage`\n" +
			"  root-causes  → `case close --root-cause`\n" +
			"Reads the live tenant config; --json emits the values as an array.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"tags", "stages", "root-causes"},
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := strings.ToLower(strings.TrimSpace(args[0]))
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			var raw legacy.RawJSON
			var field string
			switch kind {
			case "tags":
				raw, err = lc.GetTagDefinitionsRecords(ctx, soarValueAllRecords)
				field = "name"
			case "stages":
				raw, err = lc.GetCaseStageDefinitionRecords(ctx, soarValueAllRecords)
				field = "name"
			case "root-causes", "rootcauses", "root_causes":
				raw, err = lc.GetRootCauseCloseRecords(ctx)
				field = "rootCause"
			default:
				return fmt.Errorf("unknown kind %q (want: tags | stages | root-causes)", args[0])
			}
			if err != nil {
				return err
			}

			values, err := extractValueField(raw, field)
			if err != nil {
				return err
			}
			if asJSON {
				return emitJSON(values)
			}
			for _, v := range values {
				fmt.Fprintln(os.Stdout, v)
			}
			fmt.Fprintf(os.Stderr, "\n%d %s\n", len(values), kind)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, jsonFlagHelp)
	return markJSON(cmd)
}

// extractValueField unwraps a list payload (objectsList wrap or flat array) and
// pulls the named string field from each record, returning sorted, de-duplicated,
// non-empty values.
func extractValueField(raw json.RawMessage, field string) ([]string, error) {
	records, err := rawListRecords(raw)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(records))
	out := make([]string, 0, len(records))
	for _, r := range records {
		var m map[string]json.RawMessage
		if json.Unmarshal(r, &m) != nil {
			continue
		}
		var v string
		if json.Unmarshal(m[field], &v) != nil || v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out, nil
}

// rawListRecords accepts either a flat JSON array of records or an objectsList /
// records-wrapped envelope (the legacy paged-list shape).
func rawListRecords(raw json.RawMessage) ([]json.RawMessage, error) {
	if t := bytes.TrimSpace(raw); len(t) > 0 && t[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	var wrap struct {
		ObjectsList []json.RawMessage `json:"objectsList"`
		Records     []json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	if len(wrap.ObjectsList) > 0 {
		return wrap.ObjectsList, nil
	}
	return wrap.Records, nil
}
