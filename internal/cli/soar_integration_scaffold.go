package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

type integrationScaffoldResult struct {
	Output string   `json:"output"`
	Files  []string `json:"files"`
}

type integrationScaffoldManifest struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Components  []string `json:"components"`
	GeneratedBy string   `json:"generatedBy"`
}

type integrationScaffoldActionDef struct {
	Name             string           `json:"name"`
	DisplayName      string           `json:"displayName"`
	Description      string           `json:"description"`
	Script           string           `json:"script"`
	Parameters       []map[string]any `json:"parameters"`
	HasJSONResult    bool             `json:"hasJsonResult"`
	ScriptResultName string           `json:"scriptResultName"`
}

type integrationScaffoldJobDef struct {
	Name                 string           `json:"name"`
	DisplayName          string           `json:"displayName"`
	Description          string           `json:"description"`
	Script               string           `json:"script"`
	Parameters           []map[string]any `json:"parameters"`
	IsEnabled            bool             `json:"isEnabled"`
	RunIntervalInSeconds int              `json:"runIntervalInSeconds"`
}

func newSOARIntegrationScaffoldCmd() *cobra.Command {
	var (
		name    string
		out     string
		actions []string
		jobs    []string
		force   bool
	)
	cmd := &cobra.Command{
		Use:   "scaffold --name <integration> --out <dir> [--action <name>] [--job <name>]",
		Short: "Offline: scaffold a Python-backed custom integration directory",
		Long: "Create a local custom integration scaffold with Python action/job\n" +
			"templates and JSON definition placeholders. This command is offline:\n" +
			"package it with `soar package-integration`, then let SecOps validate it\n" +
			"during import and playbook debug/run.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := scaffoldSOARIntegration(name, out, actions, jobs, force)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(res)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "soar-integration-scaffold: wrote %s (%d file(s))\n", res.Output, len(res.Files))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "custom integration name (required)")
	f.StringVar(&out, "out", "", "output directory (default: ./<name>)")
	f.StringArrayVar(&actions, "action", nil, "custom action name to scaffold (repeatable)")
	f.StringArrayVar(&jobs, "job", nil, "custom job name to scaffold (repeatable)")
	f.BoolVar(&force, "force", false, "overwrite existing scaffold files")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func scaffoldSOARIntegration(name, out string, actions, jobs []string, force bool) (integrationScaffoldResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return integrationScaffoldResult{}, fmt.Errorf("--name is required")
	}
	actions = trimNonEmpty(actions)
	jobs = trimNonEmpty(jobs)
	if len(actions) == 0 && len(jobs) == 0 {
		return integrationScaffoldResult{}, fmt.Errorf("at least one --action or --job is required")
	}
	if strings.TrimSpace(out) == "" {
		out = integrationFileStem(name)
	}
	root, err := filepath.Abs(out)
	if err != nil {
		return integrationScaffoldResult{}, err
	}

	var files []string
	components := make([]string, 0, len(actions)+len(jobs))
	for _, action := range actions {
		stem := integrationFileStem(action)
		pyRel := filepath.ToSlash(filepath.Join("Actions", stem+".py"))
		defRel := filepath.ToSlash(filepath.Join("Actions", stem+".json"))
		components = append(components, "action:"+action)
		if err := writeScaffoldJSON(filepath.Join(root, defRel), integrationScaffoldActionDef{
			Name:             action,
			DisplayName:      action,
			Description:      "Generated action scaffold. Edit and validate in SecOps before using in playbooks.",
			Script:           pyRel,
			Parameters:       []map[string]any{},
			HasJSONResult:    true,
			ScriptResultName: "result",
		}, force); err != nil {
			return integrationScaffoldResult{}, err
		}
		files = append(files, defRel)
		if err := writeScaffoldFile(filepath.Join(root, pyRel), []byte(actionPythonTemplate()), force); err != nil {
			return integrationScaffoldResult{}, err
		}
		files = append(files, pyRel)
	}
	for _, job := range jobs {
		stem := integrationFileStem(job)
		pyRel := filepath.ToSlash(filepath.Join("Jobs", stem+".py"))
		defRel := filepath.ToSlash(filepath.Join("Jobs", stem+".json"))
		components = append(components, "job:"+job)
		if err := writeScaffoldJSON(filepath.Join(root, defRel), integrationScaffoldJobDef{
			Name:                 job,
			DisplayName:          job,
			Description:          "Generated job scaffold. Edit and validate in SecOps before enabling.",
			Script:               pyRel,
			Parameters:           []map[string]any{},
			IsEnabled:            false,
			RunIntervalInSeconds: 3600,
		}, force); err != nil {
			return integrationScaffoldResult{}, err
		}
		files = append(files, defRel)
		if err := writeScaffoldFile(filepath.Join(root, pyRel), []byte(jobPythonTemplate()), force); err != nil {
			return integrationScaffoldResult{}, err
		}
		files = append(files, pyRel)
	}
	if err := writeScaffoldJSON(filepath.Join(root, "manifest.json"), integrationScaffoldManifest{
		Name:        name,
		DisplayName: name,
		Description: "Generated custom integration scaffold. Package and import through SecOps for validation.",
		Version:     "0.1.0",
		Components:  components,
		GeneratedBy: "secopsctl",
	}, force); err != nil {
		return integrationScaffoldResult{}, err
	}
	files = append([]string{"manifest.json"}, files...)
	return integrationScaffoldResult{Output: root, Files: files}, nil
}

func writeScaffoldJSON(path string, v any, force bool) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeScaffoldFile(path, raw, force)
}

func writeScaffoldFile(path string, raw []byte, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func integrationFileStem(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	stem := strings.Trim(b.String(), "_")
	if stem == "" {
		return "integration"
	}
	return stem
}

func actionPythonTemplate() string {
	return `from SiemplifyAction import SiemplifyAction
from SiemplifyUtils import output_handler


@output_handler
def main():
    siemplify = SiemplifyAction()
    result = {"ok": True}
    siemplify.result.add_result_json(result)
    siemplify.end("Action completed", "true")


if __name__ == "__main__":
    main()
`
}

func jobPythonTemplate() string {
	return `from SiemplifyJob import SiemplifyJob
from SiemplifyUtils import output_handler


@output_handler
def main():
    siemplify = SiemplifyJob()
    siemplify.end("Job completed", "true")


if __name__ == "__main__":
    main()
`
}
