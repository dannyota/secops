package cli

import (
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
