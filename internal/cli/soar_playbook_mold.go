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
	cmd.AddCommand(newSOARPlaybookMoldExtractCmd())
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
			if err := writePrettyJSONFile(out, mold, 0o644); err != nil {
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
