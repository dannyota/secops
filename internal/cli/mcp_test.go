package cli

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestMCPToolsFromCobra(t *testing.T) {
	tools := mcpToolsFromCobra()
	if len(tools) == 0 {
		t.Fatal("expected at least one MCP tool")
	}

	index := map[string]mcpTool{}
	for _, tool := range tools {
		index[tool.Name] = tool
	}

	// Verify well-known tools exist.
	for _, name := range []string{"doctor", "info", "search_udm", "cases_list", "audit_user"} {
		if _, ok := index[name]; !ok {
			t.Errorf("expected tool %q to exist", name)
		}
	}

	// Verify meta commands are excluded.
	for _, name := range []string{"mcp_serve", "mcp_install", "completion"} {
		if _, ok := index[name]; ok {
			t.Errorf("tool %q should be excluded from MCP", name)
		}
	}

	// Verify guarded mutations carry the guard description.
	if tool, ok := index["push_rules-update"]; ok {
		if tool.Description == "" || len(tool.Description) < 10 {
			t.Errorf("push_rules-update should have a description, got %q", tool.Description)
		}
	}
}

func TestMCPToolSchema(t *testing.T) {
	tools := mcpToolsFromCobra()
	index := map[string]mcpTool{}
	for _, tool := range tools {
		index[tool.Name] = tool
	}

	// search_udm should have an args property and common flags.
	tool, ok := index["search_udm"]
	if !ok {
		t.Skip("search_udm tool not found")
	}
	props, _ := tool.InputSchema["properties"].(map[string]any)
	if props == nil {
		t.Fatal("expected InputSchema.properties")
	}
	if _, ok := props["args"]; !ok {
		t.Error("search_udm should have an 'args' property for the filter")
	}
	if _, ok := props["hours"]; !ok {
		t.Error("search_udm should have an 'hours' flag property")
	}
}

func TestMCPResolveCommandPath(t *testing.T) {
	tests := []struct {
		segments []string
		want     string
	}{
		{[]string{"doctor"}, "doctor"},
		{[]string{"search", "udm"}, "search udm"},
		{[]string{"content", "hub", "browse"}, "content-hub browse"},
		{[]string{"data", "access", "labels", "list"}, "data-access labels list"},
	}
	for _, tt := range tests {
		resolved := mcpResolveCommandPath(tt.segments)
		got := joinPath(resolved)
		if got != tt.want {
			t.Errorf("mcpResolveCommandPath(%v) = %q, want %q", tt.segments, got, tt.want)
		}
	}
}

func joinPath(parts []string) string {
	return strings.Join(parts, " ")
}

func TestMCPResourcesFromEmbedded(t *testing.T) {
	resources, content := mcpResourcesFromEmbedded()
	if len(resources) == 0 {
		t.Fatal("expected at least one resource")
	}

	want := map[string]bool{
		"tips://15-recipes": false,
		"tips://16-gotchas": false,
		"guide://mcp":       false,
		"guide://search":    false,
		"guide://configure": false,
	}
	for _, r := range resources {
		if _, ok := want[r.URI]; ok {
			want[r.URI] = true
		}
		if body, ok := content[r.URI]; !ok || len(body) == 0 {
			t.Errorf("resource %q has no content", r.URI)
		}
	}
	for uri, found := range want {
		if !found {
			t.Errorf("expected resource %q", uri)
		}
	}
}

func TestMCPNoArgsCommandsLackArgsProperty(t *testing.T) {
	tools := mcpToolsFromCobra()
	index := map[string]mcpTool{}
	for _, tool := range tools {
		index[tool.Name] = tool
	}

	// Commands with cobra.NoArgs must NOT have an "args" property — their Use
	// string may contain flag hints (e.g. "close --id N --reason <enum>") that
	// positionalSpec must strip completely.
	noArgTools := []string{
		"doctor",
		"version",
		"status_capabilities",
		"mcp_install", // excluded, but if present must not have args
	}
	for _, name := range noArgTools {
		tool, ok := index[name]
		if !ok {
			continue // excluded from MCP or doesn't exist
		}
		props, _ := tool.InputSchema["properties"].(map[string]any)
		if props == nil {
			continue
		}
		if _, hasArgs := props["args"]; hasArgs {
			t.Errorf("tool %q has cobra.NoArgs but got an 'args' schema property", name)
		}
	}
}

func TestMCPPositionalArgsPresent(t *testing.T) {
	tools := mcpToolsFromCobra()
	index := map[string]mcpTool{}
	for _, tool := range tools {
		index[tool.Name] = tool
	}

	// Commands with genuine positional args must have an "args" property.
	tests := []struct {
		name    string
		wantSub string // substring in the args description
	}{
		{"search_udm", "<filter>"},
		{"audit_user", "<email>"},
		{"search_raw", "<pattern>"},
	}
	for _, tt := range tests {
		tool, ok := index[tt.name]
		if !ok {
			t.Errorf("tool %q not found", tt.name)
			continue
		}
		props, _ := tool.InputSchema["properties"].(map[string]any)
		if props == nil {
			t.Errorf("tool %q has no properties", tt.name)
			continue
		}
		argsProp, ok := props["args"]
		if !ok {
			t.Errorf("tool %q should have an 'args' property", tt.name)
			continue
		}
		desc, _ := argsProp.(map[string]any)["description"].(string)
		if !strings.Contains(desc, tt.wantSub) {
			t.Errorf("tool %q args description = %q, want substring %q", tt.name, desc, tt.wantSub)
		}
	}
}

func TestStripFlagHints(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Pure flags → empty.
		{"--id <alert-id> --reason <enum>", ""},
		{"[--enabled[=bool]] [--alerting[=bool]]", ""},
		{"(--a | --b)", ""},
		// Positional only.
		{"<filter>", "<filter>"},
		{"<email>", "<email>"},
		// Mixed: positional + flags.
		{"<alert-id> [--verbose]", "<alert-id>"},
		// Bare flag with pipe-separated values.
		{"--precision precise|broad", ""},
		// Flag with key=value placeholder.
		{"--property <name>=<value>", ""},
		// [flags] residue.
		{"<query> [flags]", "<query>"},
		// Variadic positional.
		{"<id> [<id>...]", "<id> [<id>...]"},
		// Real-world: search_raw.
		{"<pattern>", "<pattern>"},
	}
	for _, tt := range tests {
		got := stripFlagHints(tt.input)
		if got != tt.want {
			t.Errorf("stripFlagHints(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMCPSplitArgs(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{`cases list --limit 5`, []string{"cases", "list", "--limit", "5"}},
		{`playbooks rerun --case-id 6145 --name "SOC Agents - Auto-Trigger"`, []string{"playbooks", "rerun", "--case-id", "6145", "--name", "SOC Agents - Auto-Trigger"}},
		{`playbooks rerun --name 'SOC Agents - Auto-Trigger' --yes`, []string{"playbooks", "rerun", "--name", "SOC Agents - Auto-Trigger", "--yes"}},
		{`query udm 'ip = "10.0.0.1"'`, []string{"query", "udm", `ip = "10.0.0.1"`}},
		{`  spaces   between  `, []string{"spaces", "between"}},
		{`single`, []string{"single"}},
		{``, nil},
	}
	for _, tt := range tests {
		got := mcpSplitArgs(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("mcpSplitArgs(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("mcpSplitArgs(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestMCPToolAnnotations(t *testing.T) {
	tools := mcpToolsFromCobra()
	index := map[string]mcpTool{}
	for _, tool := range tools {
		index[tool.Name] = tool
	}

	// Read-only tools must have readOnlyHint.
	for _, name := range []string{"doctor", "info", "search_udm", "cases_list"} {
		tool, ok := index[name]
		if !ok {
			t.Errorf("tool %q not found", name)
			continue
		}
		if tool.Annotations == nil {
			t.Errorf("tool %q has no annotations", name)
			continue
		}
		if tool.Annotations["readOnlyHint"] != true {
			t.Errorf("tool %q should have readOnlyHint=true", name)
		}
	}

	// Guarded mutations must have destructiveHint.
	if tool, ok := index["push"]; ok {
		if tool.Annotations == nil || tool.Annotations["destructiveHint"] != true {
			t.Error("push should have destructiveHint=true")
		}
	}

	// Every tool should have a title annotation.
	for _, tool := range tools {
		if tool.Annotations == nil {
			t.Errorf("tool %q has no annotations", tool.Name)
			continue
		}
		if _, ok := tool.Annotations["title"]; !ok {
			t.Errorf("tool %q should have a title annotation", tool.Name)
		}
	}
}

func TestArgvHasOutputFlag(t *testing.T) {
	tests := []struct {
		argv []string
		want bool
	}{
		{[]string{"cases", "list"}, false},
		{[]string{"cases", "list", "--json"}, true},
		{[]string{"cases", "list", "--json=false"}, true},
		{[]string{"cases", "list", "--output", "csv"}, true},
		{[]string{"cases", "list", "--output=csv"}, true},
		{[]string{"audit", "user", "--format=json"}, true},
		{[]string{"cases", "list", "--jsonl"}, false},
		{[]string{"--outputs", "x"}, false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := argvHasOutputFlag(tt.argv); got != tt.want {
			t.Errorf("argvHasOutputFlag(%v) = %v, want %v", tt.argv, got, tt.want)
		}
	}
}

// Concurrent tools/call dispatch means focus/unfocus/usage and the read
// paths race against each other by design; this exercises them together so
// `go test -race` guards the session's locking. The usage call with an
// unknown deep path additionally walks cobra subtrees skipped at init,
// covering the presortCommandTree guarantee.
func TestMCPSessionConcurrentToolCalls(t *testing.T) {
	s := newMCPSession()
	s.enc = json.NewEncoder(io.Discard)

	id := json.RawMessage(`1`)
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Go(func() {
			switch i % 5 {
			case 0:
				s.handleFocus(id, map[string]any{"group": "cases"})
			case 1:
				s.handleUsage(id, map[string]any{"command": "cases list"})
			case 2:
				s.visibleTools()
				s.isFocused("cases_list")
			case 3:
				s.handleUnfocus(id, map[string]any{"group": "cases"})
			case 4:
				s.handleUsage(id, map[string]any{"command": "completion bash"})
			}
		})
	}
	wg.Wait()

	if got := len(s.visibleTools()); got < len(s.metaTools) {
		t.Errorf("visibleTools() = %d tools, want at least the %d meta tools", got, len(s.metaTools))
	}
}

func TestFlagSchemaProperty(t *testing.T) {
	tests := []struct {
		flag flagInfo
		want string
	}{
		{flagInfo{Name: "hours", Type: "int"}, "integer"},
		{flagInfo{Name: "name", Type: "string"}, "string"},
		{flagInfo{Name: "yes", Type: "bool"}, "boolean"},
		{flagInfo{Name: "ids", Type: "stringSlice"}, "array"},
		{flagInfo{Name: "timeout", Type: "duration"}, "string"},
	}
	for _, tt := range tests {
		prop := flagSchemaProperty(tt.flag)
		if got := prop["type"]; got != tt.want {
			t.Errorf("flagSchemaProperty(%q, %q) type = %v, want %v", tt.flag.Name, tt.flag.Type, got, tt.want)
		}
	}
}
