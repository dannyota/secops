package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

type soarBuildPlaybookResult struct {
	Output           string `json:"output"`
	StepReplacements int    `json:"step_replacements"`
}

func newSOARBuildPlaybookCmd() *cobra.Command {
	var (
		basePath     string
		outPath      string
		name         string
		cron         string
		replacements []string
	)
	cmd := &cobra.Command{
		Use:   "build-playbook --base <playbook.json> --cron <expr> --out <playbook.json>",
		Short: "Offline: compose a scheduled playbook from exported JSON molds",
		Long: "Build a save-ready SOAR playbook JSON file without calling SOAR.\n" +
			"Start from a full exported base playbook, set trigger.cronSchedule,\n" +
			"and optionally replace named placeholder steps with exported, already\n" +
			"wired integration-action step molds.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			base, err := os.ReadFile(basePath)
			if err != nil {
				return err
			}
			stepRepls, err := loadPlaybookStepReplacements(replacements)
			if err != nil {
				return err
			}
			built, err := legacy.BuildPlaybookFromMolds(legacy.Playbook(base), legacy.PlaybookBuildOptions{
				Name:             name,
				CronSchedule:     cron,
				StepReplacements: stepRepls,
			})
			if err != nil {
				return err
			}
			if err := writePrettyJSONFile(outPath, built); err != nil {
				return err
			}
			res := soarBuildPlaybookResult{Output: outPath, StepReplacements: len(stepRepls)}
			if jsonOut {
				return emitJSON(res)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "soar-build-playbook: wrote %s\n", outPath)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&basePath, "base", "", "full exported base playbook JSON (required)")
	f.StringVar(&outPath, "out", "", "output playbook JSON path (required)")
	f.StringVar(&name, "name", "", "override playbook name")
	f.StringVar(&cron, "cron", "", "cron expression to set on trigger.cronSchedule (required)")
	f.StringArrayVar(&replacements, "replace-step", nil, "replace a base step with a mold: <step-name|id>=<step.json> (repeatable)")
	_ = cmd.MarkFlagRequired("base")
	_ = cmd.MarkFlagRequired("out")
	_ = cmd.MarkFlagRequired("cron")
	return cmd
}

func loadPlaybookStepReplacements(args []string) ([]legacy.PlaybookStepReplacement, error) {
	var out []legacy.PlaybookStepReplacement
	for _, arg := range args {
		match, path, err := parsePlaybookStepReplacementArg(arg)
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		out = append(out, legacy.PlaybookStepReplacement{
			Match: match,
			Mold:  json.RawMessage(raw),
		})
	}
	return out, nil
}

func parsePlaybookStepReplacementArg(arg string) (match, path string, err error) {
	left, right, ok := strings.Cut(arg, "=")
	if !ok {
		return "", "", fmt.Errorf("invalid --replace-step %q (want <step-name|id>=<step.json>)", arg)
	}
	match = strings.TrimSpace(left)
	path = strings.TrimSpace(right)
	if match == "" || path == "" {
		return "", "", fmt.Errorf("invalid --replace-step %q (both step match and file are required)", arg)
	}
	return match, path, nil
}

func writePrettyJSONFile(path string, raw []byte) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("empty output path")
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return fmt.Errorf("indent output JSON: %w", err)
	}
	buf.WriteByte('\n')
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
