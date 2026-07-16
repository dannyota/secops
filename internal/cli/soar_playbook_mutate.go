package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newSOARPlaybookDeleteCmd() *cobra.Command {
	var (
		name       string
		identifier string
		fromFile   string
		dryRun     bool
		yes        bool
	)
	cmd := &cobra.Command{
		Use:   "delete (--name <playbook> | --identifier <uuid>[,uuid,...] | --from-file <path>)",
		Short: "MUTATING (guarded): delete one or more playbooks permanently",
		Long: "Delete playbook definitions by name or identifier UUID.\n\n" +
			"Single: --name resolves to the definition id via the live playbook list\n" +
			"(case-insensitive exact match).\n\n" +
			"Batch: --identifier accepts comma-separated UUIDs, or --from-file reads\n" +
			"one UUID per line (blank lines and #-comments skipped). A batch delete\n" +
			"uses a single API call and reports per-playbook success/failure.\n\n" +
			"Guarded: dry-run by default, --yes to apply. Deleting a playbook that\n" +
			"is attached to a case stops its execution — irreversible.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			name = strings.TrimSpace(name)
			identifier = strings.TrimSpace(identifier)
			fromFile = strings.TrimSpace(fromFile)

			ids, err := collectPlaybookDeleteIDs(name, identifier, fromFile)
			if err != nil {
				return err
			}

			lc, lerr := newSOARLegacyClient()
			if lerr != nil {
				return lerr
			}
			ctx := baseContext()

			// Resolve name → identifier for the single-name path.
			if name != "" {
				resolved, rerr := resolvePlaybookDefinition(ctx, lc, name)
				if rerr != nil {
					return rerr
				}
				ids = []string{resolved}
			}

			label := formatDeleteLabel(name, ids)
			dr, ay := soarGuard("playbook delete "+label, dryRun, yes)
			if dr {
				fmt.Fprintf(os.Stdout, "DRY RUN — would delete %d playbook(s): %s\n", len(ids), label)
				return nil
			}
			if !ay {
				fmt.Fprintln(os.Stdout, "Refusing to delete without confirmation (pass --yes). Aborted.")
				return nil
			}

			// For batch (>1), don't use preferModern — a partial success
			// must not trigger a legacy fallback that re-deletes succeeded items.
			if len(ids) == 1 {
				return preferModern("playbooks delete",
					func() error {
						mc, merr := newSOARClient()
						if merr != nil {
							return merr
						}
						_, merr = mc.DeleteWorkflows(ctx, ids)
						if merr != nil {
							return merr
						}
						fmt.Fprintf(cmd.OutOrStdout(), "deleted %s\n", ids[0])
						return nil
					},
					func() error {
						body := map[string]any{"identifiers": ids}
						_, lerr := lc.DeleteWorkflows(ctx, body)
						if lerr != nil {
							return lerr
						}
						fmt.Fprintf(cmd.OutOrStdout(), "deleted %s\n", ids[0])
						return nil
					},
				)
			}
			// Batch: try modern first; on transport error fall back to legacy.
			// On a successful API response, report per-item results and don't retry.
			mc, merr := newSOARClient()
			if merr == nil {
				raw, merr := mc.DeleteWorkflows(ctx, ids)
				if merr == nil {
					return reportBatchDelete(cmd.OutOrStdout(), ids, raw)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "playbooks delete: modern path failed (%v) — falling back to legacy\n", merr)
			}
			body := map[string]any{"identifiers": ids}
			raw, lerr := lc.DeleteWorkflows(ctx, body)
			if lerr != nil {
				return lerr
			}
			return reportBatchDelete(cmd.OutOrStdout(), ids, raw)
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "playbook name (resolved to its id via the live playbook list)")
	f.StringVar(&identifier, "identifier", "", "playbook definition UUID(s), comma-separated")
	f.StringVar(&fromFile, "from-file", "", "file with one playbook UUID per line")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("name", "identifier", "from-file")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}

func collectPlaybookDeleteIDs(name, identifier, fromFile string) ([]string, error) {
	if name != "" {
		return nil, nil // resolved later
	}
	if fromFile != "" {
		data, err := os.ReadFile(fromFile)
		if err != nil {
			return nil, fmt.Errorf("reading --from-file: %w", err)
		}
		var ids []string
		for line := range strings.SplitSeq(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			ids = append(ids, line)
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("--from-file %q contains no identifiers", fromFile)
		}
		return ids, nil
	}
	if identifier != "" {
		var ids []string
		for id := range strings.SplitSeq(identifier, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("--identifier is empty")
		}
		return ids, nil
	}
	return nil, fmt.Errorf("pass --name, --identifier, or --from-file")
}

func formatDeleteLabel(name string, ids []string) string {
	if name != "" {
		return fmt.Sprintf("%q", name)
	}
	if len(ids) <= 3 {
		return strings.Join(ids, ", ")
	}
	return fmt.Sprintf("%s, ... (%d total)", strings.Join(ids[:2], ", "), len(ids))
}

func reportBatchDelete(w io.Writer, ids []string, raw json.RawMessage) error {
	if len(ids) == 1 {
		fmt.Fprintf(w, "deleted playbook %s\n", ids[0])
		return nil
	}
	var resp struct {
		Results []struct {
			Identifier   string `json:"identifier"`
			Failed       bool   `json:"failed"`
			ErrorMessage string `json:"errorMessage"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Results) == 0 {
		fmt.Fprintf(w, "deleted %d playbook(s)\n", len(ids))
		return nil
	}
	var ok, fail int
	for _, r := range resp.Results {
		if r.Failed {
			fmt.Fprintf(w, "FAILED  %s: %s\n", r.Identifier, r.ErrorMessage)
			fail++
		} else {
			fmt.Fprintf(w, "deleted %s\n", r.Identifier)
			ok++
		}
	}
	if fail > 0 {
		return fmt.Errorf("%d of %d playbook(s) failed to delete", fail, ok+fail)
	}
	return nil
}

func newSOARPlaybookDeployCmd() *cobra.Command {
	var (
		name       string
		identifier string
		enable     bool
		disable    bool
		dryRun     bool
		yes        bool
	)
	cmd := &cobra.Command{
		Use:   "deploy (--name <playbook> | --identifier <uuid>) --enable|--disable",
		Short: "MUTATING (guarded): enable or disable a playbook",
		Long: "Toggle a playbook's isEnabled state. Reads the full definition, flips the\n" +
			"flag, and saves via SaveWorkflowDefinitions (the only API path — this mints a\n" +
			"new version). Guarded: dry-run by default, --yes to apply.\n\n" +
			"Mirrors `rules-deploy` for consistency.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !enable && !disable {
				return fmt.Errorf("pass --enable or --disable")
			}
			wantEnabled := enable

			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			identifier, err = resolvePlaybookSelector(ctx, lc, name, identifier)
			if err != nil {
				return err
			}

			// Read the full definition.
			raw, err := lc.GetWorkflowFullInfo(ctx, identifier)
			if err != nil {
				return err
			}
			var def map[string]any
			if err := json.Unmarshal(raw, &def); err != nil {
				return fmt.Errorf("decode playbook definition: %w", err)
			}

			currentEnabled, _ := def["isEnabled"].(bool)
			pbName, _ := def["name"].(string)
			if pbName == "" {
				pbName = identifier
			}

			toggle := "disable"
			if wantEnabled {
				toggle = "enable"
			}

			if currentEnabled == wantEnabled {
				fmt.Fprintf(os.Stdout, "playbook %q is already %sd — nothing to do.\n", pbName, toggle)
				return nil
			}

			action := fmt.Sprintf("playbook deploy %s → %s", pbName, toggle)
			if !jsonOut {
				fmt.Fprintf(os.Stdout, "Playbook: %q (%s)\n", pbName, identifier)
				fmt.Fprintf(os.Stdout, "  isEnabled: %v → %v (mints a new version)\n", currentEnabled, wantEnabled)
			}

			return soarGuardedMutation(action, dryRun, yes, func() error {
				def["isEnabled"] = wantEnabled
				deployed := false
				if !forceLegacy {
					mc, merr := newSOARClient()
					if merr == nil {
						if _, merr = mc.SaveWorkflowDefinitions(ctx, def); merr == nil {
							deployed = true
						} else if !isEnumTypeMismatch(merr) {
							fmt.Fprintf(os.Stderr, "playbooks deploy: modern v1alpha path failed (%v) — falling back to legacy\n", merr)
						}
					}
				}
				if !deployed {
					if _, lerr := lc.SaveWorkflowDefinitions(ctx, def); lerr != nil {
						return wrapPlaybookSaveError(lerr)
					}
				}
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "playbook name (resolved to its id via the live playbook list)")
	f.StringVar(&identifier, "identifier", "", "playbook definition UUID (overrides --name)")
	f.BoolVar(&enable, "enable", false, "set isEnabled=true")
	f.BoolVar(&disable, "disable", false, "set isEnabled=false")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("enable", "disable")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return cmd
}
