package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// `secopsctl commands` — the machine-readable command catalog. `surfaces` maps
// the API families; this maps the VERBS: every runnable command with its kind
// (read vs guarded-mutation), so an agent or an allowlist generator can
// enumerate the surface offline instead of walking ~100 --help screens.

func init() { rootCmd.AddCommand(newCommandsCmd()) }

// jsonAnnotation marks a command whose output honors the global --json flag.
// A constructor calls markJSON when its RunE (or a helper it calls) changes
// output under jsonOut; the catalog reads the annotation back so agents can
// answer "does this command speak JSON?" offline, without the hand-maintained
// prose list in docs.
const jsonAnnotation = "secopsctl_json"

// markJSON tags cmd as honoring the global --json flag and returns it for
// wrap-style use at a constructor's `return`.
func markJSON(cmd *cobra.Command) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[jsonAnnotation] = "true"
	return cmd
}

// commandRow is one catalog entry (the --json element shape).
type commandRow struct {
	Path  string   `json:"path"`            // command path without the binary name
	Kind  string   `json:"kind"`            // "read" | "guarded-mutation"
	JSON  bool     `json:"json"`            // honors the global --json flag
	Short string   `json:"short"`           // one-line description
	Flags []string `json:"flags,omitempty"` // local flag names
}

func newCommandsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "commands",
		Short: "Read-only: list every command with its kind (read vs guarded-mutation), offline",
		Long: "Walk the command tree and list every runnable command: its path, one-line\n" +
			"description, local flags, and KIND — `guarded-mutation` for commands that\n" +
			"carry the standard --dry-run/--yes live-mutation gate, `read` otherwise.\n" +
			"Offline (no API call, no credentials) — the verb-level companion to\n" +
			"`secopsctl surfaces`, and the input for building per-command allowlists\n" +
			"for automation/agents.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rows := collectCommands(rootCmd, "")
			sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
			if jsonOut {
				return emitJSON(rows)
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "KIND\tJSON\tCOMMAND\tDESCRIPTION")
			for _, r := range rows {
				kind := ""
				if r.Kind == "guarded-mutation" {
					kind = "guarded"
				}
				js := "-"
				if r.JSON {
					js = "y"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", kind, js, r.Path, truncate(r.Short, 80))
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "\n%d command(s). `guarded` = live mutation behind --dry-run/--yes; blank = read.\n", len(rows))
			return nil
		},
	}
	return markJSON(cmd)
}

// collectCommands walks the tree depth-first and returns one row per runnable,
// visible command: every leaf, plus parents that do real work of their own
// (e.g. `info`); navigation-only group parents (whose RunE is the help-only
// one requireSubcommand injects), hidden commands, and cobra's built-ins are
// skipped.
func collectCommands(cmd *cobra.Command, prefix string) []commandRow {
	var rows []commandRow
	for _, c := range cmd.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		path := strings.TrimSpace(prefix + " " + c.Name())
		runnable := !c.HasSubCommands() ||
			((c.Run != nil || c.RunE != nil) && !helpOnlyParents[c])
		if runnable {
			rows = append(rows, commandRow{
				Path:  path,
				Kind:  commandKind(c),
				JSON:  c.Annotations[jsonAnnotation] == "true",
				Short: c.Short,
				Flags: localFlagNames(c),
			})
		}
		rows = append(rows, collectCommands(c, path)...)
	}
	return rows
}

// commandKind classifies a command: any command carrying the standard
// --dry-run + --yes pair is a guarded live mutation; everything else is a read
// (including offline utilities and local-file writers like `pull`).
func commandKind(c *cobra.Command) string {
	if c.Flags().Lookup("dry-run") != nil && c.Flags().Lookup("yes") != nil {
		return "guarded-mutation"
	}
	return "read"
}

// localFlagNames lists the command's own flag names (not inherited globals).
func localFlagNames(c *cobra.Command) []string {
	var names []string
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		names = append(names, f.Name)
	})
	sort.Strings(names)
	return names
}
