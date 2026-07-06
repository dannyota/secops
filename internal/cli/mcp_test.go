package cli

import (
	"strings"
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

func TestMCPResourcesFromTips(t *testing.T) {
	resources, content := mcpResourcesFromTips()
	if len(resources) == 0 {
		t.Fatal("expected at least one resource")
	}

	foundRecipes := false
	foundGotchas := false
	for _, r := range resources {
		if r.URI == "tips://15-recipes" {
			foundRecipes = true
		}
		if r.URI == "tips://16-gotchas" {
			foundGotchas = true
		}
		if body, ok := content[r.URI]; !ok || len(body) == 0 {
			t.Errorf("resource %q has no content", r.URI)
		}
	}
	if !foundRecipes {
		t.Error("expected tips://15-recipes resource")
	}
	if !foundGotchas {
		t.Error("expected tips://16-gotchas resource")
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
