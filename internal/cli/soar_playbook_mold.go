package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

type soarPlaybookMoldExtractResult struct {
	Output string `json:"output"`
	Step   string `json:"step"`
}

func newSOARPlaybookMoldCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mold",
		Short: "Create reusable playbook step molds from SecOps-exported JSON",
		Long: "Create reusable playbook step molds from exported SecOps playbook JSON.\n" +
			"Molds are offline authoring artifacts for `soar build-playbook`; SecOps\n" +
			"still validates the final playbook on save/debug/run.",
	}
	cmd.AddCommand(newSOARPlaybookMoldExtractCmd(), newSOARPlaybookMoldApplyCmd())
	return cmd
}

func newSOARPlaybookMoldExtractCmd() *cobra.Command {
	var (
		file string
		step string
		out  string
	)
	cmd := &cobra.Command{
		Use:   "extract --file <playbook.json> --step <name|id> --out <step.json>",
		Short: "Extract one action step as a reusable mold",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			mold, err := legacy.ExtractActionStepMold(legacy.Playbook(raw), step)
			if err != nil {
				return err
			}
			if err := writePrettyJSONFile(out, mold); err != nil {
				return err
			}
			res := soarPlaybookMoldExtractResult{Output: out, Step: step}
			if jsonOut {
				return emitJSON(res)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "soar-playbook-mold: wrote %s\n", out)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "exported playbook JSON file (required)")
	f.StringVar(&step, "step", "", "step name, identifier, or originalStepIdentifier to extract (required)")
	f.StringVar(&out, "out", "", "output step mold JSON path (required)")
	_ = cmd.MarkFlagRequired("file")
	_ = cmd.MarkFlagRequired("step")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

func newSOARPlaybookMoldApplyCmd() *cobra.Command {
	var (
		file         string
		out          string
		replacements []string
	)
	cmd := &cobra.Command{
		Use:   "apply --file <playbook.json> --replace-step <step=step.json> --out <playbook.json>",
		Short: "Apply action step molds to an exported playbook",
		Long: "Apply exported action step molds to placeholder steps in an exported\n" +
			"playbook JSON file. The output is still an offline artifact; validate\n" +
			"and save it through the normal guarded SecOps playbook path.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			stepRepls, err := loadPlaybookStepReplacements(replacements)
			if err != nil {
				return err
			}
			if len(stepRepls) == 0 {
				return fmt.Errorf("--replace-step is required")
			}
			built, err := legacy.BuildPlaybookFromMolds(legacy.Playbook(raw), legacy.PlaybookBuildOptions{
				StepReplacements: stepRepls,
			})
			if err != nil {
				return err
			}
			if err := writePrettyJSONFile(out, built); err != nil {
				return err
			}
			res := struct {
				Output           string `json:"output"`
				StepReplacements int    `json:"step_replacements"`
			}{Output: out, StepReplacements: len(stepRepls)}
			if jsonOut {
				return emitJSON(res)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "soar-playbook-mold: wrote %s\n", out)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "exported playbook JSON file (required)")
	f.StringVar(&out, "out", "", "output playbook JSON path (required)")
	f.StringArrayVar(&replacements, "replace-step", nil, "replace a base step with a mold: <step-name|id>=<step.json> (repeatable)")
	_ = cmd.MarkFlagRequired("file")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}
