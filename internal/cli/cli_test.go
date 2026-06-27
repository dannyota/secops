package cli

import (
	"encoding/json"
	"testing"
)

// TestUDMSummarySnakeCase locks in the snake_case fallback: the legacy tool
// honored event_timestamp/event_type as well as the camelCase keys.
func TestUDMSummarySnakeCase(t *testing.T) {
	cases := []struct {
		name        string
		event       string
		when, etype string
	}{
		{
			"camel nested", `{"udm":{"metadata":{"eventTimestamp":"2026-01-01T00:00:00Z","eventType":"USER_LOGIN"}}}`,
			"2026-01-01T00:00:00Z", "USER_LOGIN",
		},
		{
			"snake nested", `{"udm":{"metadata":{"event_timestamp":"2026-02-02T00:00:00Z","event_type":"NETWORK_DNS"}}}`,
			"2026-02-02T00:00:00Z", "NETWORK_DNS",
		},
		{
			"snake top-level", `{"metadata":{"event_timestamp":"2026-03-03T00:00:00Z","event_type":"FILE_OPEN"}}`,
			"2026-03-03T00:00:00Z", "FILE_OPEN",
		},
		{"missing", `{"udm":{}}`, "?", "?"},
	}
	for _, tc := range cases {
		when, etype := udmSummary(json.RawMessage(tc.event))
		if when != tc.when || etype != tc.etype {
			t.Errorf("%s: udmSummary = (%q,%q), want (%q,%q)", tc.name, when, etype, tc.when, tc.etype)
		}
	}
}

// TestCommandsRegistered verifies each subcommand self-registered via its init()
// so the CLI tree is wired without touching the network or credentials.
func TestCommandsRegistered(t *testing.T) {
	want := []string{"info", "pull", "push", "search", "soar", "doctor"}
	have := map[string]bool{}
	for _, c := range rootCmd.Commands() {
		have[c.Name()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("command %q not registered on root", w)
		}
	}
}

// TestQueryHasUDMSubcommand verifies the nested query udm command exists.
func TestQueryHasUDMSubcommand(t *testing.T) {
	for _, c := range rootCmd.Commands() {
		if c.Name() != "search" {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Name() == "udm" {
				return
			}
		}
		t.Fatal("query command has no udm subcommand")
	}
	t.Fatal("query command not found")
}
