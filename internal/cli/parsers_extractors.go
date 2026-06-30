package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// newExtExtractCmd discovers extractable raw-log fields and optionally creates
// an extractor parser extension from selected fields.
//
// Without --fields/--all: read-only discovery (shows what the console's
// "Extract Additional Fields" panel shows).
// With --fields f1,f2 or --all --yes: creates a parserExtension with
// dynamicParsing.optedFields (mutation, guarded).
func newExtExtractCmd() *cobra.Command {
	var (
		fields string
		all    bool
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "extract <log-type>",
		Short: "Discover extractable fields, or create an extractor extension",
		Long: "Without --fields or --all: read-only discovery of raw-log fields that\n" +
			"can be extracted to UDM (equivalent to the console's Extract Additional\n" +
			"Fields panel).\n\n" +
			"With --fields f1,f2 or --all: create a parser extension (extractor type)\n" +
			"that maps the selected fields. Guarded; pass --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logType := strings.TrimSpace(args[0])
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			logs, err := c.ListLogs(baseContext(), logType, 1, "")
			if err != nil {
				return err
			}
			if len(logs) == 0 {
				fmt.Fprintf(os.Stderr, "no sample logs for %q — cannot generate field mappings\n", logType)
				return nil
			}
			mappings, err := c.GenerateUdmKeyValueMappings(baseContext(), logs[0].Data, "JSON")
			if err != nil {
				return err
			}
			if len(mappings) == 0 {
				fmt.Fprintf(os.Stderr, "no extractable fields for %q\n", logType)
				return nil
			}

			if fields == "" && !all {
				showFieldMappings(mappings)
				return nil
			}
			var selected []extractField
			if all {
				for k, v := range mappings {
					selected = append(selected, extractField{Path: k, SampleValue: v})
				}
			} else {
				for f := range strings.SplitSeq(fields, ",") {
					f = strings.TrimSpace(f)
					if f == "" {
						continue
					}
					v, ok := mappings[f]
					if !ok {
						return fmt.Errorf("field %q not found in extractable fields", f)
					}
					selected = append(selected, extractField{Path: f, SampleValue: v})
				}
			}
			sort.Slice(selected, func(i, j int) bool { return selected[i].Path < selected[j].Path })

			fmt.Fprintf(os.Stdout, "Create extractor extension for %s with %d field(s):\n", logType, len(selected))
			for _, f := range selected {
				fmt.Fprintf(os.Stdout, "  %s\n", f.Path)
			}

			action := fmt.Sprintf("parsers extension extract %s (%d fields)", logType, len(selected))
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				dp := map[string]any{"optedFields": selected}
				cfg := &chronicle.ParserExtensionConfig{
					DynamicParsing: dp,
					Log:            logs[0].Data,
				}
				ext, cerr := c.CreateParserExtension(baseContext(), logType, cfg)
				if cerr != nil {
					return cerr
				}
				if jsonOut {
					return emitJSON(ext)
				}
				fmt.Fprintf(os.Stdout, "\nCreated extractor extension %s (state %s).\n", lastSegment(ext.Name), orDash(ext.State))
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&fields, "fields", "", "comma-separated field names to extract")
	f.BoolVar(&all, "all", false, "extract all discovered fields")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply the change")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	cmd.MarkFlagsMutuallyExclusive("fields", "all")
	return markJSON(cmd)
}

type extractField struct {
	Path        string `json:"path"`
	SampleValue string `json:"sampleValue"`
}

func showFieldMappings(mappings map[string]string) {
	keys := make([]string, 0, len(mappings))
	for k := range mappings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "RAW LOG FIELD\tSAMPLE VALUE")
	for _, k := range keys {
		fmt.Fprintf(tw, "%s\t%s\n", k, truncate(mappings[k], 80))
	}
	_ = tw.Flush()
	fmt.Fprintf(os.Stdout, "\n%d field(s). Use --fields or --all to create an extractor extension.\n", len(mappings))
}

// newExtSettingCmd reads or updates the per-log-type extraction setting
// (autonomousParsingExtractionType: OPT_IN / ALL_FIELDS / DISABLED).
func newExtSettingCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "setting <log-type> [opt-in|all|disabled]",
		Short: "Show or update the auto-extraction setting for a log type",
		Long: "Without a second argument, shows the current extraction setting (OPT_IN,\n" +
			"ALL_FIELDS, or DISABLED). With a setting, updates it (guarded).\n\n" +
			"  opt-in    extract only explicitly selected fields\n" +
			"  all       extract all fields (up to 100)\n" +
			"  disabled  turn off auto-extraction",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			logType := strings.TrimSpace(args[0])
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				setting, serr := c.GetLogTypeSetting(baseContext(), logType)
				if serr != nil {
					return serr
				}
				if jsonOut {
					return emitJSON(setting.Raw)
				}
				var parsed struct {
					ExtractionType string `json:"autonomousParsingExtractionType"`
					DedupeEnabled  bool   `json:"autonomousParsingDedupeEnabled"`
					MaxFields      int    `json:"autonomousParsingMaxOptinFields"`
					MaxArray       int    `json:"autonomousParsingMaxArrayElements"`
				}
				_ = json.Unmarshal(setting.Raw, &parsed)
				fmt.Fprintf(os.Stdout, "Log type:     %s\n", logType)
				fmt.Fprintf(os.Stdout, "Extraction:   %s\n", orDash(parsed.ExtractionType))
				fmt.Fprintf(os.Stdout, "Dedupe:       %v\n", parsed.DedupeEnabled)
				if parsed.MaxFields > 0 {
					fmt.Fprintf(os.Stdout, "Max fields:   %d\n", parsed.MaxFields)
				}
				if parsed.MaxArray > 0 {
					fmt.Fprintf(os.Stdout, "Max array:    %d\n", parsed.MaxArray)
				}
				return nil
			}
			settingMap := map[string]string{
				"opt-in":   "OPT_IN",
				"all":      "ALL_FIELDS",
				"disabled": "DISABLED",
			}
			apiVal, ok := settingMap[strings.ToLower(args[1])]
			if !ok {
				return fmt.Errorf("unknown extraction setting %q — use opt-in, all, or disabled", args[1])
			}
			action := fmt.Sprintf("parsers extension setting %s %s", logType, apiVal)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				body, _ := json.Marshal(map[string]string{
					"autonomousParsingExtractionType": apiVal,
				})
				updated, uerr := c.UpdateLogTypeSetting(baseContext(), logType, body)
				if uerr != nil {
					return uerr
				}
				if jsonOut {
					return emitJSON(updated.Raw)
				}
				fmt.Fprintf(os.Stdout, "Updated extraction setting for %q to %s.\n", logType, apiVal)
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply the change")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}
