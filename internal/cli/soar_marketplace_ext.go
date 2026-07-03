package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newSOARFeaturedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "featured <verb>",
		Short: "Browse and install Google-curated featured playbooks",
	}
	cmd.AddCommand(newSOARFeaturedListCmd(), newSOARFeaturedInstallCmd())
	return cmd
}

func newSOARFeaturedListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List featured playbooks from the Content Hub",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			raw, err := mc.ListFeaturedPlaybooks(baseContext())
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "featured playbooks", raw)
			return nil
		},
	}
	return markJSON(cmd)
}

func newSOARFeaturedInstallCmd() *cobra.Command {
	var (
		name   string
		env    string
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "install --name <uid>",
		Short: "MUTATING (guarded): install a featured playbook or block",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			uid := strings.TrimSpace(name)
			if uid == "" {
				return fmt.Errorf("--name is required (the uid from `featured list --json`)")
			}
			uid = lastSegment(uid)
			label := "featured install " + uid
			dr, ay := soarGuard(label, dryRun, yes)
			fmt.Fprintf(os.Stdout, "Install featured playbook: %s (env: %s)\n", uid, env)
			if dr {
				fmt.Fprintln(os.Stdout, "DRY RUN — no API call made. Re-run with --yes to apply.")
				return nil
			}
			if !ay {
				fmt.Fprintln(os.Stdout, "Refused. Pass --yes.")
				return nil
			}
			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			body := map[string]any{"environments": []string{env}}
			raw, err := mc.InstallFeaturedPlaybook(baseContext(), uid, body)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			fmt.Fprintln(os.Stdout, "installed.")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "featured playbook uid (from `featured list --json`, or the full resource name)")
	f.StringVar(&env, "env", "Default Environment", "target environment name")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

func newSOARMarketplaceDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <integration-id>",
		Short: "Show the diff between installed and marketplace version of an integration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mc, err := newSOARClient()
			if err != nil {
				return err
			}
			raw, err := mc.FetchCommercialDiff(baseContext(), strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printCommercialDiff(raw, args[0])
			return nil
		},
	}
	return markJSON(cmd)
}

func printCommercialDiff(raw json.RawMessage, identifier string) {
	var d struct {
		Version string `json:"version"`
		Type    string `json:"type"`
		Custom  bool   `json:"custom"`
		Diff    struct {
			Actions      diffBucket `json:"actions"`
			Connectors   diffBucket `json:"connectors"`
			Jobs         diffBucket `json:"jobs"`
			Managers     diffBucket `json:"managers"`
			Transformers diffBucket `json:"transformers"`
		} `json:"diff"`
		Actions    []json.RawMessage `json:"actions"`
		Connectors []json.RawMessage `json:"connectors"`
		Jobs       []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		fmt.Fprintf(os.Stdout, "Identifier: %s\n(parse error: %v; use --json)\n", identifier, err)
		return
	}
	fmt.Fprintf(os.Stdout, "Identifier:  %s\n", identifier)
	fmt.Fprintf(os.Stdout, "Version:     %s (marketplace)\n", d.Version)
	fmt.Fprintf(os.Stdout, "Type:        %s\n", d.Type)
	fmt.Fprintf(os.Stdout, "Actions:     %d total\n", len(d.Actions))
	fmt.Fprintf(os.Stdout, "Connectors:  %d total\n", len(d.Connectors))
	fmt.Fprintf(os.Stdout, "Jobs:        %d total\n", len(d.Jobs))
	printDiffBucket("Actions", d.Diff.Actions)
	printDiffBucket("Connectors", d.Diff.Connectors)
	printDiffBucket("Jobs", d.Diff.Jobs)
	printDiffBucket("Managers", d.Diff.Managers)
	printDiffBucket("Transformers", d.Diff.Transformers)
	fmt.Println("\n(--json for the full record)")
}

type diffBucketItem struct {
	DisplayName string `json:"displayName"`
	Custom      bool   `json:"custom"`
}

type diffBucket struct {
	Keep     []diffBucketItem `json:"keep"`
	Override []diffBucketItem `json:"override"`
}

func printDiffBucket(label string, b diffBucket) {
	if len(b.Keep) == 0 && len(b.Override) == 0 {
		return
	}
	fmt.Fprintf(os.Stdout, "\n%s diff: %d keep, %d override\n", label, len(b.Keep), len(b.Override))
	for _, item := range b.Override {
		tag := ""
		if item.Custom {
			tag = " (custom)"
		}
		fmt.Fprintf(os.Stdout, "  override  %s%s\n", item.DisplayName, tag)
	}
}
