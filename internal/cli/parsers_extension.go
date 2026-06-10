package cli

import (
	"fmt"
	"os"
	"strings"

	"danny.vn/secops/chronicle"

	"github.com/spf13/cobra"
)

func newParsersExtensionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extension <verb>",
		Short: "Manage low-code parser extensions (list / get / create / activate / delete)",
	}
	cmd.AddCommand(
		newExtListCmd(),
		newExtGetCmd(),
		newExtCreateCmd(),
		newExtActivateCmd(),
		newExtDeleteCmd(),
	)
	return cmd
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
	return cmd
}

func newExtGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <log-type> <extension-id>",
		Short: "Get a parser extension",
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
	return cmd
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
				return err
			}
			cfg := &chronicle.ParserExtensionConfig{CbnSnippet: string(cbn)}
			if strings.TrimSpace(logFile) != "" {
				log, lerr := os.ReadFile(logFile)
				if lerr != nil {
					return lerr
				}
				cfg.Log = string(log)
			}
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
	return cmd
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
	return cmd
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
	return cmd
}
