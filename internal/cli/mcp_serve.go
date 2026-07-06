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

	"danny.vn/secops/docs/tips"
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

// mcpSession holds the runtime state for one MCP server session, including
// the progressive tool-disclosure state (which categories have been expanded).
type mcpSession struct {
	allTools   []mcpTool            // every tool from the cobra tree
	toolIndex  map[string]mcpTool   // name → tool (all tools, for execution)
	categories map[string]*mcpGroup // top-level group → category info
	expanded   map[string]bool      // which categories have been expanded
	resources  []mcpResource
	resCont    map[string]string // resource URI → content
	enc        *json.Encoder
}

// mcpGroup represents a top-level command group exposed as a single category
// tool in the initial tools/list.
type mcpGroup struct {
	router   mcpTool   // the category router tool
	children []mcpTool // the sub-tools (tier 2), registered on first use
}

// standaloneGroups are top-level groups with ≤1 command — they stay as flat
// tools in the initial listing (tier 0). Groups not in this set become
// category routers.
var standaloneGroups = map[string]bool{
	"audit": true, "cleanup": true, "commands": true, "config": true,
	"data-tables": true, "doctor": true, "drift": true, "mitre": true,
	"pull": true, "push": true, "version": true,
}

// promotedTools are specific tools from multi-command groups that are useful
// enough to appear in the initial listing alongside standalone groups and
// category routers (tier 0 promoted).
var promotedTools = map[string]bool{
	"search_udm":          true,
	"search_stats":        true,
	"search_raw":          true,
	"gemini_ask":          true,
	"gemini_search":       true,
	"status_capabilities": true,
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
		allTools:   allTools,
		toolIndex:  toolIndex,
		categories: make(map[string]*mcpGroup),
		expanded:   make(map[string]bool),
		resources:  resources,
		resCont:    resCont,
		enc:        json.NewEncoder(os.Stdout),
	}
	s.buildCategories()
	return s
}

// mcpToolGroup returns the top-level group from a tool name by splitting on
// the first underscore. Hyphenated cobra names (content-hub, data-access) are
// preserved as-is because cobraToMCPTool only replaces spaces with underscores.
func mcpToolGroup(name string) string {
	if i := strings.Index(name, "_"); i > 0 {
		return name[:i]
	}
	return name
}

// buildCategories partitions the flat tool list into standalone (tier 0),
// promoted (tier 0), and category groups (tier 1 router + tier 2 children).
func (s *mcpSession) buildCategories() {
	grouped := map[string][]mcpTool{} // top-level group → tools
	for _, t := range s.allTools {
		grouped[mcpToolGroup(t.Name)] = append(grouped[mcpToolGroup(t.Name)], t)
	}

	for group, tools := range grouped {
		cobraGroup := strings.ReplaceAll(group, "_", "-")
		if standaloneGroups[cobraGroup] {
			continue // stays flat
		}
		if len(tools) <= 1 {
			continue // single tool, stays flat
		}

		// Separate promoted tools from children.
		var children []mcpTool
		for _, t := range tools {
			if !promotedTools[t.Name] {
				children = append(children, t)
			}
		}
		if len(children) == 0 {
			continue // all promoted, no category needed
		}

		s.categories[group] = &mcpGroup{
			router:   buildCategoryRouter(group, cobraGroup, children),
			children: children,
		}
	}
}

// buildCategoryRouter creates the tier-1 category tool that summarizes
// available subcommands.
func buildCategoryRouter(group, cobraGroup string, children []mcpTool) mcpTool {
	subs := make([]string, 0, len(children))
	for _, c := range children {
		sub := strings.TrimPrefix(c.Name, group+"_")
		short := c.Description
		if i := strings.Index(short, " [guarded:"); i > 0 {
			short = short[:i]
		}
		if len(short) > 80 {
			short = short[:77] + "..."
		}
		subs = append(subs, sub+": "+short)
	}

	desc := fmt.Sprintf("%s operations (%d subcommands). "+
		"Call with an action, or action=\"help\" for full details",
		cobraGroup, len(children))

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Subcommand: " + strings.Join(subs, "; "),
			},
			"args": map[string]any{
				"type":        "string",
				"description": "positional arguments for the subcommand",
			},
		},
		"required": []string{"action"},
	}

	return mcpTool{Name: group, Description: desc, InputSchema: schema}
}

// visibleTools returns the tools that should appear in the current tools/list.
func (s *mcpSession) visibleTools() []mcpTool {
	var out []mcpTool

	// Tier 0: standalone groups and promoted tools (always visible).
	for _, t := range s.allTools {
		group := mcpToolGroup(t.Name)
		cobraGroup := strings.ReplaceAll(group, "_", "-")
		if standaloneGroups[cobraGroup] || promotedTools[t.Name] {
			out = append(out, t)
		}
	}

	// Tier 1: category routers.
	for _, cat := range s.categories {
		out = append(out, cat.router)
	}

	// Tier 2: expanded category children.
	for group := range s.expanded {
		if cat, ok := s.categories[group]; ok {
			out = append(out, cat.children...)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
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

	// Check if this is a category router call.
	if cat, ok := s.categories[params.Name]; ok {
		return s.handleCategoryCall(req.ID, params.Name, cat, params.Arguments)
	}

	// Direct tool call (standalone, promoted, or expanded tier-2).
	if _, ok := s.toolIndex[params.Name]; !ok {
		return text("unknown tool: "+params.Name, true)
	}

	out, err := mcpExecTool(params.Name, params.Arguments)
	if err != nil {
		return text(err.Error(), true)
	}
	return text(out, false)
}

// handleCategoryCall routes a category tool invocation to the right sub-tool.
func (s *mcpSession) handleCategoryCall(
	id json.RawMessage, group string, cat *mcpGroup, args map[string]any,
) jrpcResponse {
	text := func(str string, isErr bool) jrpcResponse {
		return jrpcResponse{JSONRPC: "2.0", ID: id, Result: map[string]any{
			"content": []map[string]string{{"type": "text", "text": str}},
			"isError": isErr,
		}}
	}

	action, _ := args["action"].(string)
	if action == "" || action == "help" {
		// Return subcommand listing.
		lines := make([]string, 0, len(cat.children))
		for _, c := range cat.children {
			sub := strings.TrimPrefix(c.Name, group+"_")
			lines = append(lines, fmt.Sprintf("  %-30s %s", sub, c.Description))
		}
		s.expandCategory(group)
		return text("Available subcommands for "+group+":\n"+strings.Join(lines, "\n"), false)
	}

	// Resolve the sub-tool name.
	toolName := group + "_" + action
	if _, ok := s.toolIndex[toolName]; !ok {
		return text("unknown action: "+action+". Call with action=\"help\" to list available subcommands.", true)
	}

	// Forward all args except "action" to the sub-tool.
	subArgs := make(map[string]any, len(args))
	for k, v := range args {
		if k != "action" {
			subArgs[k] = v
		}
	}

	s.expandCategory(group)

	out, err := mcpExecTool(toolName, subArgs)
	if err != nil {
		return text(err.Error(), true)
	}
	return text(out, false)
}

// expandCategory marks a category as expanded and sends a listChanged
// notification so the client re-fetches tools/list with the sub-tools.
func (s *mcpSession) expandCategory(group string) {
	if s.expanded[group] {
		return
	}
	s.expanded[group] = true
	// Send listChanged notification (JSON-RPC notification: no id).
	_ = s.enc.Encode(map[string]string{
		"jsonrpc": "2.0",
		"method":  "notifications/tools/list_changed",
	})
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
	name := strings.ReplaceAll(path, " ", "_")

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

func (s *mcpSession) handleResourceRead(req jrpcRequest) jrpcResponse {
	var params struct {
		URI string `json:"uri"`
	}
	_ = json.Unmarshal(req.Params, &params)

	body, ok := s.resCont[params.URI]
	if !ok {
		return jrpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &jrpcError{Code: -32602, Message: "unknown resource: " + params.URI},
		}
	}
	return jrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
		"contents": []map[string]string{
			{"uri": params.URI, "mimeType": "text/markdown", "text": body},
		},
	}}
}

// --- Resources from embedded docs/tips ---

func mcpResourcesFromTips() ([]mcpResource, map[string]string) {
	entries := tips.All()
	resources := make([]mcpResource, 0, len(entries))
	content := make(map[string]string, len(entries))

	for _, e := range entries {
		uri := "tips://" + e.Name
		resources = append(resources, mcpResource{
			URI:         uri,
			Name:        e.Title,
			Description: e.Title,
			MimeType:    "text/markdown",
		})
		content[uri] = e.Content
	}
	return resources, content
}
