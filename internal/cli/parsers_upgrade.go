package cli

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

func newParsersUpgradeCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "upgrade <log-type>",
		Short: "Preview and activate a prebuilt parser update (release candidate)",
		Long: "Check whether a newer version of a prebuilt parser is available, show a diff\n" +
			"of current vs candidate CBN source, and activate the release candidate.\n\n" +
			"Equivalent to the web console's \"Opt in to Preview\" action. Dry-run by default;\n" +
			"pass --yes to activate the candidate.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logType := strings.TrimSpace(args[0])
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			candidates, err := c.FetchParserCandidates(baseContext(), logType, chronicle.ParserActionOptInToPreview)
			if err != nil {
				var apiErr *chronicle.APIError
				if errors.As(err, &apiErr) && apiErr.Status == 500 {
					fmt.Fprintf(os.Stderr, "no preview candidate for %q — parser is up to date (or not prebuilt)\n", logType)
					return nil
				}
				return err
			}
			if jsonOut && (dryRun || !yes) {
				return emitJSON(candidates)
			}
			if len(candidates) == 0 {
				fmt.Fprintf(os.Stderr, "no preview candidate for %q — parser is up to date\n", logType)
				return nil
			}
			cand := candidates[0]
			ver := ""
			if vi, ok := cand.VersionInfo["version"]; ok {
				ver = fmt.Sprintf(" (version %v)", vi)
			}
			fmt.Fprintf(os.Stdout, "Preview candidate for %s%s:\n", logType, ver)
			fmt.Fprintf(os.Stdout, "  parser:  %s\n", parserID(cand.Name))
			fmt.Fprintf(os.Stdout, "  state:   %s\n", orDash(cand.State))
			fmt.Fprintf(os.Stdout, "  stage:   %s\n", orDash(cand.ReleaseStage))
			if cand.CBN != "" {
				decoded, derr := base64.StdEncoding.DecodeString(cand.CBN)
				if derr == nil {
					lines := strings.Count(string(decoded), "\n") + 1
					fmt.Fprintf(os.Stdout, "  cbn:     %d lines (use --json to see full source)\n", lines)
				}
			}
			action := fmt.Sprintf("parsers upgrade %s (activate release candidate %s)", logType, parserID(cand.Name))
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				if err := c.ActivateReleaseCandidateParser(baseContext(), logType, parserID(cand.Name)); err != nil {
					return err
				}
				if jsonOut {
					return emitJSON(cand)
				}
				fmt.Fprintf(os.Stdout, "\nActivated release candidate %s for %q.\n", parserID(cand.Name), logType)
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "activate the release candidate")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

func newParsersRollbackCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "rollback <log-type>",
		Short: "Revert a parser to its previous version (opt out of preview / rollback)",
		Long: "Deactivate the active release-candidate parser for a log type, reverting to\n" +
			"the previous stable version. Equivalent to the web console's\n" +
			"\"Opt out of Preview\" / \"Rollback to Last Used Version\".\n\n" +
			"The parser must have an active RELEASE_CANDIDATE (from a prior upgrade).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logType := strings.TrimSpace(args[0])
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			parsers, err := c.ListParsers(baseContext(), logType)
			if err != nil {
				return err
			}
			var target *chronicle.Parser
			for i := range parsers {
				if parsers[i].State == "ACTIVE" && parsers[i].ReleaseStage == "RELEASE_CANDIDATE" {
					target = &parsers[i]
					break
				}
			}
			if target == nil {
				fmt.Fprintf(os.Stderr, "no active release candidate for %q — nothing to roll back\n", logType)
				return nil
			}
			if jsonOut && (dryRun || !yes) {
				return emitJSON(target)
			}
			fmt.Fprintf(os.Stdout, "Active release candidate for %s:\n", logType)
			fmt.Fprintf(os.Stdout, "  parser:  %s\n", parserID(target.Name))
			fmt.Fprintf(os.Stdout, "  state:   %s\n", orDash(target.State))
			fmt.Fprintf(os.Stdout, "  stage:   %s\n", orDash(target.ReleaseStage))

			label := fmt.Sprintf("parsers rollback %s (deactivate %s)", logType, parserID(target.Name))
			return guardedSIEMMutation(label, dryRun, yes, func() error {
				if err := c.DeactivateParser(baseContext(), logType, parserID(target.Name)); err != nil {
					return err
				}
				if jsonOut {
					return emitJSON(target)
				}
				fmt.Fprintf(os.Stdout, "\nReverted %q — deactivated release candidate %s.\n", logType, parserID(target.Name))
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply the rollback")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}
