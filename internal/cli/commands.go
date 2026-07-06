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
		Use:   "commands [group]",
		Short: "Read-only: list every command with its kind (read vs guarded-mutation), offline. Pass a group name to drill in for flags and args",
		Long: "Walk the command tree and list every runnable command: its path, one-line\n" +
			"description, local flags, and KIND — `guarded-mutation` for commands that\n" +
			"carry the standard --dry-run/--yes live-mutation gate, `read` otherwise.\n\n" +
			"Without arguments: grouped catalog (compact). With a group name: detail\n" +
			"for that group's commands (flags, args).\n\n" +
			"Offline (no API call, no credentials) — the verb-level companion to\n" +
			"`secopsctl surfaces`, and the input for building per-command allowlists\n" +
			"for automation/agents.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rows := collectCommands(rootCmd)
			sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })

			// Drill into one group: commands <group>
			if len(args) == 1 {
				return commandsGroup(args[0], rows)
			}
			// Default: grouped catalog
			groups := collectGroups(rootCmd)
			return commandsGrouped(groups, rows)
		},
	}
	return markJSON(cmd)
}

// commandGroup holds a top-level group parent's metadata.
type commandGroup struct {
	Name  string `json:"name"`
	Short string `json:"short"`
}

// collectGroups returns the top-level group parents (alerts, cases, …) with
// their Short descriptions. Standalone root commands (doctor, pull, …) are
// included as single-command groups.
func collectGroups(root *cobra.Command) []commandGroup {
	var groups []commandGroup
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		groups = append(groups, commandGroup{Name: c.Name(), Short: c.Short})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return groups
}

// commandsGrouped prints the compact grouped catalog.
func commandsGrouped(groups []commandGroup, rows []commandRow) error {
	// Index rows by top-level group.
	byGroup := map[string][]commandRow{}
	for _, r := range rows {
		top, _, _ := strings.Cut(r.Path, " ")
		byGroup[top] = append(byGroup[top], r)
	}

	type groupEntry struct {
		Name    string `json:"name"`
		Short   string `json:"short"`
		Total   int    `json:"total"`
		Guarded int    `json:"guarded"`
	}

	var entries []groupEntry
	for _, g := range groups {
		gRows := byGroup[g.Name]
		if len(gRows) == 0 {
			continue
		}
		guarded := 0
		for _, r := range gRows {
			if r.Kind == "guarded-mutation" {
				guarded++
			}
		}
		entries = append(entries, groupEntry{
			Name:    g.Name,
			Short:   g.Short,
			Total:   len(gRows),
			Guarded: guarded,
		})
	}

	if jsonOut {
		return emitJSON(entries)
	}

	fmt.Fprintf(os.Stdout, "secopsctl (%d commands, use `commands <group>` for detail)\n\n", len(rows))
	for _, e := range entries {
		guarded := ""
		if e.Guarded > 0 {
			guarded = fmt.Sprintf(", %d guarded", e.Guarded)
		}
		fmt.Fprintf(os.Stdout, "%-16s %2d cmds%-16s %s\n", e.Name, e.Total, guarded, truncate(e.Short, 60))
	}
	return nil
}

// commandsGroup prints detail for one group's commands. The group arg can be
// a top-level name ("soar") or a dotted sub-group ("soar.push") for nested
// drill-down. Groups with sub-groups automatically show a sub-group catalog
// instead of a flat command list.
func commandsGroup(group string, rows []commandRow) error {
	// Normalize "soar.push" → prefix "soar push ".
	prefix := strings.ReplaceAll(group, ".", " ")

	var filtered []commandRow
	for _, r := range rows {
		if r.Path == prefix || strings.HasPrefix(r.Path, prefix+" ") {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) == 0 {
		var names []string
		for _, c := range rootCmd.Commands() {
			if !c.Hidden && c.Name() != "help" && c.Name() != "completion" {
				names = append(names, c.Name())
			}
		}
		return fmt.Errorf("no commands found in group %q; valid groups: %s", group, strings.Join(names, ", "))
	}

	// Check if this group has sub-groups (commands with further nesting).
	depth := len(strings.Fields(prefix))
	subs := subGroups(filtered, depth)
	if len(subs) > 1 {
		if jsonOut {
			return emitJSON(subs)
		}
		return subGroupsTable(prefix, subs)
	}

	if jsonOut {
		return emitJSON(compactRows(filtered))
	}
	return commandsFlat(filtered)
}

// subGroupEntry is a mini-catalog entry for a nested sub-group.
type subGroupEntry struct {
	Name    string `json:"name"`
	Short   string `json:"short"`
	Total   int    `json:"total"`
	Guarded int    `json:"guarded"`
}

// subGroups detects whether commands at the given depth have a shared
// sub-prefix (e.g. "soar push *", "soar jobs *") and returns sub-group
// entries. Returns nil if all commands are at the same depth (no nesting).
func subGroups(rows []commandRow, depth int) []subGroupEntry {
	type info struct {
		total, guarded int
	}
	groups := map[string]*info{}
	var order []string

	for _, r := range rows {
		parts := strings.Fields(r.Path)
		if len(parts) <= depth {
			continue // root-level command in this group (e.g. "soar pull")
		}
		sub := parts[depth]
		if len(parts) == depth+1 {
			// Leaf at this depth — not a sub-group, just a command.
			// Use empty-string key to count ungrouped leaves.
			sub = ""
		}
		g, ok := groups[sub]
		if !ok {
			g = &info{}
			groups[sub] = g
			order = append(order, sub)
		}
		g.total++
		if r.Kind == "guarded-mutation" {
			g.guarded++
		}
	}

	// If there's only one sub-group (or none), don't nest — show flat.
	realGroups := 0
	for k, g := range groups {
		if k != "" && g.total > 1 {
			realGroups++
		}
	}
	if realGroups <= 1 {
		return nil
	}

	// Resolve Short descriptions from the cobra tree.
	prefix := strings.Join(strings.Fields(rows[0].Path)[:depth], " ")
	parentCmd := findCobraCommand(prefix)

	var entries []subGroupEntry
	for _, sub := range order {
		g := groups[sub]
		if sub == "" {
			// Ungrouped leaf commands — list each individually.
			for _, r := range rows {
				parts := strings.Fields(r.Path)
				if len(parts) == depth+1 {
					guarded := 0
					if r.Kind == "guarded-mutation" {
						guarded = 1
					}
					entries = append(entries, subGroupEntry{
						Name:    parts[depth],
						Short:   r.Short,
						Total:   1,
						Guarded: guarded,
					})
				}
			}
			continue
		}
		short := ""
		if parentCmd != nil {
			for _, ch := range parentCmd.Commands() {
				if ch.Name() == sub {
					short = ch.Short
					break
				}
			}
		}
		entries = append(entries, subGroupEntry{
			Name:    sub,
			Short:   short,
			Total:   g.total,
			Guarded: g.guarded,
		})
	}
	return entries
}

// findCobraCommand resolves a space-separated command path to its cobra node.
func findCobraCommand(path string) *cobra.Command {
	cmd := rootCmd
	for seg := range strings.FieldsSeq(path) {
		found := false
		for _, ch := range cmd.Commands() {
			if ch.Name() == seg {
				cmd = ch
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return cmd
}

// subGroupsTable prints a sub-group catalog in table form.
func subGroupsTable(prefix string, entries []subGroupEntry) error {
	fmt.Fprintf(os.Stdout, "%s (use `commands %s.<sub>` for detail)\n\n",
		prefix, strings.ReplaceAll(prefix, " ", "."))
	for _, e := range entries {
		guarded := ""
		if e.Guarded > 0 {
			guarded = fmt.Sprintf(", %d guarded", e.Guarded)
		}
		fmt.Fprintf(os.Stdout, "%-16s %2d cmds%-16s %s\n",
			e.Name, e.Total, guarded, truncate(e.Short, 60))
	}
	return nil
}

// compactRow is the drill-down JSON shape: path, kind, description, and
// positional args only. Flag detail is available via the `usage` meta-tool,
// so the drill-down stays small even for large groups like cases (51 cmds).
type compactRow struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Short string `json:"short"`
	Args  string `json:"args,omitempty"`
}

func compactRows(rows []commandRow) []compactRow {
	out := make([]compactRow, len(rows))
	for i, r := range rows {
		out[i] = compactRow{
			Path:  r.Path,
			Kind:  r.Kind,
			Short: r.Short,
			Args:  r.Args,
		}
	}
	return out
}

// commandsFlat prints the tabular per-command listing (the legacy format).
func commandsFlat(rows []commandRow) error {
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
//
// Flag-like tokens (--flag, [--flag ...], (--a | --b)) are stripped because
// cobra Use strings often document both positional args and required flags in
// the same line — the flags are already exposed as individual schema properties
// via localFlagInfos. After stripping, if nothing remains the command is
// flag-only and callers should not emit a positional-args field.
func positionalSpec(c *cobra.Command) string {
	use := strings.TrimSpace(c.Use)
	i := strings.IndexAny(use, " \t")
	if i < 0 {
		return ""
	}
	return stripFlagHints(strings.TrimSpace(use[i+1:]))
}

// stripFlagHints removes flag-like patterns from a Use-string tail, keeping
// only genuine positional-arg tokens (<placeholder>, bare-word args, ...).
//
// Patterns removed:
//   - Bracketed/parenthesized groups containing "--": [--flag ...], (--a | --b)
//   - Bare --flag tokens and their inline values
//   - Residual pipes, ellipses, and bracket-only tokens like [flags]
func stripFlagHints(spec string) string {
	// Pass 1: remove balanced [...] and (...) groups that contain "--".
	spec = stripFlagGroups(spec)

	// Pass 2: remove bare --flag tokens and their values.
	spec = bareFlagRE.ReplaceAllString(spec, "")

	// Pass 3: clean residue — [flags], empty brackets.
	spec = residueRE.ReplaceAllString(spec, "")

	// Pass 4: remove stray pipes and ellipses that are NOT inside <...>.
	spec = stripStrayPunctuation(spec)

	return strings.TrimSpace(strings.Join(strings.Fields(spec), " "))
}

// stripStrayPunctuation removes bare |, ..., and … that sit outside angle
// brackets and square brackets (inside those they are part of the grammar).
func stripStrayPunctuation(s string) string {
	var out []byte
	inAngle, inBracket := 0, 0
	i := 0
	for i < len(s) {
		protected := inAngle > 0 || inBracket > 0
		switch {
		case s[i] == '<':
			inAngle++
			out = append(out, s[i])
		case s[i] == '>':
			if inAngle > 0 {
				inAngle--
			}
			out = append(out, s[i])
		case s[i] == '[':
			inBracket++
			out = append(out, s[i])
		case s[i] == ']':
			if inBracket > 0 {
				inBracket--
			}
			out = append(out, s[i])
		case protected:
			out = append(out, s[i])
		case s[i] == '|':
			// skip stray pipe
		case s[i] == '.' && i+2 < len(s) && s[i+1] == '.' && s[i+2] == '.':
			// keep ... only after ] or > (variadic marker)
			if len(out) > 0 && (out[len(out)-1] == ']' || out[len(out)-1] == '>') {
				out = append(out, s[i:i+3]...)
			}
			i += 3
			continue
		case s[i] == '\xe2' && i+2 < len(s) && s[i+1] == '\x80' && s[i+2] == '\xa6':
			i += 3
			continue
		default:
			out = append(out, s[i])
		}
		i++
	}
	return string(out)
}

// stripFlagGroups removes balanced [...] and (...) groups that contain "--".
func stripFlagGroups(s string) string {
	var out []byte
	i := 0
	for i < len(s) {
		if s[i] == '[' || s[i] == '(' {
			close := closerFor(s[i])
			end := findBalancedClose(s, i, s[i], close)
			group := s[i : end+1]
			// Also consume trailing dots/ellipsis.
			trail := end + 1
			for trail < len(s) && (s[trail] == '.' || s[trail] == '\xe2') { // \xe2 = start of … (U+2026)
				if s[trail] == '\xe2' && trail+2 < len(s) {
					trail += 3 // UTF-8 three-byte sequence
				} else {
					trail++
				}
			}
			if strings.Contains(group, "--") {
				i = trail
				continue
			}
			// Non-flag group: keep it including any trailing dots.
			out = append(out, s[i:trail]...)
			i = trail
		} else {
			out = append(out, s[i])
			i++
		}
	}
	return string(out)
}

func closerFor(open byte) byte {
	if open == '[' {
		return ']'
	}
	return ')'
}

func findBalancedClose(s string, start int, open, close byte) int {
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(s) - 1
}

// bareFlagRE matches a --flag token, optional |alt or =val suffix, and any
// inline flag-value arguments (bare words, <placeholders>, key=value, or
// pipe-separated alternatives like precise|broad).
var bareFlagRE = regexp.MustCompile(`--[\w-]+(?:[|=]\S+)?(?:\s+(?:<[^>]+>(?:=<[^>]+>|=\S+)?|[A-Za-z0-9_.,']+(?:[|][A-Za-z0-9_.]+)*(?:=<[^>]+>|=\S+)?))*`)

// residueRE cleans up leftover noise: bracket-only tokens like [flags] and
// empty bracket/paren pairs. Stray pipes and ellipses outside angle brackets
// are handled by stripResidue.
var residueRE = regexp.MustCompile(`\[flags\]|\[\s*\]|\(\s*\)`)

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
