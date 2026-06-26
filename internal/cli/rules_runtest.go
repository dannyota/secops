package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newRulesTestCmd dry-runs a YARA-L rule file against historical data WITHOUT
// creating the rule — preview the detections it would produce before deploying.
// Read-only (nothing is stored). Goes beyond `rules validate`, which only
// compile-checks: this previews real matches over a window.
func newRulesTestCmd() *cobra.Command {
	var hours, maxResults int
	cmd := &cobra.Command{
		Use:   "test <file.yaral>",
		Short: "Read-only: dry-run a YARA-L rule against historical data (preview detections, no deploy)",
		Long: "Run a YARA-L rule file over the last --hours of data WITHOUT creating the\n" +
			"rule, and report the detections it would have produced (and any compile\n" +
			"errors). Unlike `rules validate` (compile-check only), this previews real\n" +
			"matches — size a rule's coverage and false-positive load before\n" +
			"`push rules-create`. Read-only: nothing is stored.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := checkHours(hours); err != nil {
				return err
			}
			ruleText, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			start, end := timeWindow(hours)
			res, err := c.RunTestRule(baseContext(), string(ruleText), start, end, maxResults)
			if err != nil {
				return err
			}
			if len(res.CompilationErrors) > 0 && !jsonOut {
				fmt.Fprintf(os.Stderr, "%d compilation error(s):\n", len(res.CompilationErrors))
				for _, e := range res.CompilationErrors {
					fmt.Fprintf(os.Stderr, "  %s\n", string(e))
				}
				return fmt.Errorf("rule did not compile — fix the YARA-L and re-run")
			}
			if jsonOut {
				return emitJSON(map[string]any{
					"detection_count":    len(res.Detections),
					"detections":         res.Detections,
					"compilation_errors": res.CompilationErrors,
				})
			}
			fmt.Printf("%s: %d detection(s) over the last %dh (use --json for the full detections).\n",
				args[0], len(res.Detections), hours)
			return nil
		},
	}
	cmd.Flags().IntVar(&hours, "hours", 24, "look-back window in hours")
	cmd.Flags().IntVar(&maxResults, "max-results", 100, "max detections to return (1-10000)")
	return markJSON(cmd)
}
