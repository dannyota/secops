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
	Annotations map[string]any `json:"annotations,omitempty"`
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
	readOnly := map[string]any{"readOnlyHint": true}
	return []mcpTool{
		{
			Name: "run",
			Description: "Escape hatch: run a raw secopsctl command string (no argument " +
				"validation). Prefer focus(<group>) for typed tools with validated schemas.",
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
			Annotations: readOnly,
		},
		{
			Name: "focus",
			Description: "Load typed tools for a command group — the preferred way to " +
				"execute commands. Each tool has validated, documented parameters. " +
				"Call help first to see groups, unfocus to free context.",
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
			Annotations: readOnly,
		},
		{
			Name: "usage",
			Description: "Show flags, args, and description for one command, and " +
				"auto-load it as a callable typed tool. No separate focus call needed.",
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
			Annotations: readOnly,
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
			Annotations: readOnly,
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
				"Siemplify SOAR). Start with `help` to see command groups, then " +
				"`focus <group>` to load typed tools — the preferred way to run " +
				"commands (validated arguments, full schemas). `usage <command>` " +
				"previews one command's schema and auto-loads it as a callable tool. " +
				"`run` is an escape hatch for raw command strings (no validation). " +
				"`unfocus` when done to free context. " +
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

	argv := mcpSplitArgs(cmd)
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
	return mcpText(id, string(out)+s.nudgeForRun(argv), false)
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

	// Auto-focus the resolved tool so the agent can call it directly without
	// a separate focus() call — makes the typed path strictly cheaper than run.
	loaded := false
	existing := s.focused["_usage"]
	alreadyLoaded := false
	for _, t := range existing {
		if t.Name == tool.Name {
			alreadyLoaded = true
			break
		}
	}
	if !alreadyLoaded {
		s.focused["_usage"] = append(existing, tool)
		s.notifyToolsChanged()
		loaded = true
	}

	b, _ := json.MarshalIndent(map[string]any{
		"name":        tool.Name,
		"description": tool.Description,
		"inputSchema": tool.InputSchema,
	}, "", "  ")
	suffix := ""
	if loaded {
		suffix = "\n\nloaded — callable as mcp__secopsctl__" + tool.Name
	}
	return mcpText(id, string(b)+suffix, false)
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
	return mcpText(id, fmt.Sprintf("focused %s: loaded %d tools (callable as mcp__<server>__<name>)\n%s",
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

// nudgeForRun returns a one-line tip when the run command matches a typed tool
// that isn't currently focused — steering the agent toward focus/usage.
func (s *mcpSession) nudgeForRun(argv []string) string {
	var parts []string
	for _, a := range argv {
		if strings.HasPrefix(a, "-") {
			break
		}
		parts = append(parts, strings.ReplaceAll(a, "-", "_"))
	}
	for end := len(parts); end > 0; end-- {
		name := strings.Join(parts[:end], "_")
		if _, ok := s.toolIndex[name]; ok {
			if s.isFocused(name) {
				return ""
			}
			group := strings.ReplaceAll(parts[0], "_", "-")
			return fmt.Sprintf("\ntip: typed tool available — focus(\"%s\") loads validated schemas", group)
		}
	}
	return ""
}

func (s *mcpSession) isFocused(toolName string) bool {
	for _, tools := range s.focused {
		for _, t := range tools {
			if t.Name == toolName {
				return true
			}
		}
	}
	return false
}
