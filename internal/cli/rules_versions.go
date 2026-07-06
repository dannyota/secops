package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// newRulesVersionsCmd lists a rule's saved revisions (each save/deploy mints one)
// and hosts the `diff` / `restore` subcommands. Run with a <rule-id> it lists the
// history; `--show N` prints one revision's YARA-L. Read-only except `restore`.
func newRulesVersionsCmd() *cobra.Command {
	var show int
	cmd := &cobra.Command{
		Use:   "versions <rule-id> [--show N]",
		Short: "List a rule's saved revisions with diff and restore",
		Long: "List a rule's revision history (newest first) — each save/deploy mints a\n" +
			"revision, identified by the @v_… suffix on its resource name. --show N prints\n" +
			"the YARA-L of the Nth listed revision. Compare two revisions with\n" +
			"`rules versions diff`, or roll back with the guarded `rules versions restore`.",
		// The parent lists; diff/restore are subcommands. A rule id (ru_…) never
		// collides with a subcommand name, so cobra routes correctly either way.
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("a single <rule-id> is required (or use `diff` / `restore`)")
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			// Accept an id, display name, or slug — same as the diff/restore
			// subcommands and every other `rules <verb> <rule>`.
			ruleID, err := resolveRuleID(ctx, c, args[0])
			if err != nil {
				return err
			}
			revs, err := c.ListRuleRevisions(ctx, ruleID)
			if err != nil {
				return err
			}
			if len(revs) == 0 {
				return fmt.Errorf("rule %s has no revisions", ruleID)
			}
			if show > 0 {
				if show > len(revs) {
					return fmt.Errorf("--show %d out of range (%d revision(s))", show, len(revs))
				}
				text, err := revisionText(ctx, c, &revs[show-1])
				if err != nil {
					return err
				}
				fmt.Fprint(os.Stdout, text)
				return nil
			}
			if jsonOut {
				return emitJSON(revs)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "#\tREVISION\tDISPLAY NAME\tYARA-L BYTES")
			for i, r := range revs {
				fmt.Fprintf(tw, "%d\t%s\t%s\t%d\n", i+1, revisionToken(r.Name), r.DisplayName, len(r.Text))
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "\n%d revision(s).\n", len(revs))
			return nil
		},
	}
	cmd.Flags().IntVar(&show, "show", 0, "print the Nth listed revision's YARA-L text instead of the list")
	cmd.AddCommand(newRulesVersionsDiffCmd(), newRulesVersionsRestoreCmd())
	return markJSON(cmd)
}

func newRulesVersionsDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <rule> <a> <b>",
		Short: "Read-only: line-by-line diff of two of a rule's revisions",
		Long: "Diff the YARA-L of two revisions of a rule. Each of <a>/<b> is either a\n" +
			"1-based index from `rules versions <rule>` or a v_… version token. <rule> is\n" +
			"an id, display name, or slug.",
		Args: cobra.ExactArgs(3),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			ruleID, err := resolveRuleID(ctx, c, args[0])
			if err != nil {
				return err
			}
			revs, err := c.ListRuleRevisions(ctx, ruleID)
			if err != nil {
				return err
			}
			ta, la, err := resolveRevision(ctx, c, ruleID, revs, args[1])
			if err != nil {
				return err
			}
			tb, lb, err := resolveRevision(ctx, c, ruleID, revs, args[2])
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(map[string]any{
					"rule": ruleID, "a": la, "b": lb,
					"identical": ta == tb,
					"diff":      unifiedDiff(ta, tb, la, lb),
				})
			}
			if ta == tb {
				fmt.Fprintf(os.Stdout, "revisions %s and %s are identical.\n", la, lb)
				return nil
			}
			fmt.Fprint(os.Stdout, unifiedDiff(ta, tb, la, lb))
			return nil
		},
	}
	return markJSON(cmd)
}

func newRulesVersionsRestoreCmd() *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "restore <rule> <version>",
		Short: "MUTATING (guarded): re-apply a prior revision's YARA-L as a new revision",
		Long: "Roll a rule back to a prior revision by re-applying that revision's YARA-L —\n" +
			"this creates a NEW revision with the old text (it does not erase history).\n" +
			"<version> is a 1-based index from `rules versions <rule>` or a v_… token.\n" +
			"Guarded: dry-run by default (prints the diff), --yes to apply.",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			ruleID, err := resolveRuleID(ctx, c, args[0])
			if err != nil {
				return err
			}
			cur, err := c.GetRule(ctx, ruleID)
			if err != nil {
				return err
			}
			revs, err := c.ListRuleRevisions(ctx, ruleID)
			if err != nil {
				return err
			}
			target, label, err := resolveRevision(ctx, c, ruleID, revs, args[1])
			if err != nil {
				return err
			}
			if strings.TrimSpace(target) == "" {
				return fmt.Errorf("revision %s has no YARA-L text to restore", label)
			}
			if target == cur.Text {
				if !jsonOut {
					fmt.Fprintf(os.Stdout, "rule %s already matches revision %s — nothing to restore.\n", ruleID, label)
				}
				return nil
			}
			if !jsonOut {
				fmt.Fprintf(os.Stderr, "restore preview (current → %s):\n%s\n", label,
					unifiedDiff(cur.Text, target, "current", label))
			}
			action := fmt.Sprintf("restore rule %s to revision %s", ruleID, label)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				nr, err := c.UpdateRule(ctx, ruleID, target, cur.Etag)
				if err != nil {
					return err
				}
				if !jsonOut {
					fmt.Fprintf(os.Stdout, "restored — new revision %s. Re-run `pull rules` to refresh the mirror.\n",
						revisionToken(nr.Name))
				}
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

// resolveRevision maps ref (a 1-based list index or a v_… token) to that
// revision's YARA-L text and a human label. revs is the ListRuleRevisions output
// (newest first); a token not in the list is fetched directly.
func resolveRevision(ctx context.Context, c *chronicle.Client, ruleID string, revs []chronicle.Rule, ref string) (text, label string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", fmt.Errorf("a revision index or v_… token is required")
	}
	// Numeric → 1-based index into the listed revisions.
	if n, convErr := strconv.Atoi(ref); convErr == nil {
		if n < 1 || n > len(revs) {
			return "", "", fmt.Errorf("revision index %d out of range (%d revision(s))", n, len(revs))
		}
		t, terr := revisionText(ctx, c, &revs[n-1])
		return t, revisionToken(revs[n-1].Name), terr
	}
	// Otherwise treat ref as a v_… version token.
	full, gerr := c.GetRuleRevision(ctx, ruleID, ref)
	if gerr != nil {
		return "", "", gerr
	}
	return full.Text, revisionToken(full.Name), nil
}

// revisionText returns a listed revision's YARA-L, fetching the full versioned
// rule when the list element omitted the text.
func revisionText(ctx context.Context, c *chronicle.Client, r *chronicle.Rule) (string, error) {
	if r.Text != "" {
		return r.Text, nil
	}
	if _, id, ok := strings.Cut(r.Name, "/rules/"); ok {
		full, err := c.GetRule(ctx, id)
		if err != nil {
			return "", err
		}
		return full.Text, nil
	}
	return "", nil
}

// revisionToken extracts the v_… version token from a rule resource name (the
// part after '@'), or the trailing segment when there is no version suffix.
func revisionToken(name string) string {
	if at := strings.LastIndex(name, "@"); at >= 0 {
		return name[at+1:]
	}
	return lastSegment(name)
}
