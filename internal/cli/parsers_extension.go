package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/docs/tips"

	"github.com/spf13/cobra"
)

func newParsersExtensionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extension <verb>",
		Short: "Manage parser extensions (list, create, activate, extract)",
	}
	cmd.AddCommand(
		newExtListCmd(),
		newExtGetCmd(),
		newExtCreateCmd(),
		newExtUpdateCmd(),
		newExtActivateCmd(),
		newExtDeleteCmd(),
		newExtExtractCmd(),
		newExtSettingCmd(),
		newExtTipsCmd(),
	)
	return cmd
}

// newExtTipsCmd prints the embedded parser-extension authoring guide — the
// non-obvious CBN patterns (grok named-capture requirement, JSON-escaped
// quotes, event_type override merge semantics, fields the validator rejects)
// that cost debugging time when learned from the error messages alone.
func newExtTipsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tips",
		Short: "Print the parser-extension authoring guide (proven CBN patterns, offline)",
		Long: "Print the parser-extension authoring guide embedded in the binary: extension\n" +
			"lifecycle, testing with `parsers run`, and the non-obvious CBN patterns —\n" +
			"grok's named-capture requirement, JSON-escaped quote matching, event_type\n" +
			"override merge semantics, validator-rejected field mappings, and multi-format\n" +
			"gating. Offline; no API calls. Same content as docs/tips/12-parser-extensions.md.",
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprint(cmd.OutOrStdout(), tips.ParserExtensions())
		},
	}
}

func newExtListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <log-type>",
		Short: "List parser extensions for a log type",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			exts, err := c.ListParserExtensions(baseContext(), strings.TrimSpace(args[0]), 0)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(exts)
			}
			if len(exts) == 0 {
				fmt.Fprintln(os.Stdout, "no parser extensions.")
				return nil
			}
			for _, e := range exts {
				fmt.Fprintf(os.Stdout, "%-10s %s\n", orDash(e.State), lastSegment(e.Name))
			}
			fmt.Fprintf(os.Stdout, "\n%d extension(s).\n", len(exts))
			return nil
		},
	}
	return markJSON(cmd)
}

func newExtGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <log-type> <extension-id>",
		Short: "Show a parser extension's state and CBN snippet",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newChronicleClient()
			if err != nil {
				return err
			}
			ext, err := c.GetParserExtension(baseContext(), strings.TrimSpace(args[0]), strings.TrimSpace(args[1]))
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(ext)
			}
			fmt.Fprintf(os.Stdout, "name:  %s\nstate: %s\n", ext.Name, orDash(ext.State))
			if ext.CbnSnippet != "" {
				fmt.Fprintf(os.Stdout, "cbn_snippet: (present, %d chars)\n", len(ext.CbnSnippet))
			}
			return nil
		},
	}
	return markJSON(cmd)
}

func newExtCreateCmd() *cobra.Command {
	var (
		logType string
		cbnFile string
		logFile string
		dryRun  bool
		yes     bool
	)
	cmd := &cobra.Command{
		Use:   "create --log-type <type> --cbn <file>",
		Short: "MUTATING (guarded): create a parser extension from a CBN file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(logType) == "" {
				return fmt.Errorf("--log-type is required")
			}
			if strings.TrimSpace(cbnFile) == "" {
				return fmt.Errorf("--cbn is required")
			}
			cbn, err := os.ReadFile(cbnFile)
			if err != nil {
				return fmt.Errorf("read --cbn %s: %w", cbnFile, err)
			}
			sampleLog := ""
			if strings.TrimSpace(logFile) != "" {
				log, lerr := os.ReadFile(logFile)
				if lerr != nil {
					return fmt.Errorf("read --log %s: %w", logFile, lerr)
				}
				sampleLog = string(log)
			}
			cfg := chronicle.NewCBNSnippetConfig(string(cbn), sampleLog)
			action := fmt.Sprintf("parsers extension create %s from %s", logType, cbnFile)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, cerr := newChronicleClient()
				if cerr != nil {
					return cerr
				}
				ext, cerr := c.CreateParserExtension(baseContext(), logType, cfg)
				if cerr != nil {
					return cerr
				}
				fmt.Fprintf(os.Stdout, "created extension %s (state %s)\n", lastSegment(ext.Name), orDash(ext.State))
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&logType, "log-type", "", "log type (required)")
	f.StringVar(&cbnFile, "cbn", "", "CBN extension snippet file (required)")
	f.StringVar(&logFile, "log", "", "sample log file (optional, for validation)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

func newExtUpdateCmd() *cobra.Command {
	var (
		logType string
		cbnFile string
		logFile string
		extID   string
		dryRun  bool
		yes     bool
	)
	cmd := &cobra.Command{
		Use:   "update --log-type <type> --cbn <file> [--log <file>] [--ext <id>]",
		Short: "MUTATING (guarded): replace a parser extension (delete → create → validate → activate)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(logType) == "" {
				return fmt.Errorf("--log-type is required")
			}
			if strings.TrimSpace(cbnFile) == "" {
				return fmt.Errorf("--cbn is required")
			}
			cbn, err := os.ReadFile(cbnFile)
			if err != nil {
				return fmt.Errorf("read --cbn %s: %w", cbnFile, err)
			}
			sampleLog := ""
			if strings.TrimSpace(logFile) != "" {
				log, lerr := os.ReadFile(logFile)
				if lerr != nil {
					return fmt.Errorf("read --log %s: %w", logFile, lerr)
				}
				sampleLog = string(log)
			}

			targetID := strings.TrimSpace(extID)
			if targetID == "" {
				c, cerr := newChronicleClient()
				if cerr != nil {
					return cerr
				}
				exts, lerr := c.ListParserExtensions(baseContext(), logType, 0)
				if lerr != nil {
					return lerr
				}
				switch len(exts) {
				case 0:
					return fmt.Errorf("no parser extensions for %s — nothing to update", logType)
				case 1:
					targetID = exts[0].ID()
				default:
					return fmt.Errorf("%d parser extensions for %s — pass --ext to choose one", len(exts), logType)
				}
			}

			cfg := chronicle.NewCBNSnippetConfig(string(cbn), sampleLog)
			action := fmt.Sprintf("parsers extension update %s/%s from %s", logType, targetID, cbnFile)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, cerr := newChronicleClient()
				if cerr != nil {
					return cerr
				}
				ctx := baseContext()

				if err := c.DeleteParserExtension(ctx, logType, targetID); err != nil {
					return fmt.Errorf("delete old extension %s: %w", targetID, err)
				}
				fmt.Fprintf(os.Stdout, "deleted extension %s\n", targetID)

				ext, cerr := c.CreateParserExtension(ctx, logType, cfg)
				if cerr != nil {
					return fmt.Errorf("create replacement extension: %w", cerr)
				}
				newID := ext.ID()
				fmt.Fprintf(os.Stdout, "created extension %s (state %s) — waiting for validation…\n", newID, orDash(ext.State))

				if err := waitExtensionValidated(ctx, c, logType, newID); err != nil {
					return err
				}

				if err := c.ActivateParserExtension(ctx, logType, newID); err != nil {
					return fmt.Errorf("activate extension %s: %w", newID, err)
				}
				fmt.Fprintf(os.Stdout, "activated extension %s\n", newID)
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&logType, "log-type", "", "log type (required)")
	f.StringVar(&cbnFile, "cbn", "", "CBN extension snippet file (required)")
	f.StringVar(&logFile, "log", "", "sample log file (optional, for validation)")
	f.StringVar(&extID, "ext", "", "extension ID to replace (auto-detected if only one exists)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

const extValidateTimeout = 5 * time.Minute

// waitExtensionValidated polls until the extension's validation settles or
// times out. Mirrors waitParserValidated (parsers_surface.go) for the
// extension state machine: NEW → VALIDATING → LIVE (or FAILED verdict).
func waitExtensionValidated(ctx context.Context, c *chronicle.Client, logType, extID string) error {
	deadline := time.Now().Add(extValidateTimeout)
	wait := 2 * time.Second
	for {
		ext, err := c.GetParserExtension(ctx, logType, extID)
		if err != nil {
			return err
		}
		state := strings.ToUpper(ext.State)
		if state == "LIVE" {
			return nil
		}
		if ext.Validation != nil {
			verdict := strings.ToUpper(ext.Validation.VerdictType)
			if strings.Contains(verdict, "PASSED") {
				return nil
			}
			if strings.Contains(verdict, "FAILED") {
				return fmt.Errorf("extension %s validation FAILED (verdict %s)", extID, ext.Validation.VerdictType)
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("extension %s created but validation did not settle within %s (state %q) — activate manually: `parsers extension activate %s %s --yes`",
				extID, extValidateTimeout, ext.State, logType, extID)
		}
		time.Sleep(wait)
		wait = min(wait*2, 30*time.Second)
	}
}

func newExtActivateCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "activate <log-type> <extension-id>",
		Short: "MUTATING (guarded): activate a parser extension (make it live)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			logType := strings.TrimSpace(args[0])
			extID := strings.TrimSpace(args[1])
			action := fmt.Sprintf("parsers extension activate %s/%s", logType, extID)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				return c.ActivateParserExtension(baseContext(), logType, extID)
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

func newExtDeleteCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "delete <log-type> <extension-id>",
		Short: "MUTATING (guarded): delete a parser extension",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			logType := strings.TrimSpace(args[0])
			extID := strings.TrimSpace(args[1])
			action := fmt.Sprintf("parsers extension delete %s/%s", logType, extID)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				return c.DeleteParserExtension(baseContext(), logType, extID)
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}
