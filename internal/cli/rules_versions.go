package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// newRulesVersionsCmd lists a rule's saved revisions (each save/deploy mints
// one) so an engineer can see its history and pull a prior version's text —
// the input to a diff or a rollback. Read-only.
func newRulesVersionsCmd() *cobra.Command {
	var show int
	cmd := &cobra.Command{
		Use:   "versions <rule-id> [--show N]",
		Short: "Read-only: list a rule's saved revisions (history); --show N prints one's YARA-L",
		Long: "List a rule's revision history (newest first) — each save/deploy mints a\n" +
			"revision, identified by the @v_… suffix on its resource name. --show N prints\n" +
			"the YARA-L text of the Nth listed revision, so two revisions can be diffed\n" +
			"externally (`--show 1 > a; --show 2 > b; diff a b`) or rolled back to.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			revs, err := c.ListRuleRevisions(baseContext(), args[0])
			if err != nil {
				return err
			}
			if len(revs) == 0 {
				return fmt.Errorf("rule %s has no revisions", args[0])
			}
			if show > 0 {
				if show > len(revs) {
					return fmt.Errorf("--show %d out of range (%d revision(s))", show, len(revs))
				}
				text := revs[show-1].Text
				if text == "" { // the revision list is BASIC; fetch the full versioned rule for its text
					if _, id, ok := strings.Cut(revs[show-1].Name, "/rules/"); ok {
						if full, gerr := c.GetRule(baseContext(), id); gerr == nil {
							text = full.Text
						}
					}
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
				rev := r.Name
				if at := strings.LastIndex(rev, "@"); at >= 0 {
					rev = rev[at+1:]
				}
				fmt.Fprintf(tw, "%d\t%s\t%s\t%d\n", i+1, rev, r.DisplayName, len(r.Text))
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "\n%d revision(s).\n", len(revs))
			return nil
		},
	}
	cmd.Flags().IntVar(&show, "show", 0, "print the Nth listed revision's YARA-L text instead of the list")
	return markJSON(cmd)
}
