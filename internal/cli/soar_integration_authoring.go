package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
)

// `soar integration action`/`job` — Python definition authoring, the IDE's
// create flow as an API loop: `template` fetches the skeleton (read-only),
// `create` posts a filled definition (guarded), `delete` removes one by its
// numeric id (guarded). Definitions land DISABLED-by-default unless the body
// says otherwise; ids appear in `soar playbook components actions`.

func newSOARIntegrationActionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "action <verb>",
		Short: "Author Python ACTION definitions inside an integration (template/create/update/delete)",
	}
	cmd.AddCommand(
		newAuthoringTemplateCmd("action", true),
		newAuthoringCreateCmd("action"),
		newAuthoringUpdateCmd("action"),
		newAuthoringDeleteCmd("action"),
	)
	return cmd
}

func newSOARIntegrationJobCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "job-def <verb>",
		Short: "Author Python JOB definitions inside an integration (template/create/update/delete)",
	}
	cmd.AddCommand(
		newAuthoringTemplateCmd("job", false),
		newAuthoringCreateCmd("job"),
		newAuthoringUpdateCmd("job"),
		newAuthoringDeleteCmd("job"),
	)
	return cmd
}

func newAuthoringUpdateCmd(kind string) *cobra.Command {
	var (
		integration string
		id          string
		script      string
		description string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "update --integration <key> --id N (--script <f.py> | --description <s>)",
		Short: "MUTATING (guarded): patch fields of an existing custom " + kind + " definition",
		Long: "Update an existing custom Python " + kind + " definition by its numeric id\n" +
			"(from `soar playbook components " + collectionFor(kind) + "` or a prior create).\n" +
			"It is a sparse PATCH — pass only what changes (`--script` swaps the Python\n" +
			"body, `--description` the text) and only those fields are touched. Create is\n" +
			"a separate verb; a save here never creates a duplicate.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fields := map[string]any{}
			if cmd.Flags().Changed("script") {
				src, err := os.ReadFile(script)
				if err != nil {
					return err
				}
				fields["script"] = string(src)
			}
			if cmd.Flags().Changed("description") {
				fields["description"] = description
			}
			if len(fields) == 0 {
				return fmt.Errorf("nothing to update — pass --script and/or --description")
			}
			mask := make([]string, 0, len(fields))
			for k := range fields {
				mask = append(mask, k)
			}
			sort.Strings(mask)
			body, err := json.Marshal(fields)
			if err != nil {
				return err
			}
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			action := fmt.Sprintf("update %s definition %s in integration %s (fields: %s)", kind, id, integration, strings.Join(mask, ","))
			dr, ay := soarGuard(action, dryRun, yes)
			if dr || !ay {
				fmt.Fprintf(os.Stdout, "DRY RUN: would %s.\n", action)
				return nil
			}
			var out json.RawMessage
			if kind == "action" {
				out, err = c.UpdateActionDef(baseContext(), integration, id, body, mask...)
			} else {
				out, err = c.UpdateJobDef(baseContext(), integration, id, body, mask...)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "updated %s definition %s (%s).\n", kind, id, strings.Join(mask, ","))
			if jsonOut {
				return writeRawJSON(os.Stdout, out)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration key the definition belongs to (required)")
	f.StringVar(&id, "id", "", "the definition's numeric id (required; from 'components "+collectionFor(kind)+"')")
	f.StringVar(&script, "script", "", "Python source file to set as the new definition body")
	f.StringVar(&description, "description", "", "new definition description")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("id")
	guardRunFlags(cmd, &dryRun, &yes)
	return markJSON(cmd)
}

// collectionFor maps the authoring kind to its `components` catalog subcommand.
func collectionFor(kind string) string {
	if kind == "action" {
		return "actions"
	}
	return "jobs"
}

func newAuthoringTemplateCmd(kind string, hasAsync bool) *cobra.Command {
	var (
		integration string
		async       bool
	)
	cmd := &cobra.Command{
		Use:   "template --integration <key>",
		Short: "Read-only: fetch the new-" + kind + " definition skeleton (Python scaffold included)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			tpl, err := fetchAuthoringTemplate(c, kind, integration, async)
			if err != nil {
				return err
			}
			return writeRawJSON(os.Stdout, tpl)
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration key the definition belongs to (required)")
	if hasAsync {
		f.BoolVar(&async, "async", false, "fetch the asynchronous-action variant of the skeleton")
	}
	_ = cmd.MarkFlagRequired("integration")
	// Output is always raw JSON (the definition skeleton), like `rules alerts`.
	return markJSON(cmd)
}

func fetchAuthoringTemplate(c *soar.Client, kind, integration string, async bool) (json.RawMessage, error) {
	if kind == "action" {
		return c.FetchActionTemplate(baseContext(), integration, async)
	}
	return c.FetchJobTemplate(baseContext(), integration)
}

func newAuthoringCreateCmd(kind string) *cobra.Command {
	var (
		integration string
		file        string
		name        string
		script      string
		description string
		async       bool
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "create --integration <key> (--file <def.json> | --name <display> --script <file.py>)",
		Short: "MUTATING (guarded): create a custom " + kind + " definition",
		Long: "Create a custom Python " + kind + " definition. Either supply the complete\n" +
			"definition JSON (--file, e.g. an edited `template` output), or let the\n" +
			"command fetch the template and fill --name/--script/--description into it.\n" +
			"The definition is created with the template's enabled state (disabled for a\n" +
			"fresh template); enable it from the IDE once reviewed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			useFile := strings.TrimSpace(file) != ""
			if useFile == (strings.TrimSpace(name) != "" || strings.TrimSpace(script) != "") {
				return fmt.Errorf("pass --file, or --name with --script")
			}
			if !useFile && (strings.TrimSpace(name) == "" || strings.TrimSpace(script) == "") {
				return fmt.Errorf("--name and --script go together")
			}
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			var body json.RawMessage
			if useFile {
				b, err := os.ReadFile(file)
				if err != nil {
					return err
				}
				if !json.Valid(b) {
					return fmt.Errorf("%s is not valid JSON", file)
				}
				body = b
			} else {
				src, err := os.ReadFile(script)
				if err != nil {
					return err
				}
				tpl, err := fetchAuthoringTemplate(c, kind, integration, async)
				if err != nil {
					return err
				}
				if body, err = fillAuthoringTemplate(tpl, integration, name, string(src), description); err != nil {
					return err
				}
			}
			action := fmt.Sprintf("create %s definition %q in integration %s (%d-byte body)", kind, displayNameOf(body, name), integration, len(body))
			dr, ay := soarGuard(action, dryRun, yes)
			if dr || !ay {
				fmt.Fprintf(os.Stdout, "DRY RUN: would %s.\n", action)
				return nil
			}
			out, err := createAuthoringDef(c, kind, integration, body)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "created. Definition id: %s (see `soar playbook components actions`)\n", definitionIDOf(out))
			if jsonOut {
				return writeRawJSON(os.Stdout, out)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration key the definition belongs to (required)")
	f.StringVar(&file, "file", "", "complete definition JSON (an edited 'template' output)")
	f.StringVar(&name, "name", "", "display name for the new definition")
	f.StringVar(&script, "script", "", "Python source file for the definition body")
	f.StringVar(&description, "description", "", "definition description")
	if kind == "action" {
		f.BoolVar(&async, "async", false, "author the asynchronous-action variant")
	}
	_ = cmd.MarkFlagRequired("integration")
	guardRunFlags(cmd, &dryRun, &yes)
	return markJSON(cmd)
}

func createAuthoringDef(c *soar.Client, kind, integration string, body json.RawMessage) (json.RawMessage, error) {
	if kind == "action" {
		return c.CreateActionDef(baseContext(), integration, body)
	}
	return c.CreateJobDef(baseContext(), integration, body)
}

func newAuthoringDeleteCmd(kind string) *cobra.Command {
	var (
		integration string
		id          string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "delete --integration <key> --id N",
		Short: "MUTATING (guarded): delete a custom " + kind + " definition by numeric id",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			action := fmt.Sprintf("delete %s definition %s from integration %s", kind, id, integration)
			dr, ay := soarGuard(action, dryRun, yes)
			if dr || !ay {
				fmt.Fprintf(os.Stdout, "DRY RUN: would %s — irreversible; playbooks referencing it break (check `components usage` first).\n", action)
				return nil
			}
			if kind == "action" {
				err = c.DeleteActionDef(baseContext(), integration, id)
			} else {
				err = c.DeleteJobDef(baseContext(), integration, id)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "deleted %s definition %s.\n", kind, id)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&integration, "integration", "", "integration key (required)")
	f.StringVar(&id, "id", "", "the definition's numeric id (required; from 'components actions' or the create output)")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("id")
	guardRunFlags(cmd, &dryRun, &yes)
	return cmd
}

// fillAuthoringTemplate overlays the caller's values onto the fetched
// template skeleton via a RawMessage map, so every numeric field the server
// sent survives byte-exact. name "" marks the body as a CREATE.
func fillAuthoringTemplate(tpl json.RawMessage, integration, displayName, script, description string) (json.RawMessage, error) {
	if len(tpl) == 0 {
		return nil, fmt.Errorf("the server returned no template — supply the full definition with --file instead")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(tpl, &m); err != nil {
		return nil, fmt.Errorf("decode template: %w", err)
	}
	set := func(key string, v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		m[key] = b
		return nil
	}
	for key, v := range map[string]any{
		"name":        "",
		"integration": integration,
		"displayName": displayName,
		"script":      script,
		"custom":      true,
	} {
		if err := set(key, v); err != nil {
			return nil, err
		}
	}
	if description != "" {
		if err := set("description", description); err != nil {
			return nil, err
		}
	}
	return json.Marshal(m)
}

// displayNameOf extracts displayName from a definition body for the guard
// banner, falling back to the flag value.
func displayNameOf(body json.RawMessage, fallback string) string {
	var probe struct {
		DisplayName string `json:"displayName"`
	}
	if json.Unmarshal(body, &probe) == nil && probe.DisplayName != "" {
		return probe.DisplayName
	}
	return fallback
}

// definitionIDOf extracts the created definition's numeric id (or resource
// name tail) from the create response.
func definitionIDOf(out json.RawMessage) string {
	var probe struct {
		ID   json.Number `json:"id"`
		Name string      `json:"name"`
	}
	if json.Unmarshal(out, &probe) == nil {
		if probe.ID.String() != "" {
			return probe.ID.String()
		}
		if i := strings.LastIndex(probe.Name, "/"); i >= 0 {
			return probe.Name[i+1:]
		}
	}
	return "(see response with --json)"
}
