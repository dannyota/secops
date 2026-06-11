package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
)

// The components palette covers every designer Step Selection tab, `actions`
// no longer requires --integration (omitting it = the all-integration
// catalog), and `triggers` works fully offline.
func TestComponentsPaletteCommands(t *testing.T) {
	root := newSOARPlaybookComponentsCmd()
	want := map[string]bool{
		"integrations": false, "actions": false, "jobs": false,
		"connectors": false, "usage": false, "flow": false,
		"triggers": false, "blocks": false,
	}
	for _, c := range root.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
		if c.Name() == "actions" {
			if f := c.Flags().Lookup("integration"); f == nil {
				t.Error("actions must keep --integration")
			} else if req, ok := f.Annotations[cobra.BashCompOneRequiredFlag]; ok && len(req) > 0 && req[0] == "true" {
				t.Error("actions --integration must be optional (omit = all-integration catalog)")
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("components %s not registered", name)
		}
	}
}

func TestComponentsTriggersOffline(t *testing.T) {
	root := newSOARPlaybookComponentsCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"triggers"})
	if err := root.Execute(); err != nil {
		t.Fatalf("triggers must run offline: %v", err)
	}
	for _, token := range []string{"ALL", "CASE_DATA", "GET_INPUTS"} {
		if !strings.Contains(out.String(), token) {
			t.Errorf("triggers output missing %s", token)
		}
	}
}

func TestUsageFlagValidation(t *testing.T) {
	root := newSOARPlaybookComponentsCmd()
	root.SilenceUsage, root.SilenceErrors = true, true
	root.SetArgs([]string{"usage"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("usage without flags must demand --action-id or --action, got %v", err)
	}
}

func TestResolveActionByNameMatching(t *testing.T) {
	defs := []soar.ActionDef{
		{DisplayName: "Ping", Integration: "HTTP", ID: "10"},
		{DisplayName: "Ping", Integration: "Siemplify", ID: "11"},
		{DisplayName: "Post Data", Integration: "HTTP", ID: "12"},
	}
	if got := matchActionDefs(defs, "", "post data"); len(got) != 1 || got[0].ID.String() != "12" {
		t.Errorf("case-insensitive unique match = %+v", got)
	}
	if got := matchActionDefs(defs, "", "Ping"); len(got) != 2 {
		t.Errorf("ambiguous match = %+v", got)
	}
	if got := matchActionDefs(defs, "siemplify", "Ping"); len(got) != 1 || got[0].ID.String() != "11" {
		t.Errorf("integration-scoped match = %+v", got)
	}
	if got := matchActionDefs(defs, "", "nope"); len(got) != 0 {
		t.Errorf("no-match = %+v", got)
	}
}
