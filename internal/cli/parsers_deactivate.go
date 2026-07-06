package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newParsersDeactivateCmd deactivates a custom parser, reverting the log type to
// its prebuilt parser. Matches the console's "Make Inactive" context-menu action.
func newParsersDeactivateCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "deactivate <log-type> [parser-id]",
		Short: "MUTATING (guarded): deactivate a custom parser (reverts to prebuilt)",
		Long: "Deactivate a custom parser for a log type — live ingestion reverts to the\n" +
			"prebuilt parser. Matches the console's \"Make Inactive\" action.\n\n" +
			"With one argument (log-type only), the ACTIVE CUSTOM parser is auto-selected.\n" +
			"The parser version is preserved as INACTIVE and can be re-activated later.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			logType := strings.TrimSpace(args[0])
			if logType == "" {
				return fmt.Errorf("a LOG_TYPE is required")
			}
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			var pid string
			if len(args) == 2 {
				pid = strings.TrimSpace(args[1])
			}
			if pid == "" {
				ps, lerr := c.ListParsers(baseContext(), logType)
				if lerr != nil {
					return lerr
				}
				for i := range ps {
					if ps[i].State == "ACTIVE" && ps[i].Type == "CUSTOM" {
						pid = parserID(ps[i].Name)
						break
					}
				}
				if pid == "" {
					return fmt.Errorf("no ACTIVE CUSTOM parser for %q — nothing to deactivate", logType)
				}
			}
			action := fmt.Sprintf("parsers deactivate %s/%s", logType, pid)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				if derr := c.DeactivateParser(baseContext(), logType, pid); derr != nil {
					return derr
				}
				fmt.Printf("Deactivated parser %s for %q — prebuilt parser is now active.\n", pid, logType)
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
