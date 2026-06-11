package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

// `soar playbook step insert` — offline graph authoring: splice a brand-new
// action step (from an exported mold) into a playbook definition after an
// existing anchor step, with fresh graph identity and rewired relations. The
// result still flows through `soar playbook validate` and the guarded save —
// SOAR remains the final validator.

type soarStepInsertResult struct {
	Out          string `json:"out"`
	Identifier   string `json:"identifier"`
	InstanceName string `json:"instance_name"`
	After        string `json:"after"`
	Branch       string `json:"branch,omitempty"`
}

func newSOARPlaybookStepInsertCmd() *cobra.Command {
	var (
		file, mold, after string
		branch, instance  string
		out               string
	)
	cmd := &cobra.Command{
		Use:   "insert --file <playbook.json> --mold <step.json> --after <step> --out <playbook.json>",
		Short: "Offline: splice a NEW action step into a playbook definition after an anchor step",
		Long: "Add a brand-new action step to a local playbook definition: the mold (an\n" +
			"exported step — `soar playbook mold extract`) supplies the wired action\n" +
			"body, the insert mints fresh graph identity (identifier, designer-style\n" +
			"instance name) and rewires the anchor's outgoing relation through the new\n" +
			"step. --after matches a step name or identifier; when the anchor is a\n" +
			"condition with several branches, pick one with --branch (the relation's\n" +
			"condition value, e.g. 1). No API call — review the result with\n" +
			"`soar playbook validate` and save it through the guarded loop.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			base, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			moldBody, err := os.ReadFile(mold)
			if err != nil {
				return err
			}
			built, err := legacy.InsertActionStep(base, legacy.PlaybookStepInsertOptions{
				Mold:         moldBody,
				After:        after,
				Branch:       branch,
				InstanceName: instance,
			})
			if err != nil {
				return err
			}
			if err := writePrettyJSONFile(out, built); err != nil {
				return err
			}
			res := soarStepInsertResult{Out: out, After: after, Branch: branch}
			res.Identifier, res.InstanceName = insertedStepIdentity(built)
			if jsonOut {
				return emitJSON(res)
			}
			fmt.Fprintf(os.Stdout, "inserted %q (identifier %s) after %q%s -> %s\nreview: secopsctl soar playbook validate --file %s\n",
				res.InstanceName, res.Identifier, after, branchSuffix(branch), out, out)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "playbook definition JSON to edit (required)")
	f.StringVar(&mold, "mold", "", "exported action-step mold JSON (required; see 'mold extract')")
	f.StringVar(&after, "after", "", "anchor step name/identifier the new step follows (required)")
	f.StringVar(&branch, "branch", "", "anchor branch to splice when it has several (the relation's condition value)")
	f.StringVar(&instance, "instance-name", "", "override the generated unique instance name")
	f.StringVar(&out, "out", "", "output playbook JSON path (required)")
	for _, req := range []string{"file", "mold", "after", "out"} {
		_ = cmd.MarkFlagRequired(req)
	}
	return markJSON(cmd)
}

// insertedStepIdentity reads the LAST step's identity from the built playbook
// (InsertActionStep appends the new step).
func insertedStepIdentity(built json.RawMessage) (identifier, instanceName string) {
	var probe struct {
		Steps []struct {
			Identifier   string `json:"identifier"`
			InstanceName string `json:"instanceName"`
		} `json:"steps"`
	}
	if json.Unmarshal(built, &probe) == nil && len(probe.Steps) > 0 {
		last := probe.Steps[len(probe.Steps)-1]
		return last.Identifier, last.InstanceName
	}
	return "", ""
}

func branchSuffix(branch string) string {
	if strings.TrimSpace(branch) == "" {
		return ""
	}
	return fmt.Sprintf(" (branch %s)", branch)
}
