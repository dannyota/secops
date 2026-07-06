package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// JSON-RPC 2.0 message types for the MCP stdio transport.
// Each message is one JSON object per line on stdin/stdout.

type jrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jrpcError      `json:"error,omitempty"`
}

type jrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP protocol types.

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

func newMCPServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run secopsctl as an MCP server (stdio JSON-RPC transport)",
		Long: "Start a Model Context Protocol server over stdin/stdout. Every\n" +
			"secopsctl command becomes an MCP tool; the docs/tips guides become\n" +
			"MCP resources. Designed for Claude Code and other MCP-aware agents.\n\n" +
			"Register with: secopsctl mcp install",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runMCPServe()
		},
	}
}

type mcpSession struct {
	allTools  []mcpTool
	toolIndex map[string]mcpTool
	metaTools []mcpTool
	focused   map[string][]mcpTool
	resources []mcpResource
	resCont   map[string]string
	enc       *json.Encoder
}

func runMCPServe() error {
	s := newMCPSession()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req jrpcRequest
		if json.Unmarshal(line, &req) != nil {
			continue
		}
		if req.ID == nil {
			continue
		}
		resp := s.dispatch(req)
		if err := s.enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func newMCPSession() *mcpSession {
	allTools := mcpToolsFromCobra()
	resources, resCont := mcpResourcesFromTips()

	toolIndex := make(map[string]mcpTool, len(allTools))
	for _, t := range allTools {
		toolIndex[t.Name] = t
	}

	s := &mcpSession{
		allTools:  allTools,
		toolIndex: toolIndex,
		focused:   make(map[string][]mcpTool),
		resources: resources,
		resCont:   resCont,
		enc:       json.NewEncoder(os.Stdout),
	}
	s.metaTools = s.buildMetaTools()
	return s
}

func (s *mcpSession) buildMetaTools() []mcpTool {
	return []mcpTool{
		{
			Name: "run",
			Description: "Run any secopsctl command. Pass the full command without the " +
				"'secopsctl' prefix.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "the command to run (e.g. 'cases list --limit 5')",
					},
				},
				"required": []string{"command"},
			},
		},
		{
			Name: "help",
			Description: "List command groups, or subcommands within a group. " +
				"Call without args for the full catalog, or pass a group name to drill in.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group": map[string]any{
						"type":        "string",
						"description": "group name (e.g. 'cases'). Omit to list all groups.",
					},
				},
			},
		},
		{
			Name: "focus",
			Description: "Load typed tools for a command group (full schemas with flags). " +
				"Call help first to see groups.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group": map[string]any{
						"type":        "string",
						"description": "group to load (e.g. 'cases', 'rules', 'dashboards')",
					},
				},
				"required": []string{"group"},
			},
		},
		{
			Name: "usage",
			Description: "Show flags, args, and description for one command. " +
				"Use before `run` to learn a command's interface.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "command path (e.g. 'cases list', 'search udm', 'rules test')",
					},
				},
				"required": []string{"command"},
			},
		},
		{
			Name: "unfocus",
			Description: "Unload a command group's typed tools to free context. " +
				"Omit group to unload all.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group": map[string]any{
						"type":        "string",
						"description": "group to unload. Omit to unload all focused groups.",
					},
				},
			},
		},
	}
}

func (s *mcpSession) visibleTools() []mcpTool {
	var out []mcpTool
	out = append(out, s.metaTools...)
	for _, tools := range s.focused {
		out = append(out, tools...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *mcpSession) notifyToolsChanged() {
	_ = s.enc.Encode(map[string]string{
		"jsonrpc": "2.0",
		"method":  "notifications/tools/list_changed",
	})
}

func (s *mcpSession) dispatch(req jrpcRequest) jrpcResponse {
	ok := func(result any) jrpcResponse {
		return jrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	}

	switch req.Method {
	case "initialize":
		return ok(map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools":     map[string]any{"listChanged": true},
				"resources": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "secopsctl",
				"version": resolveBuildInfo().Version,
			},
			"instructions": "secopsctl is a CLI for Google SecOps (Chronicle SIEM + " +
				"Siemplify SOAR). Start with `help` to see command groups. " +
				"Use `usage <command>` to see flags and args for a specific command, " +
				"then `run` to execute it. Or `focus` a group to load all its typed tools. " +
				"Mutations are guarded: pass yes=true to apply (dry-run by default).",
		})

	case "ping":
		return ok(map[string]any{})

	case "tools/list":
		return ok(map[string]any{"tools": s.visibleTools()})

	case "tools/call":
		return s.handleToolCall(req)

	case "resources/list":
		return ok(map[string]any{"resources": s.resources})

	case "resources/read":
		return s.handleResourceRead(req)

	default:
		return jrpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &jrpcError{Code: -32601, Message: "method not found: " + req.Method},
		}
	}
}

func (s *mcpSession) handleToolCall(req jrpcRequest) jrpcResponse {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	_ = json.Unmarshal(req.Params, &params)

	text := func(str string, isErr bool) jrpcResponse {
		return jrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"content": []map[string]string{{"type": "text", "text": str}},
			"isError": isErr,
		}}
	}

	switch params.Name {
	case "run":
		return s.handleRun(req.ID, params.Arguments)
	case "help":
		return s.handleHelp(req.ID, params.Arguments)
	case "usage":
		return s.handleUsage(req.ID, params.Arguments)
	case "focus":
		return s.handleFocus(req.ID, params.Arguments)
	case "unfocus":
		return s.handleUnfocus(req.ID, params.Arguments)
	default:
		if _, ok := s.toolIndex[params.Name]; !ok {
			return text("unknown tool: "+params.Name+
				". Use 'help' to discover commands, or 'focus' to load typed tools.", true)
		}
		out, err := mcpExecTool(params.Name, params.Arguments)
		if err != nil {
			return text(err.Error(), true)
		}
		return text(out, false)
	}
}

func mcpText(id json.RawMessage, str string, isErr bool) jrpcResponse {
	return jrpcResponse{JSONRPC: "2.0", ID: id, Result: map[string]any{
		"content": []map[string]string{{"type": "text", "text": str}},
		"isError": isErr,
	}}
}

func (s *mcpSession) handleRun(id json.RawMessage, args map[string]any) jrpcResponse {
	cmd, _ := args["command"].(string)
	if cmd == "" {
		return mcpText(id, "missing required parameter: command", true)
	}

	argv := strings.Fields(cmd)
	if len(argv) == 0 {
		return mcpText(id, "empty command", true)
	}

	if !mcpHasFlag(args, "json") && !mcpHasFlag(args, "format") && !mcpHasFlag(args, "output") {
		hasJSON := false
		for _, a := range argv {
			if a == "--json" || a == "--format" || a == "--output" {
				hasJSON = true
				break
			}
		}
		if !hasJSON {
			argv = append(argv, "--json")
		}
	}

	argv = append(argv, mcpGlobalFlags(nil)...)

	self, err := os.Executable()
	if err != nil {
		return mcpText(id, fmt.Sprintf("cannot find secopsctl binary: %v", err), true)
	}

	c := exec.Command(self, argv...) //nolint:gosec // self is os.Executable, argv from agent input
	c.Env = os.Environ()
	out, err := c.CombinedOutput()
	if err != nil && len(out) > 0 {
		return mcpText(id, string(out), true)
	}
	if err != nil {
		return mcpText(id, err.Error(), true)
	}
	return mcpText(id, string(out), false)
}

func (s *mcpSession) handleHelp(id json.RawMessage, args map[string]any) jrpcResponse {
	group, _ := args["group"].(string)
	execArgs := map[string]any{}
	if group != "" {
		execArgs["args"] = group
	}
	out, err := mcpExecTool("commands", execArgs)
	if err != nil {
		return mcpText(id, err.Error(), true)
	}
	return mcpText(id, out, false)
}

func (s *mcpSession) handleUsage(id json.RawMessage, args map[string]any) jrpcResponse {
	cmd, _ := args["command"].(string)
	if cmd == "" {
		return mcpText(id, "missing required parameter: command", true)
	}

	toolName := strings.ReplaceAll(strings.ReplaceAll(cmd, " ", "_"), "-", "_")

	// Try exact match first, then resolve via the command path.
	tool, ok := s.toolIndex[toolName]
	if !ok {
		resolved := mcpResolveCommandPath(strings.Split(toolName, "_"))
		resolvedName := strings.Join(resolved, "_")
		tool, ok = s.toolIndex[resolvedName]
	}
	if !ok {
		return mcpText(id, "unknown command: "+cmd+
			". Use 'help' to see available commands.", true)
	}

	b, _ := json.MarshalIndent(map[string]any{
		"name":        tool.Name,
		"description": tool.Description,
		"inputSchema": tool.InputSchema,
	}, "", "  ")
	return mcpText(id, string(b), false)
}

func (s *mcpSession) handleFocus(id json.RawMessage, args map[string]any) jrpcResponse {
	group, _ := args["group"].(string)
	if group == "" {
		return mcpText(id, "missing required parameter: group", true)
	}

	// Filter allTools to those belonging to this group (prefix match on
	// underscore-separated tool names, matching the cobra group name which
	// may contain hyphens).
	prefix := strings.ReplaceAll(group, "-", "_") + "_"
	var tools []mcpTool
	for _, t := range s.allTools {
		if t.Name == group || strings.HasPrefix(t.Name, prefix) {
			tools = append(tools, t)
		}
	}

	// Also try with the group name as-is (handles e.g. "content_hub" input).
	if len(tools) == 0 {
		altPrefix := group + "_"
		for _, t := range s.allTools {
			if t.Name == group || strings.HasPrefix(t.Name, altPrefix) {
				tools = append(tools, t)
			}
		}
	}

	if len(tools) == 0 {
		return mcpText(id, "no tools found for group "+group+
			". Use 'help' to see available groups.", true)
	}

	s.focused[group] = tools
	s.notifyToolsChanged()

	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return mcpText(id, fmt.Sprintf("focused %s: loaded %d tools\n%s",
		group, len(tools), strings.Join(names, "\n")), false)
}

func (s *mcpSession) handleUnfocus(id json.RawMessage, args map[string]any) jrpcResponse {
	group, _ := args["group"].(string)
	if group == "" {
		count := len(s.focused)
		s.focused = make(map[string][]mcpTool)
		if count > 0 {
			s.notifyToolsChanged()
		}
		return mcpText(id, fmt.Sprintf("unfocused all (%d groups)", count), false)
	}

	if _, ok := s.focused[group]; !ok {
		return mcpText(id, "group "+group+" is not focused", true)
	}
	delete(s.focused, group)
	s.notifyToolsChanged()
	return mcpText(id, "unfocused "+group, false)
}

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
	if commandKind(c) == "guarded-mutation" {
		desc += " [guarded: dry-run by default, pass yes=true to apply]"
	}

	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return mcpTool{Name: name, Description: desc, InputSchema: schema}
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

	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot find secopsctl binary: %w", err)
	}

	cmd := exec.Command(self, argv...) //nolint:gosec // self is os.Executable, argv from validated tool schema
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) > 0 {
		return "", fmt.Errorf("%s", out)
	}
	if err != nil {
		return "", err
	}
	return string(out), nil
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

// mcpGlobalFlags returns the global flags that should be forwarded from the
// parent MCP serve process to a tool subprocess, skipping any the caller
// already set explicitly.
func mcpGlobalFlags(callerArgs map[string]any) []string {
	var flags []string
	fwd := func(name string, val bool) {
		if val && !mcpHasFlag(callerArgs, name) {
			flags = append(flags, "--"+name)
		}
	}
	fwd("read-only", readOnlyMode())
	fwd("legacy", forceLegacy)
	fwd("non-interactive", true) // subprocesses should never prompt
	fwd("no-progress", true)     // no TTY inside MCP
	if cfgFile != "" && !mcpHasFlag(callerArgs, "config") {
		flags = append(flags, "--config", cfgFile)
	}
	if rootCmd.PersistentFlags().Changed("timeout") && !mcpHasFlag(callerArgs, "timeout") {
		flags = append(flags, "--timeout", requestTimeout.String())
	}
	return flags
}

func mcpHasFlag(args map[string]any, name string) bool {
	if _, ok := args[name]; ok {
		return true
	}
	_, ok := args[strings.ReplaceAll(name, "-", "_")]
	return ok
}
