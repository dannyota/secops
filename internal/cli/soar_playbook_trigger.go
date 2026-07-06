package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

type soarPlaybookTriggerSetResult struct {
	Output string   `json:"output"`
	Fields []string `json:"fields"`
}

func newSOARPlaybookTriggerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trigger",
		Short: "Manage playbook trigger fields offline and list live trigger tag values",
		Long: "Edit playbook trigger fields in exported JSON without calling SOAR.\n" +
			"The output is a reviewable artifact for `playbooks validate` and\n" +
			"`soar push playbook --dry-run` before any live save. `trigger tags` reads\n" +
			"the live tag vocabulary a Tag-Name condition can reference.",
	}
	cmd.AddCommand(newSOARPlaybookTriggerSetCmd(), newSOARPlaybookTriggerTagsCmd())
	return cmd
}

func newSOARPlaybookTriggerSetCmd() *cobra.Command {
	var (
		file               string
		out                string
		enabled            string
		triggerEnabled     string
		triggerType        string
		executionMode      string
		cron               string
		conditions         string
		reactionConditions string
	)
	cmd := &cobra.Command{
		Use:   "set --file <playbook.json> --out <playbook.json> [flags]",
		Short: "Set reviewable playbook trigger fields offline",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			opts := legacy.PlaybookTriggerPatchOptions{}
			var fields []string
			if v, ok, err := optionalBoolFlag("--enabled", enabled); err != nil {
				return err
			} else if ok {
				opts.PlaybookEnabled = v
				fields = append(fields, "isEnabled")
			}
			if v, ok, err := optionalBoolFlag("--trigger-enabled", triggerEnabled); err != nil {
				return err
			} else if ok {
				opts.TriggerEnabled = v
				fields = append(fields, "trigger.isEnabled")
			}
			if v, ok, err := optionalTriggerScalar(triggerType); err != nil {
				return err
			} else if ok {
				opts.Type = v
				fields = append(fields, "trigger.type")
			}
			if strings.TrimSpace(executionMode) != "" {
				opts.ExecutionMode = executionMode
				fields = append(fields, "trigger.executionMode")
			}
			if cmd.Flags().Changed("cron") {
				opts.CronSchedule = &cron
				fields = append(fields, "trigger.cronSchedule")
			}
			if strings.TrimSpace(conditions) != "" {
				if opts.Conditions, err = os.ReadFile(conditions); err != nil {
					return err
				}
				fields = append(fields, "trigger.conditions")
			}
			if strings.TrimSpace(reactionConditions) != "" {
				if opts.ReactionConditions, err = os.ReadFile(reactionConditions); err != nil {
					return err
				}
				fields = append(fields, "trigger.reactionConditions")
			}
			if len(fields) == 0 {
				return fmt.Errorf("no trigger fields requested")
			}
			patched, err := legacy.PatchPlaybookTrigger(legacy.Playbook(raw), opts)
			if err != nil {
				return err
			}
			if err := writePrettyJSONFile(out, patched); err != nil {
				return err
			}
			res := soarPlaybookTriggerSetResult{Output: out, Fields: fields}
			if jsonOut {
				return emitJSON(res)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "soar-playbook-trigger: wrote %s (%s)\n", out, strings.Join(fields, ", "))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "exported playbook JSON file (required)")
	f.StringVar(&out, "out", "", "output playbook JSON path (required)")
	f.StringVar(&enabled, "enabled", "", "set top-level playbook isEnabled: true or false")
	f.StringVar(&triggerEnabled, "trigger-enabled", "", "set trigger.isEnabled: true or false")
	f.StringVar(&triggerType, "type", "", "set trigger.type (JSON scalar, e.g. 1 or \"Case\")")
	f.StringVar(&executionMode, "execution-mode", "", "set trigger.executionMode")
	f.StringVar(&cron, "cron", "", "set trigger.cronSchedule; pass an empty value to clear")
	f.StringVar(&conditions, "conditions", "", "JSON file to set trigger.conditions")
	f.StringVar(&reactionConditions, "reaction-conditions", "", "JSON file to set trigger.reactionConditions")
	_ = cmd.MarkFlagRequired("file")
	_ = cmd.MarkFlagRequired("out")
	return markJSON(cmd)
}

func optionalBoolFlag(name, value string) (*bool, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil, false, fmt.Errorf("%s must be true or false", name)
	}
	return &parsed, true, nil
}

func optionalTriggerScalar(value string) (any, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false, nil
	}
	dec := json.NewDecoder(strings.NewReader(value))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return value, true, nil
	}
	switch out.(type) {
	case nil:
		return nil, false, fmt.Errorf("--type must be one JSON scalar")
	case map[string]any, []any:
		return nil, false, fmt.Errorf("--type must be one JSON scalar")
	}
	if trailing := strings.TrimSpace(value[int(dec.InputOffset()):]); trailing != "" {
		return nil, false, fmt.Errorf("--type must be one JSON scalar")
	}
	return out, true, nil
}
