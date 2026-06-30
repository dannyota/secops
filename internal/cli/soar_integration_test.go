package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPickIntegrationInstance(t *testing.T) {
	insts := []integrationInstance{
		{Identifier: "inst-a", IntegrationIdentifier: "EmailV2", EnvironmentIdentifier: "Default Environment", InstanceName: "Email A"},
		{Identifier: "inst-b", IntegrationIdentifier: "EmailV2", EnvironmentIdentifier: "Prod", InstanceName: "Email B"},
	}

	// Single instance → auto-resolve without any narrowing.
	if id, env, err := pickIntegrationInstance(insts[:1], "EmailV2", "", ""); err != nil ||
		id != "inst-a" || env != "Default Environment" {
		t.Errorf("single instance: got (%q,%q,%v)", id, env, err)
	}

	// Several instances, no narrowing → ambiguous error listing the copy-paste flags.
	if _, _, err := pickIntegrationInstance(insts, "EmailV2", "", ""); err == nil ||
		!strings.Contains(err.Error(), "has 2 instances") ||
		!strings.Contains(err.Error(), "--id inst-a") {
		t.Errorf("ambiguous should list instances, got %v", err)
	}

	// Narrow by id → resolves the environment.
	if id, env, err := pickIntegrationInstance(insts, "EmailV2", "inst-b", ""); err != nil ||
		id != "inst-b" || env != "Prod" {
		t.Errorf("narrow by id: got (%q,%q,%v)", id, env, err)
	}

	// Narrow by environment (case-insensitive) → resolves the id.
	if id, _, err := pickIntegrationInstance(insts, "EmailV2", "", "default environment"); err != nil ||
		id != "inst-a" {
		t.Errorf("narrow by env: got (%q,%v)", id, err)
	}

	// No match → clean error pointing at the instances command.
	if _, _, err := pickIntegrationInstance(insts, "EmailV2", "nope", ""); err == nil ||
		!strings.Contains(err.Error(), "no matching instance") {
		t.Errorf("no match should error, got %v", err)
	}
}

// The authoring `update` verb patches by numeric id and needs at least one
// field to change; with neither --script nor --description it must refuse
// (offline, before any API call).
func TestAuthoringUpdateRequiresAField(t *testing.T) {
	cmd := newSOARIntegrationActionCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"update", "--integration", "HTTP", "--id", "42", "--yes"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Errorf("update with no field flags must refuse, got %v", err)
	}

	// Missing required flags (--integration / --id).
	cmd = newSOARIntegrationActionCmd()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"update"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("update without flags must error, got %v", err)
	}
}

func TestCollectionFor(t *testing.T) {
	if collectionFor("action") != "actions" || collectionFor("job") != "jobs" {
		t.Error("collectionFor must map action->actions, job->jobs")
	}
}

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

func TestAPIKeysAndActionVerbsRegistration(t *testing.T) {
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
		for _, want := range []string{"template", "create", "update", "delete"} {
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
