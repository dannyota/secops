package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newParsersDeleteCmd deletes a specific parser version. An ACTIVE parser is
// refused unless --force is set (which triggers the API's force-delete path).
func newParsersDeleteCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
		force  bool
	)
	cmd := &cobra.Command{
		Use:   "delete <log-type> <parser-id>",
		Short: "MUTATING (guarded): delete a parser version",
		Long: "Delete a specific parser version by log type and parser id. An ACTIVE parser\n" +
			"is refused unless --force is set — this is a safeguard against accidentally\n" +
			"removing the live parser. Use `parsers versions` to list a log type's versions\n" +
			"and find the parser-id to delete (e.g. an old inactive version).",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			logType := strings.TrimSpace(args[0])
			pid := strings.TrimSpace(args[1])
			if logType == "" || pid == "" {
				return fmt.Errorf("both LOG_TYPE and PARSER_ID are required")
			}
			action := fmt.Sprintf("parsers delete %s/%s", logType, pid)

			// Pre-flight: read the parser to confirm it exists and check its state.
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			p, err := c.GetParser(baseContext(), logType, pid)
			if err != nil {
				return fmt.Errorf("get parser %s/%s: %w", logType, pid, err)
			}
			if p.State == "ACTIVE" && !force {
				return fmt.Errorf("parser %s/%s is ACTIVE — pass --force to delete an active parser", logType, pid)
			}

			return guardedSIEMMutation(action, dryRun, yes, func() error {
				return c.DeleteParser(baseContext(), logType, pid, force)
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	f.BoolVar(&force, "force", false, "force-delete even if the parser is ACTIVE")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}
