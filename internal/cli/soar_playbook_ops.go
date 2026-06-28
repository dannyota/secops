package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
	"danny.vn/secops/soar/legacy"
)

// Playbook lifecycle operations (Wave 55): version history + rollback (the
// natural undo for the new version every save/deploy mints), cross-case run
// statistics, and typed export/import (cross-tenant promotion / offline
// backup — the producer of the "exported playbook JSON" the mold and
// build-playbook commands consume).

// resolvePlaybookSelector maps the shared --name/--identifier pair to a
// workflow identifier (uuid), resolving a name via the live playbook list.
func resolvePlaybookSelector(ctx context.Context, lc *legacy.Client, name, identifier string) (string, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier != "" {
		return identifier, nil
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("pass --name or --identifier (list playbooks with `playbooks list`)")
	}
	return resolvePlaybookDefinition(ctx, lc, name)
}

func newSOARPlaybookVersionsCmd() *cobra.Command {
	var name, identifier string
	cmd := &cobra.Command{
		Use:   "versions (--name <playbook> | --identifier <uuid>)",
		Short: "Read-only: a playbook's saved version history (each save/deploy mints one)",
		Long: "List a playbook's version log — every save (including `deploy` toggles and\n" +
			"`push playbook`) mints a new version. The identifier shown per entry is what\n" +
			"`playbooks restore` rolls back to.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			id, err := resolvePlaybookSelector(ctx, lc, name, identifier)
			if err != nil {
				return err
			}
			raw, err := lc.GetWorkflowVersionLogs(ctx, map[string]any{"workFlowIdentifier": id})
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			return emitPlaybookVersions(raw)
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "playbook name (resolved via the live playbook list)")
	f.StringVar(&identifier, "identifier", "", "playbook definition identifier (uuid)")
	return markJSON(cmd)
}

// playbookVersionView is the tolerant per-entry decode of the version log.
type playbookVersionView struct {
	Identifier     string `json:"identifier"`
	Comment        string `json:"comment"`
	Creator        string `json:"creator"`
	Version        int    `json:"version"`
	CreationTimeMs int64  `json:"creationTimeUnixTimeInMs"`
}

func emitPlaybookVersions(raw json.RawMessage) error {
	var entries []playbookVersionView
	if err := json.Unmarshal(raw, &entries); err != nil {
		// Some revisions wrap the list. A present-but-empty wrap key is a valid
		// empty result ("no versions."), NOT an unknown shape — only a body with
		// no recognizable list key falls back to raw.
		var wrap struct {
			Items       *[]playbookVersionView `json:"items"`
			ObjectsList *[]playbookVersionView `json:"objectsList"`
		}
		switch err := json.Unmarshal(raw, &wrap); {
		case err == nil && wrap.Items != nil:
			entries = *wrap.Items
		case err == nil && wrap.ObjectsList != nil:
			entries = *wrap.ObjectsList
		default:
			return writeRawJSON(os.Stdout, raw)
		}
	}
	if len(entries) == 0 {
		fmt.Fprintln(os.Stdout, "no versions.")
		return nil
	}
	fmt.Fprintf(os.Stdout, "%-38s %-8s %-17s %-20s %s\n", "VERSION IDENTIFIER", "VERSION", "SAVED", "BY", "COMMENT")
	for _, e := range entries {
		fmt.Fprintf(os.Stdout, "%-38s %-8d %-17s %-20s %s\n",
			orDash(e.Identifier), e.Version, msToUTC(e.CreationTimeMs), truncate(orDash(e.Creator), 19), truncate(e.Comment, 50))
	}
	fmt.Fprintf(os.Stdout, "\n%d version(s). Roll back with: playbooks restore --version <identifier>\n", len(entries))
	return nil
}

func newSOARPlaybookRestoreCmd() *cobra.Command {
	var (
		version, comment string
		override         bool
		dryRun, yes      bool
	)
	cmd := &cobra.Command{
		Use:   "restore --version <identifier> [--comment <s>] [--override]",
		Short: "GUARDED: restore a playbook to a prior saved version",
		Long: "Roll a playbook back to a version from `playbooks versions` — the undo\n" +
			"for a bad save or deploy. The restore itself mints a new version (nothing is\n" +
			"lost); --override replaces the current definition outright.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := map[string]any{
				"restoreWorkflowIdentifier": version,
				"restoreComment":            comment,
				"isOverride":                override,
			}
			return caseAction(fmt.Sprintf("restore playbook version %s", version), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.RestoreWorkflowDefinition(ctx, body)
				})
		},
	}
	f := cmd.Flags()
	f.StringVar(&version, "version", "", "version identifier from 'playbooks versions' (required)")
	f.StringVar(&comment, "comment", "", "restore comment (why the rollback)")
	f.BoolVar(&override, "override", false, "replace the current definition outright")
	guardRunFlags(cmd, &dryRun, &yes)
	_ = cmd.MarkFlagRequired("version")
	return markJSON(cmd)
}

func newSOARPlaybookStatsCmd() *cobra.Command {
	var (
		name, identifier string
		hours            int
	)
	cmd := &cobra.Command{
		Use:   "stats (--name <playbook> | --identifier <uuid>) [--hours N]",
		Short: "Read-only: a playbook's run statistics across all cases over a window",
		Long: "Aggregate run statistics for one playbook across every case it ran on —\n" +
			"the automation-health view (`summary` inspects a single run). Prefers the\n" +
			"modern v1alpha bridge and falls back to the legacy API.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			id, err := resolvePlaybookSelector(ctx, lc, name, identifier)
			if err != nil {
				return err
			}
			end := time.Now().UTC()
			body := map[string]any{
				"fromUnixTimeMs":             end.Add(-time.Duration(hours) * time.Hour).UnixMilli(),
				"toUnixTimeMs":               end.UnixMilli(),
				"originalWorkflowIdentifier": id,
			}
			render := func(raw json.RawMessage) error {
				if jsonOut {
					return writeRawJSON(os.Stdout, raw)
				}
				return emitPlaybookStats(raw, hours)
			}
			return preferModern("playbooks stats",
				func() error {
					raw, merr := lc.GetPlaybookStats(ctx, body)
					if merr != nil {
						return merr
					}
					return render(raw)
				},
				func() error {
					raw, lerr := lc.PlaybookXGetStatsMap(ctx, body)
					if lerr != nil {
						return lerr
					}
					return render(raw)
				},
			)
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "playbook name (resolved via the live playbook list)")
	f.StringVar(&identifier, "identifier", "", "playbook definition identifier (uuid)")
	f.IntVar(&hours, "hours", 7*24, "look-back window in hours (default 7 days)")
	return markJSON(cmd)
}

// emitPlaybookStats renders the stats map compactly for the human path
// (per-step and per-flow entry counts); the full payload rides --json.
func emitPlaybookStats(raw json.RawMessage, hours int) error {
	var probe struct {
		Steps map[string]json.RawMessage `json:"steps"`
		Flows map[string]json.RawMessage `json:"flows"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return writeRawJSON(os.Stdout, raw)
	}
	fmt.Fprintf(os.Stdout, "Run statistics over the last %dh: %d step entr(ies), %d flow entr(ies).\n",
		hours, len(probe.Steps), len(probe.Flows))
	if len(probe.Steps) == 0 && len(probe.Flows) == 0 {
		fmt.Fprintln(os.Stdout, "No recorded runs in the window.")
		return nil
	}
	fmt.Fprintln(os.Stdout, "Full per-step/per-flow payloads with --json.")
	return nil
}

func newSOARPlaybookExportCmd() *cobra.Command {
	var (
		name, identifier, out string
		asZip                 bool
	)
	cmd := &cobra.Command{
		Use:   "export (--name <playbook> | --identifier <uuid>) [--out <file>] [--zip]",
		Short: "Read-only: export a playbook definition (save-compatible JSON, or a zip bundle)",
		Long: "Export one playbook. Default: the full definition as save-compatible JSON —\n" +
			"the same shape `pull playbooks` writes, so the export → edit → `push playbook`\n" +
			"loop round-trips and the file is the input `playbooks mold` /\n" +
			"`soar build-playbook` consume. --zip exports the platform bundle instead\n" +
			"(the format `playbooks import` takes — cross-tenant promotion / offline\n" +
			"backup). Writes to --out, or stdout for JSON.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			id, err := resolvePlaybookSelector(ctx, lc, name, identifier)
			if err != nil {
				return err
			}
			if !asZip {
				// GetPlaybook returns the camelCase, string-enum definition — the
				// SAME shape `pull playbooks` writes and `push playbook` accepts — so
				// export → edit → push round-trips. (The legacy
				// ExportWorkflowWithBlocks bundle is PascalCase/int-enum and not
				// save-compatible; the platform bundle is `--zip`.)
				raw, err := lc.GetPlaybook(ctx, id)
				if err != nil {
					return err
				}
				if out == "" {
					return writeRawJSON(os.Stdout, raw)
				}
				if err := os.WriteFile(out, raw, 0o600); err != nil {
					return err
				}
				fmt.Fprintf(os.Stdout, "Exported playbook JSON -> %s\n", out)
				return nil
			}
			if out == "" {
				return fmt.Errorf("--zip needs --out <file.zip> (binary bundle)")
			}
			raw, err := lc.ExportPlaybookDefinitions(ctx, map[string]any{"identifiers": []string{id}})
			if err != nil {
				return err
			}
			data, err := decodeExportBundle(raw)
			if err != nil {
				return err
			}
			// A bundle that is not a zip must never be archived as one — a
			// "successful" export of a JSON error envelope would only surface
			// when the backup is needed.
			if !bytes.HasPrefix(data, []byte("PK")) {
				return fmt.Errorf("export did not return a zip bundle (got %s…) — inspect with --json via `soar legacy call`", truncate(string(data), 60))
			}
			if err := os.WriteFile(out, data, 0o600); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Exported playbook bundle -> %s (%d bytes)\n", out, len(data))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "playbook name (resolved via the live playbook list)")
	f.StringVar(&identifier, "identifier", "", "playbook definition identifier (uuid)")
	f.StringVar(&out, "out", "", "output file (default: JSON to stdout)")
	f.BoolVar(&asZip, "zip", false, "export the platform zip bundle (for 'import') instead of JSON")
	// Default mode writes the playbook definition as JSON to stdout.
	return markJSON(cmd)
}

// decodeExportBundle extracts the bundle bytes from an ExportDefinitions
// response — an ApiFile {fileName, blob(base64)} envelope, or raw bytes. An
// ApiFile envelope with an EMPTY blob is an error, not a bundle: silently
// writing the envelope would fake a successful backup.
func decodeExportBundle(raw json.RawMessage) ([]byte, error) {
	var file struct {
		FileName string `json:"fileName"`
		Blob     string `json:"blob"`
	}
	if err := json.Unmarshal(raw, &file); err == nil {
		switch {
		case file.Blob != "":
			data, err := base64.StdEncoding.DecodeString(file.Blob)
			if err != nil {
				return nil, fmt.Errorf("decode export blob: %w", err)
			}
			return data, nil
		case file.FileName != "":
			return nil, fmt.Errorf("export returned an empty bundle (fileName %q, no blob)", file.FileName)
		}
	}
	return raw, nil
}

func newSOARPlaybookImportCmd() *cobra.Command {
	var (
		file        string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "import --file <bundle.zip>",
		Short: "GUARDED: import a playbook bundle (the zip `export --zip` produces)",
		Long: "Import playbook definitions from an exported bundle — cross-tenant promotion\n" +
			"(stage → prod) or restore from an offline backup. The bundle is sent as\n" +
			"base64; SOAR validates and creates/updates the definitions it carries.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			body := map[string]any{
				"fileName": filepath.Base(file),
				"blob":     base64.StdEncoding.EncodeToString(data),
			}
			preview := map[string]any{"fileName": filepath.Base(file), "bytes": len(data)}
			return caseAction(fmt.Sprintf("import playbook bundle %s", filepath.Base(file)), preview, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.ImportPlaybookDefinitions(ctx, body)
				})
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "exported playbook bundle (required)")
	guardRunFlags(cmd, &dryRun, &yes)
	_ = cmd.MarkFlagRequired("file")
	return markJSON(cmd)
}

func newSOARPlaybookTriggerTagsCmd() *cobra.Command {
	var grep string
	cmd := &cobra.Command{
		Use:   "tags [--grep <s>]",
		Short: "Read-only: list the live trigger tag values playbook triggers can reference",
		Long: "The tag vocabulary available to a Tag-Name trigger condition — check a\n" +
			"hand-typed condition references a tag that exists before `trigger set` +\n" +
			"a guarded save.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			// The endpoint is server-paginated; loop until a short page so a
			// tenant with many tags never silently truncates (the documented use
			// is an existence check before a guarded save). pagesCap is a runaway
			// backstop, not an expected bound.
			const pageSize, pagesCap = 200, 50
			var pages []json.RawMessage
			full := true
			for page := 0; page < pagesCap && full; page++ {
				raw, err := lc.PlaybookXGetTriggerTags(ctx, map[string]any{
					"searchTerm": grep, "requestedPage": page, "pageSize": pageSize,
				})
				if err != nil {
					return err
				}
				records, ok := triggerTagRecords(raw)
				if !ok {
					// Unrecognized shape: surface the raw page rather than guess.
					return writeRawJSON(os.Stdout, raw)
				}
				pages = append(pages, records...)
				full = len(records) == pageSize
			}
			if jsonOut {
				return emitJSON(pages)
			}
			return emitTriggerTags(pages)
		},
	}
	cmd.Flags().StringVar(&grep, "grep", "", "server-side search term")
	return markJSON(cmd)
}

// triggerTagRecords extracts the page's records: a bare array, or an
// {objectsList|items} wrap. ok=false means an unrecognized shape.
func triggerTagRecords(raw json.RawMessage) ([]json.RawMessage, bool) {
	var records []json.RawMessage
	if err := json.Unmarshal(raw, &records); err == nil {
		return records, true
	}
	var wrap struct {
		ObjectsList []json.RawMessage `json:"objectsList"`
		Items       []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, false
	}
	if wrap.ObjectsList != nil {
		return wrap.ObjectsList, true
	}
	return wrap.Items, true
}

// emitTriggerTags renders the collected tag values; the count reflects the
// lines actually printed (a record whose value lives under an unanticipated
// key is reported, not silently absorbed into the count).
func emitTriggerTags(records []json.RawMessage) error {
	printed, skipped := 0, 0
	for _, r := range records {
		if v := tagValue(r); v != "" {
			fmt.Fprintln(os.Stdout, v)
			printed++
		} else {
			skipped++
		}
	}
	if printed == 0 && skipped == 0 {
		fmt.Fprintln(os.Stdout, "no trigger tags.")
		return nil
	}
	fmt.Fprintf(os.Stdout, "\n%d tag(s).", printed)
	if skipped > 0 {
		fmt.Fprintf(os.Stdout, " (%d record(s) had no recognizable tag value — inspect with --json.)", skipped)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func newSOARPlaybookComponentsUsageCmd() *cobra.Command {
	var (
		actionID    int
		integration string
		action      string
	)
	cmd := &cobra.Command{
		Use:   "usage (--action-id N | --action <name> [--integration <key>])",
		Short: "Read-only: which playbooks use an integration action (impact analysis)",
		Long: "The reverse index for editing or removing an action: every playbook whose\n" +
			"steps reference it. Address the action by its numeric id (--action-id, as\n" +
			"listed by `components actions`) or by name (--action, optionally scoped by\n" +
			"--integration when the name exists in several integrations).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strconv.Itoa(actionID)
			if strings.TrimSpace(action) != "" {
				if actionID != 0 {
					return fmt.Errorf("use --action-id or --action, not both")
				}
				c, err := newSOARClient()
				if err != nil {
					return err
				}
				resolved, err := resolveActionByName(baseContext(), c, integration, action)
				if err != nil {
					return err
				}
				id = resolved
			} else if actionID == 0 {
				return fmt.Errorf("one of --action-id or --action is required")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.PlaybookXGetWorkflowsInvolvingAction(baseContext(), map[string]any{"actionId": id})
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			return emitWorkflowsInvolvingAction(raw)
		},
	}
	f := cmd.Flags()
	f.IntVar(&actionID, "action-id", 0, "the action definition's numeric id")
	f.StringVar(&action, "action", "", "the action's display name (resolved via the all-integration catalog)")
	f.StringVar(&integration, "integration", "", "scope --action to one integration when the name is ambiguous")
	return markJSON(cmd)
}

// resolveActionByName maps an action display name (optionally scoped to one
// integration) to its numeric definition id via the wildcard action catalog.
func resolveActionByName(ctx context.Context, c *soar.Client, integration, name string) (string, error) {
	defs, err := c.ListAllActions(ctx)
	if err != nil {
		return "", err
	}
	matches := matchActionDefs(defs, integration, name)
	switch len(matches) {
	case 1:
		return matches[0].PathID(), nil
	case 0:
		return "", fmt.Errorf("no action named %q%s — see `components actions`", name, scopeSuffix(integration))
	default:
		keys := make([]string, 0, len(matches))
		for i := range matches {
			keys = append(keys, matches[i].Integration+"/"+matches[i].PathID())
		}
		return "", fmt.Errorf("action %q is ambiguous (%s) — scope with --integration or use --action-id", name, strings.Join(keys, ", "))
	}
}

// matchActionDefs filters the catalog by case-insensitive display name and
// optional integration scope.
func matchActionDefs(defs []soar.ActionDef, integration, name string) []soar.ActionDef {
	name = strings.TrimSpace(name)
	integration = strings.TrimSpace(integration)
	var matches []soar.ActionDef
	for i := range defs {
		if !strings.EqualFold(defs[i].DisplayName, name) {
			continue
		}
		if integration != "" && !strings.EqualFold(defs[i].Integration, integration) {
			continue
		}
		matches = append(matches, defs[i])
	}
	return matches
}

func scopeSuffix(integration string) string {
	if integration == "" {
		return ""
	}
	return " in integration " + strconv.Quote(integration)
}

// emitWorkflowsInvolvingAction renders the playbook names using the action.
func emitWorkflowsInvolvingAction(raw json.RawMessage) error {
	var names []string
	if err := json.Unmarshal(raw, &names); err == nil {
		if len(names) == 0 {
			fmt.Fprintln(os.Stdout, "no playbooks use this action.")
			return nil
		}
		for _, n := range names {
			fmt.Fprintln(os.Stdout, n)
		}
		fmt.Fprintf(os.Stdout, "\n%d playbook(s).\n", len(names))
		return nil
	}
	return writeRawJSON(os.Stdout, raw)
}
