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

func runMCPServe() error {
	mcpTools := mcpToolsFromCobra()
	mcpResources, resourceContent := mcpResourcesFromTips()

	toolIndex := make(map[string]mcpTool, len(mcpTools))
	for _, t := range mcpTools {
		toolIndex[t.Name] = t
	}

	enc := json.NewEncoder(os.Stdout)
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
		resp := mcpDispatch(req, mcpTools, toolIndex, mcpResources, resourceContent)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func mcpDispatch(
	req jrpcRequest, tools []mcpTool, toolIndex map[string]mcpTool,
	resources []mcpResource, resourceContent map[string]string,
) jrpcResponse {
	ok := func(result any) jrpcResponse {
		return jrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	}

	switch req.Method {
	case "initialize":
		return ok(map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools":     map[string]any{},
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
		return ok(map[string]any{"tools": tools})

	case "tools/call":
		return mcpHandleToolCall(req, toolIndex)

	case "resources/list":
		return ok(map[string]any{"resources": resources})

	case "resources/read":
		return mcpHandleResourceRead(req, resourceContent)

	default:
		return jrpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &jrpcError{Code: -32601, Message: "method not found: " + req.Method},
		}
	}
}

func mcpHandleToolCall(req jrpcRequest, toolIndex map[string]mcpTool) jrpcResponse {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	_ = json.Unmarshal(req.Params, &params)

	text := func(s string, isErr bool) jrpcResponse {
		return jrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"content": []map[string]string{{"type": "text", "text": s}},
			"isError": isErr,
		}}
	}

	if _, ok := toolIndex[params.Name]; !ok {
		return text("unknown tool: "+params.Name, true)
	}

	out, err := mcpExecTool(params.Name, params.Arguments)
	if err != nil {
		return text(err.Error(), true)
	}
	return text(out, false)
}

func mcpHandleResourceRead(req jrpcRequest, content map[string]string) jrpcResponse {
	var params struct {
		URI string `json:"uri"`
	}
	_ = json.Unmarshal(req.Params, &params)

	body, ok := content[params.URI]
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

func mcpHasFlag(args map[string]any, name string) bool {
	if _, ok := args[name]; ok {
		return true
	}
	_, ok := args[strings.ReplaceAll(name, "-", "_")]
	return ok
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
