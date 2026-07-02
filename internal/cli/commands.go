package cli

import (
	"fmt"
	"os"
	"regexp"
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

// commandRow is one catalog entry (the --json element shape). The flag detail
// (type / default / required / enum) lets an agent build a correct invocation on
// the first try instead of inferring a flag from training data.
type commandRow struct {
	Path    string     `json:"path"`              // command path without the binary name
	Aliases []string   `json:"aliases,omitempty"` // back-compat alternate names for this command
	Kind    string     `json:"kind"`              // "read" | "guarded-mutation"
	JSON    bool       `json:"json"`              // honors the global --json flag
	Short   string     `json:"short"`             // one-line description
	Args    string     `json:"args,omitempty"`    // positional-arg spec (from Use)
	Example string     `json:"example,omitempty"` // a one-line invocation example
	Flags   []flagInfo `json:"flags,omitempty"`   // local flags, with type/required/enum
}

// flagInfo describes one local flag for the catalog.
type flagInfo struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"` // pflag value type: string|bool|int|stringSlice|…
	Default  string   `json:"default,omitempty"`
	Required bool     `json:"required,omitempty"`
	Enum     []string `json:"enum,omitempty"` // accepted values, parsed from the flag's help
	Usage    string   `json:"usage,omitempty"`
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
			rows := collectCommands(rootCmd)
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

// walkRunnable walks the tree depth-first and visits every runnable, visible
// command: every leaf, plus parents that do real work of their own (e.g.
// `info`); navigation-only group parents (whose RunE is the help-only one
// requireSubcommand injects), hidden commands, and cobra's built-ins are
// skipped. Shared by the `commands` catalog and `docs generate`.
func walkRunnable(cmd *cobra.Command, prefix string, visit func(path string, c *cobra.Command)) {
	for _, c := range cmd.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		path := strings.TrimSpace(prefix + " " + c.Name())
		runnable := !c.HasSubCommands() ||
			((c.Run != nil || c.RunE != nil) && !helpOnlyParents[c])
		if runnable {
			visit(path, c)
		}
		walkRunnable(c, path, visit)
	}
}

// collectCommands returns one catalog row per runnable command. Only runnable
// verbs are rows (an agent invokes every row). A navigation-only group is never
// a row even if it carries aliases — the old→new group-rename map lives in
// `capabilities --json` instead.
func collectCommands(cmd *cobra.Command) []commandRow {
	var rows []commandRow
	walkRunnable(cmd, "", func(path string, c *cobra.Command) {
		rows = append(rows, commandRow{
			Path:    path,
			Aliases: c.Aliases,
			Kind:    commandKind(c),
			JSON:    c.Annotations[jsonAnnotation] == "true",
			Short:   c.Short,
			Args:    positionalSpec(c),
			Example: strings.TrimSpace(c.Example),
			Flags:   localFlagInfos(c),
		})
	})
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

// localFlagInfos lists the command's own flags (not inherited globals) with the
// detail an agent needs to build a valid invocation: value type, default,
// whether the flag is required, and any accepted-value set parsed from the help.
func localFlagInfos(c *cobra.Command) []flagInfo {
	var infos []flagInfo
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		_, required := f.Annotations[cobra.BashCompOneRequiredFlag]
		infos = append(infos, flagInfo{
			Name:     f.Name,
			Type:     f.Value.Type(),
			Default:  f.DefValue,
			Required: required,
			Enum:     enumFromUsage(f.Usage),
			Usage:    f.Usage,
		})
	})
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos
}

// positionalSpec returns the positional-argument portion of a command's Use
// string (everything after the verb name), e.g. "update <alert-id> [<id>...]"
// → "<alert-id> [<id>...]". Empty when the command takes no positionals.
func positionalSpec(c *cobra.Command) string {
	use := strings.TrimSpace(c.Use)
	if i := strings.IndexAny(use, " \t"); i >= 0 {
		return strings.TrimSpace(use[i+1:])
	}
	return ""
}

// enumPattern matches a run of pipe-separated tokens, each ≥2 chars (so an "a|b"
// prose pipe never matches) — the idiomatic way these commands document an
// accepted-value set (e.g. "precise|broad", "malicious | not-malicious | …").
var enumPattern = regexp.MustCompile(`[A-Za-z][\w-]+(?:\s*\|\s*[A-Za-z][\w-]+)+`)

// placeholderRE matches an angle-bracket placeholder like "<step-name|id>" — a
// usage grammar token, not an accepted-value set, so its pipe must not be read as
// an enum.
var placeholderRE = regexp.MustCompile(`<[^>]*>`)

// enumFromUsage extracts an accepted-value list from a flag's help text, or nil.
// Angle-bracket placeholders are stripped first (their `|` is grammar, not an
// enum), then the first pipe-run of ≥2-char tokens is taken. Parsing the help
// keeps the catalog's enum in lock-step with it instead of a drift-prone registry.
func enumFromUsage(usage string) []string {
	run := enumPattern.FindString(placeholderRE.ReplaceAllString(usage, ""))
	if run == "" {
		return nil
	}
	parts := strings.Split(run, "|")
	vals := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			vals = append(vals, p)
		}
	}
	if len(vals) < 2 {
		return nil
	}
	return vals
}
