package cli

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"danny.vn/secops/soar"
)

func TestBuildSOARIntegrationHealth(t *testing.T) {
	installed := []soar.Integration{
		{Identifier: "ExamplePack", DisplayName: "Example Pack"},
		{Identifier: "IdlePack", DisplayName: "Idle Pack", Custom: true},
	}
	connectors := json.RawMessage(`[
	  {
	    "integration": {"identifier": "ExamplePack", "displayName": "Example Pack"},
	    "cards": [
	      {
	        "identifier": "connector-1",
	        "displayName": "Example Connector",
	        "isEnabled": true,
	        "isConfigured": true,
	        "environment": {"name": "Default Environment"}
	      }
	    ]
	  }
	]`)
	jobs := json.RawMessage(`[
	  {
	    "uniqueIdentifier": "job-1",
	    "name": "Example Job",
	    "integration": {"identifier": "MissingPack", "displayName": "Missing Pack"},
	    "isEnabled": false,
	    "isConfigured": false,
	    "environmentName": "Default Environment"
	  }
	]`)

	rows, err := buildSOARIntegrationHealth(installed, connectors, jobs)
	if err != nil {
		t.Fatalf("buildSOARIntegrationHealth: %v", err)
	}
	byKey := map[string]soarIntegrationHealthRow{}
	for _, r := range rows {
		byKey[r.Key] = r
	}

	ex := byKey["ExamplePack"]
	if !ex.Installed || ex.ConnectorInstances != 1 || ex.EnabledConnectorInstances != 1 || len(ex.Issues) != 0 {
		t.Errorf("ExamplePack row = %+v, want installed connector runtime with no issues", ex)
	}
	if got := strings.Join(ex.Environments, ","); got != "Default Environment" {
		t.Errorf("ExamplePack environments = %q", got)
	}

	idle := byKey["IdlePack"]
	if !idle.Installed || !idle.Custom || !hasIssue(idle, "config_without_runtime") {
		t.Errorf("IdlePack row = %+v, want config_without_runtime", idle)
	}

	missing := byKey["MissingPack"]
	for _, issue := range []string{"runtime_without_installed_pack", "runtime_disabled", "unconfigured_runtime"} {
		if !hasIssue(missing, issue) {
			t.Errorf("MissingPack issues = %v, missing %q", missing.Issues, issue)
		}
	}
	if missing.JobInstances != 1 || missing.EnabledJobInstances != 0 || missing.UnconfiguredRuntime != 1 {
		t.Errorf("MissingPack row = %+v, want one disabled unconfigured job", missing)
	}
}

func TestBuildSOARIntegrationHealthMatchesProdIdentifierAlias(t *testing.T) {
	installed := []soar.Integration{
		{Identifier: "BasePack__00000000-0000-0000-0000-000000000000", ProdIdentifier: "BasePack", DisplayName: "Base Pack"},
	}
	connectors := json.RawMessage(`[
	  {"identifier": "connector-1", "integrationIdentifier": "BasePack", "isEnabled": true}
	]`)

	rows, err := buildSOARIntegrationHealth(installed, connectors, nil)
	if err != nil {
		t.Fatalf("buildSOARIntegrationHealth: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one aliased row", rows)
	}
	if rows[0].Key != "BasePack__00000000-0000-0000-0000-000000000000" || rows[0].ConnectorInstances != 1 {
		t.Errorf("aliased row = %+v", rows[0])
	}
}

func TestEmitSOARIntegrationHealth(t *testing.T) {
	rows := []soarIntegrationHealthRow{{
		Key:                       "ExamplePack",
		DisplayName:               "Example Pack",
		Installed:                 true,
		ConnectorInstances:        1,
		EnabledConnectorInstances: 1,
		Environments:              []string{"Default Environment"},
	}}
	var buf bytes.Buffer
	emitSOARIntegrationHealth(&buf, rows)
	out := buf.String()
	for _, want := range []string{"INTEGRATION", "Example Pack", "conn=1 job=0", "Default Environment"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func hasIssue(r soarIntegrationHealthRow, want string) bool {
	return slices.Contains(r.Issues, want)
}
