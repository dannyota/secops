package cli

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
)

func newSOARIntegrationImportCmd() *cobra.Command {
	var (
		file        string
		staging     bool
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "import --file <zip>",
		Short: "MUTATING (guarded): import a custom integration from a packaged ZIP",
		Long: "Import a custom integration ZIP (produced by `soar ide package-integration`)\n" +
			"into SecOps SOAR. Creates the integration and all action/job definitions\n" +
			"it contains, with their Python scripts.\n\n" +
			"By default the integration is created in staging mode (--staging, the\n" +
			"safe default); pass --no-staging to create directly in production.\n" +
			"Promote a staged integration from the IDE (More options → Push to production).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bundle, err := readIntegrationZIP(file)
			if err != nil {
				return err
			}
			return importIntegration(bundle, staging, dryRun, yes)
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "packaged integration ZIP (required)")
	f.BoolVar(&staging, "staging", true, "create in staging mode (promote from IDE later)")
	guardRunFlags(cmd, &dryRun, &yes)
	_ = cmd.MarkFlagRequired("file")
	return markJSON(cmd)
}

type integrationBundle struct {
	Manifest importManifest
	Actions  []importComponentDef
	Jobs     []importComponentDef
}

type importManifest struct {
	Name        string           `json:"name"`
	DisplayName string           `json:"displayName"`
	Description string           `json:"description"`
	Version     string           `json:"version"`
	Components  []string         `json:"components"`
	Parameters  []map[string]any `json:"parameters"`
	GeneratedBy string           `json:"generatedBy"`
}

type importComponentDef struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Script      string `json:"script"`
	ScriptBody  string `json:"-"`
}

func readIntegrationZIP(path string) (*integrationBundle, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open ZIP: %w", err)
	}
	defer func() { _ = r.Close() }()

	files := make(map[string][]byte)
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f.Name, err)
		}
		files[f.Name] = data
	}

	manifestData, ok := files["manifest.json"]
	if !ok {
		return nil, fmt.Errorf("ZIP has no manifest.json")
	}
	var manifest importManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest.json: %w", err)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return nil, fmt.Errorf("manifest.json: name is required")
	}

	bundle := &integrationBundle{Manifest: manifest}
	for _, comp := range manifest.Components {
		kind, name, ok := strings.Cut(comp, ":")
		if !ok {
			return nil, fmt.Errorf("manifest.json: invalid component %q (expected kind:name)", comp)
		}
		def, err := resolveComponent(files, kind, name)
		if err != nil {
			return nil, err
		}
		switch kind {
		case "action":
			bundle.Actions = append(bundle.Actions, def)
		case "job":
			bundle.Jobs = append(bundle.Jobs, def)
		default:
			return nil, fmt.Errorf("manifest.json: unsupported component kind %q in %q", kind, comp)
		}
	}
	return bundle, nil
}

func resolveComponent(files map[string][]byte, kind, name string) (importComponentDef, error) {
	stem := integrationFileStem(name)
	var dir string
	switch kind {
	case "action":
		dir = "Actions"
	case "job":
		dir = "Jobs"
	default:
		return importComponentDef{}, fmt.Errorf("unsupported component kind %q", kind)
	}

	defPath := dir + "/" + stem + ".json"
	defData, ok := files[defPath]
	if !ok {
		return importComponentDef{}, fmt.Errorf("ZIP missing definition file %s for component %s:%s", defPath, kind, name)
	}
	var def importComponentDef
	if err := json.Unmarshal(defData, &def); err != nil {
		return importComponentDef{}, fmt.Errorf("parse %s: %w", defPath, err)
	}
	if def.DisplayName == "" {
		def.DisplayName = name
	}

	pyPath := dir + "/" + stem + ".py"
	if def.Script != "" {
		pyPath = def.Script
	}
	pyData, ok := files[pyPath]
	if !ok {
		return importComponentDef{}, fmt.Errorf("ZIP missing script %s for component %s:%s", pyPath, kind, name)
	}
	def.ScriptBody = string(pyData)
	return def, nil
}

func importIntegration(bundle *integrationBundle, staging, dryRun, yes bool) error {
	m := bundle.Manifest
	modeLabel := "staging"
	if !staging {
		modeLabel = "production"
	}

	action := fmt.Sprintf("import integration %q (%d actions, %d jobs) → %s",
		m.DisplayName, len(bundle.Actions), len(bundle.Jobs), modeLabel)

	dr, ay := soarGuard(action, dryRun, yes)
	fmt.Fprintf(os.Stdout, "Integration: %s\n", m.DisplayName)
	if m.Description != "" {
		fmt.Fprintf(os.Stdout, "Description: %s\n", m.Description)
	}
	fmt.Fprintf(os.Stdout, "Mode:        %s\n", modeLabel)
	for _, a := range bundle.Actions {
		fmt.Fprintf(os.Stdout, "  action: %s (%d bytes)\n", a.DisplayName, len(a.ScriptBody))
	}
	for _, j := range bundle.Jobs {
		fmt.Fprintf(os.Stdout, "  job:    %s (%d bytes)\n", j.DisplayName, len(j.ScriptBody))
	}
	fmt.Fprintln(os.Stdout)

	if dr {
		fmt.Fprintln(os.Stdout, "DRY RUN — no API calls made. Re-run with --yes to apply.")
		return nil
	}
	if !ay {
		fmt.Fprintln(os.Stdout, "Refusing to import without confirmation (pass --yes). Aborted.")
		return nil
	}

	c, err := newSOARClient()
	if err != nil {
		return err
	}
	ctx := baseContext()

	createBody, err := json.Marshal(map[string]any{
		"displayName": m.DisplayName,
		"parameters":  convertManifestParams(m.Parameters),
		"categories":  []string{},
		"staging":     staging,
		"type":        "RESPONSE",
	})
	if err != nil {
		return err
	}
	integration, err := c.CreateIntegration(ctx, createBody)
	if err != nil {
		return fmt.Errorf("create integration: %w", err)
	}
	integrationKey := integration.Identifier
	fmt.Fprintf(os.Stdout, "Created integration %q (identifier: %s)\n", integration.DisplayName, integrationKey)

	for _, a := range bundle.Actions {
		if err := importActionDef(ctx, c, integrationKey, a); err != nil {
			return fmt.Errorf("create action %q: %w", a.DisplayName, err)
		}
		fmt.Fprintf(os.Stdout, "  created action: %s\n", a.DisplayName)
	}
	for _, j := range bundle.Jobs {
		if err := importJobDef(ctx, c, integrationKey, j); err != nil {
			return fmt.Errorf("create job %q: %w", j.DisplayName, err)
		}
		fmt.Fprintf(os.Stdout, "  created job: %s\n", j.DisplayName)
	}

	fmt.Fprintf(os.Stdout, "\nDone. Integration %q imported (%s).\n", m.DisplayName, modeLabel)
	if staging {
		fmt.Fprintln(os.Stdout, "Promote to production from IDE: More options → Push to production.")
	}
	return nil
}

func importActionDef(ctx context.Context, c *soar.Client, integration string, def importComponentDef) error {
	tpl, err := c.FetchActionTemplate(ctx, integration, false)
	if err != nil {
		return fmt.Errorf("fetch action template: %w", err)
	}
	body, err := fillAuthoringTemplate(tpl, integration, def.DisplayName, def.ScriptBody, def.Description)
	if err != nil {
		return fmt.Errorf("fill template: %w", err)
	}
	_, err = c.CreateActionDef(ctx, integration, body)
	return err
}

// convertManifestParams maps scaffold manifest parameters (lowercase type
// names like "string") to the v1alpha IntegrationParamTypeEnum values the
// server expects (e.g. "STRING"). Unknown types pass through unchanged.
func convertManifestParams(params []map[string]any) []map[string]any {
	if len(params) == 0 {
		return []map[string]any{}
	}
	out := make([]map[string]any, len(params))
	for i, p := range params {
		cp := make(map[string]any, len(p))
		maps.Copy(cp, p)
		if t, ok := cp["type"].(string); ok {
			cp["type"] = paramTypeEnum(t)
		}
		out[i] = cp
	}
	return out
}

func paramTypeEnum(t string) string {
	switch strings.ToLower(t) {
	case "string":
		return "STRING"
	case "password":
		return "PASSWORD"
	case "boolean", "bool":
		return "BOOLEAN"
	case "integer", "int":
		return "INTEGER"
	case "ip":
		return "IP"
	case "email":
		return "EMAIL"
	case "content":
		return "CONTENT"
	case "domain":
		return "DOMAIN"
	case "url":
		return "URL"
	case "user":
		return "USER"
	default:
		return strings.ToUpper(t)
	}
}

func importJobDef(ctx context.Context, c *soar.Client, integration string, def importComponentDef) error {
	tpl, err := c.FetchJobTemplate(ctx, integration)
	if err != nil {
		return fmt.Errorf("fetch job template: %w", err)
	}
	body, err := fillAuthoringTemplate(tpl, integration, def.DisplayName, def.ScriptBody, def.Description)
	if err != nil {
		return fmt.Errorf("fill template: %w", err)
	}
	_, err = c.CreateJobDef(ctx, integration, body)
	return err
}
