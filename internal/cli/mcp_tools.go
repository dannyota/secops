package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// --- Tool generation from the cobra command tree ---

func mcpToolsFromCobra() []mcpTool {
	var out []mcpTool
	walkRunnable(rootCmd, "", func(path string, c *cobra.Command) {
		top, _, _ := strings.Cut(path, " ")
		switch top {
		case "mcp", "completion", "docs":
			return
		}
		out = append(out, cobraToMCPTool(path, c))
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func cobraToMCPTool(path string, c *cobra.Command) mcpTool {
	name := strings.ReplaceAll(strings.ReplaceAll(path, " ", "_"), "-", "_")

	props := map[string]any{}
	var required []string

	if spec := positionalSpec(c); spec != "" {
		props["args"] = map[string]any{
			"type":        "string",
			"description": "positional arguments: " + spec,
		}
		required = append(required, "args")
	}

	for _, f := range localFlagInfos(c) {
		props[strings.ReplaceAll(f.Name, "-", "_")] = flagSchemaProperty(f)
		if f.Required {
			required = append(required, strings.ReplaceAll(f.Name, "-", "_"))
		}
	}

	desc := c.Short
	kind := commandKind(c)
	if kind == "guarded-mutation" {
		desc += " [guarded: dry-run by default, pass yes=true to apply]"
	}

	annotations := map[string]any{"title": path}
	if kind == "read" {
		annotations["readOnlyHint"] = true
	} else {
		annotations["destructiveHint"] = true
	}

	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return mcpTool{Name: name, Description: desc, InputSchema: schema, Annotations: annotations}
}

func flagSchemaProperty(f flagInfo) map[string]any {
	prop := map[string]any{"description": f.Usage}
	switch f.Type {
	case "bool":
		prop["type"] = "boolean"
	case "int", "int32", "int64", "uint", "uint32", "uint64":
		prop["type"] = "integer"
	case "float32", "float64":
		prop["type"] = "number"
	case "stringSlice", "stringArray":
		prop["type"] = "array"
		prop["items"] = map[string]any{"type": "string"}
	default:
		prop["type"] = "string"
	}
	if f.Default != "" && f.Default != "false" && f.Default != "0" && f.Default != "[]" {
		prop["default"] = f.Default
	}
	if len(f.Enum) > 0 {
		prop["enum"] = f.Enum
	}
	return prop
}

// --- Tool execution via subprocess ---

func mcpExecTool(name string, args map[string]any) (string, error) {
	parts := strings.Split(name, "_")
	var argv []string

	// Restore hyphenated command names: content_hub → content-hub.
	// The command tree uses hyphens; tool names use underscores as separators.
	// Walk the cobra tree to resolve the actual command path.
	argv = mcpResolveCommandPath(parts)

	if positional, ok := args["args"]; ok {
		if s, ok := positional.(string); ok && s != "" {
			argv = append(argv, strings.Fields(s)...)
		}
	}

	mcpStripSecurityFlags(args)

	for k, v := range args {
		if k == "args" {
			continue
		}
		flag := "--" + strings.ReplaceAll(k, "_", "-")
		switch val := v.(type) {
		case bool:
			if val {
				argv = append(argv, flag)
			}
		case float64:
			if val == float64(int(val)) {
				argv = append(argv, flag, fmt.Sprintf("%d", int(val)))
			} else {
				argv = append(argv, flag, fmt.Sprintf("%g", val))
			}
		case string:
			if val != "" {
				argv = append(argv, flag, val)
			}
		case []any:
			for _, item := range val {
				argv = append(argv, flag, fmt.Sprint(item))
			}
		default:
			argv = append(argv, flag, fmt.Sprint(val))
		}
	}

	// Force JSON output unless the caller already set a format.
	if !mcpHasFlag(args, "json") && !mcpHasFlag(args, "format") && !mcpHasFlag(args, "output") {
		argv = append(argv, "--json")
	}

	// Forward global flags from the parent MCP session so the subprocess
	// inherits --read-only, --config, --timeout, etc.
	argv = append(argv, mcpGlobalFlags(args)...)

	out, _, err := mcpRunSelf(argv)
	return out, err
}

// presortCommandTree visits every node (hidden subtrees included) so cobra's
// lazy in-place child sort runs once up front. mcpResolveCommandPath calls
// Commands() from concurrent tool-call goroutines; without the presort, two
// goroutines reaching a not-yet-visited node could sort the same slice at once.
func presortCommandTree(c *cobra.Command) {
	for _, child := range c.Commands() {
		presortCommandTree(child)
	}
}

// mcpResolveCommandPath maps underscore-separated tool-name segments back to
// the hyphenated cobra command path. It walks rootCmd's children greedily:
// ["content", "hub", "browse"] → finds "content-hub" child → then "browse".
func mcpResolveCommandPath(segments []string) []string {
	var resolved []string
	cmd := rootCmd
	i := 0
	for i < len(segments) {
		found := false
		// Try longest match first: "content-hub" before "content".
		for end := min(len(segments), i+3); end > i; end-- {
			candidate := strings.Join(segments[i:end], "-")
			for _, child := range cmd.Commands() {
				if child.Name() == candidate {
					resolved = append(resolved, candidate)
					cmd = child
					i = end
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			resolved = append(resolved, segments[i])
			i++
		}
	}
	return resolved
}

// mcpSplitArgs splits a command string into tokens, respecting double and
// single quotes (like a minimal POSIX shell). Unquoted tokens split on
// whitespace; quoted spans preserve interior whitespace and are stripped
// of the outer quotes.
func mcpSplitArgs(s string) []string {
	var args []string
	var cur []byte
	inSingle, inDouble := false, false
	for _, c := range []byte(s) {
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case (c == ' ' || c == '\t') && !inSingle && !inDouble:
			if len(cur) > 0 {
				args = append(args, string(cur))
				cur = cur[:0]
			}
		default:
			cur = append(cur, c)
		}
	}
	if len(cur) > 0 {
		args = append(args, string(cur))
	}
	return args
}

// mcpGlobalFlags returns the global flags that should be forwarded from the
// parent MCP serve process to a tool subprocess. Security-critical flags
// (--read-only, --non-interactive, --no-progress) are forwarded unconditionally
// when the parent has them set — a caller cannot suppress them via arguments.
func mcpGlobalFlags(callerArgs map[string]any) []string {
	var flags []string

	// Security-critical: forward unconditionally, ignore caller overrides.
	if readOnlyMode() {
		flags = append(flags, "--read-only")
	}
	flags = append(flags, "--non-interactive") // subprocesses must never prompt
	flags = append(flags, "--no-progress")     // no TTY inside MCP

	// Non-security flags: skip if the caller already set them.
	if forceLegacy && !mcpHasFlag(callerArgs, "legacy") {
		flags = append(flags, "--legacy")
	}
	if cfgFile != "" && !mcpHasFlag(callerArgs, "config") {
		flags = append(flags, "--config", cfgFile)
	}
	if rootCmd.PersistentFlags().Changed("timeout") && !mcpHasFlag(callerArgs, "timeout") {
		flags = append(flags, "--timeout", requestTimeout.String())
	}
	return flags
}

// mcpStripSecurityFlags removes security-critical flag keys from caller
// arguments so they cannot contradict the unconditionally-forwarded flags.
func mcpStripSecurityFlags(args map[string]any) {
	for _, name := range []string{
		"read-only", "read_only",
		"non-interactive", "non_interactive",
		"no-progress", "no_progress",
	} {
		delete(args, name)
	}
}

func mcpHasFlag(args map[string]any, name string) bool {
	if _, ok := args[name]; ok {
		return true
	}
	_, ok := args[strings.ReplaceAll(name, "-", "_")]
	return ok
}

// argvHasOutputFlag reports whether argv already carries an output-format
// flag, in either "--flag value" or "--flag=value" form. --json and --output
// are mutually exclusive at the root command, so a missed detection would
// append --json next to an explicit --output=… and fail the subprocess.
func argvHasOutputFlag(argv []string) bool {
	for _, a := range argv {
		for _, f := range [...]string{"--json", "--format", "--output"} {
			if a == f || strings.HasPrefix(a, f+"=") {
				return true
			}
		}
	}
	return false
}
