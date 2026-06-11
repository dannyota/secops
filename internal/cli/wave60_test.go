package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// fillAuthoringTemplate must overlay the create-marker fields while leaving
// every other template field byte-exact (no float64 round-trip damage on the
// server's int64 timeouts).
func TestFillAuthoringTemplate(t *testing.T) {
	tpl := json.RawMessage(`{"name":null,"displayName":"New Action","custom":true,"integration":null,` +
		`"script":null,"timeoutSeconds":600,"asyncPollingIntervalSeconds":3600,"asyncTotalTimeoutSeconds":86400,` +
		`"parameters":[],"dynamicResultsMetadataJson":"[{\"ResultName\":\"JsonResult\"}]"}`)
	out, err := fillAuthoringTemplate(tpl, "HTTP", "my-action", "print('x')\n", "does x")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"name":                        `""`,
		"integration":                 `"HTTP"`,
		"displayName":                 `"my-action"`,
		"custom":                      "true",
		"description":                 `"does x"`,
		"timeoutSeconds":              "600",
		"asyncPollingIntervalSeconds": "3600",
		"asyncTotalTimeoutSeconds":    "86400",
	} {
		if string(m[key]) != want {
			t.Errorf("%s = %s, want %s", key, m[key], want)
		}
	}
	if !strings.Contains(string(m["script"]), "print") {
		t.Errorf("script = %s", m["script"])
	}
	// The embedded JSON-string field must survive untouched.
	if string(m["dynamicResultsMetadataJson"]) != `"[{\"ResultName\":\"JsonResult\"}]"` {
		t.Errorf("dynamicResultsMetadataJson = %s", m["dynamicResultsMetadataJson"])
	}
}

func TestNewRandomUUID(t *testing.T) {
	pat := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a, err := newRandomUUID()
	if err != nil || !pat.MatchString(a) {
		t.Fatalf("uuid %q, %v", a, err)
	}
	b, _ := newRandomUUID()
	if a == b {
		t.Error("two mints must differ")
	}
}

// The Wave 60 verbs are registered with their guard flags, and revoke
// enforces exactly-one selector offline.
func TestWave60CommandRegistration(t *testing.T) {
	keys := newSOARAPIKeysCmd()
	found := map[string]bool{}
	for _, c := range keys.Commands() {
		found[c.Name()] = true
		if c.Name() == "create" || c.Name() == "revoke" {
			if c.Flags().Lookup("dry-run") == nil || c.Flags().Lookup("yes") == nil {
				t.Errorf("api-keys %s must carry the guard flags", c.Name())
			}
		}
	}
	for _, name := range []string{"list", "create", "revoke"} {
		if !found[name] {
			t.Errorf("api-keys %s not registered", name)
		}
	}

	integ := newSOARIntegrationCmd()
	subtrees := map[string][]string{"action": nil, "job-def": nil}
	for _, c := range integ.Commands() {
		if _, ok := subtrees[c.Name()]; ok {
			for _, sub := range c.Commands() {
				subtrees[c.Name()] = append(subtrees[c.Name()], sub.Name())
			}
		}
	}
	for tree, verbs := range subtrees {
		got := strings.Join(verbs, ",")
		for _, want := range []string{"template", "create", "delete"} {
			if !strings.Contains(got, want) {
				t.Errorf("integration %s missing %s (has %s)", tree, want, got)
			}
		}
	}
}

func TestAPIKeyRevokeSelectorValidation(t *testing.T) {
	for _, args := range [][]string{
		{"revoke"},
		{"revoke", "--name", "x", "--id", "7"},
	} {
		cmd := newSOARAPIKeysCmd()
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Errorf("args %v: err = %v, want exactly-one selector error", args, err)
		}
	}
}
