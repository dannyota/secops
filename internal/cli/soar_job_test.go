package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSOARJobCommandRegistered(t *testing.T) {
	soar := commandChild(rootCmd, "soar")
	if soar == nil {
		t.Fatal("soar command not registered")
	}
	job := commandChild(soar, "jobs")
	if job == nil {
		t.Fatal("soar jobs command not registered")
	}
	for _, name := range []string{"list", "run", "template", "instance", "logs"} {
		if commandChild(job, name) == nil {
			t.Fatalf("soar job %s command not registered", name)
		}
	}
	template := commandChild(job, "template")
	if commandChild(template, "list") == nil {
		t.Fatal("soar job template list command not registered")
	}
}

func TestPythonLogsBody(t *testing.T) {
	body := pythonLogsBody(" severity>=ERROR ", " token ", " desc ", 25)
	for k, v := range map[string]any{
		"filter":    "severity>=ERROR",
		"pageToken": "token",
		"sortOrder": "desc",
		"pageSize":  25,
	} {
		if body[k] != v {
			t.Fatalf("body[%s] = %#v, want %#v", k, body[k], v)
		}
	}

	empty := pythonLogsBody(" ", "\t", "\n", 0)
	if len(empty) != 0 {
		t.Fatalf("empty body = %#v, want no fields", empty)
	}
}

func TestSummarizeSOARJobs(t *testing.T) {
	raw := json.RawMessage(`[
	  {
	    "id": 1,
	    "uniqueIdentifier": "job-uuid",
	    "name": "Nightly Sync",
	    "integration": "Example",
	    "jobDefinitionName": "Sync",
	    "script": "secret script body",
	    "isEnabled": false,
	    "lastRunStatus": 2,
	    "parameters": [{"name": "Token"}]
	  }
	]`)
	rows, err := summarizeSOARJobs(raw, "")
	if err != nil {
		t.Fatalf("summarizeSOARJobs: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want 1", rows)
	}
	row := rows[0]
	if row.ID != "1" || row.UniqueIdentifier != "job-uuid" || row.Name != "Nightly Sync" {
		t.Fatalf("row = %#v", row)
	}
	if row.ParameterCount != 1 {
		t.Fatalf("ParameterCount = %d, want 1", row.ParameterCount)
	}
	encoded, _ := json.Marshal(row)
	if strings.Contains(string(encoded), "secret script body") {
		t.Fatalf("summary leaked script body: %s", encoded)
	}
}

func TestFindSOARJob(t *testing.T) {
	raw := json.RawMessage(`[
	  {"id": 1, "uniqueIdentifier": "first", "name": "A"},
	  {"id": 2, "uniqueIdentifier": "second", "name": "B"}
	]`)
	_, row, err := findSOARJob(raw, "second")
	if err != nil {
		t.Fatalf("findSOARJob: %v", err)
	}
	if row.ID != "2" || row.Name != "B" {
		t.Fatalf("row = %#v", row)
	}
}

func TestSummarizeSOARJobTemplates(t *testing.T) {
	raw := json.RawMessage(`[
	  {
	    "id": 3,
	    "uniqueIdentifier": "template-uuid",
	    "name": "Template",
	    "integration": "Example",
	    "jobDefinitionName": "Definition",
	    "script": "secret script body",
	    "isEnabled": true,
	    "isCustom": false,
	    "isSystemJob": true,
	    "runIntervalInSeconds": 300,
	    "parameters": [{"name": "Token"}]
	  }
	]`)
	rows, err := summarizeSOARJobTemplates(raw, "")
	if err != nil {
		t.Fatalf("summarizeSOARJobTemplates: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want 1", rows)
	}
	row := rows[0]
	if row.ID != "3" || row.UniqueIdentifier != "template-uuid" || row.Name != "Template" {
		t.Fatalf("row = %#v", row)
	}
	if row.ParameterCount != 1 || row.RunIntervalInSeconds != "300" {
		t.Fatalf("row = %#v, want parameter count and interval", row)
	}
	encoded, _ := json.Marshal(row)
	if strings.Contains(string(encoded), "secret script body") {
		t.Fatalf("summary leaked script body: %s", encoded)
	}
}

func TestSummarizeSOARJobInstances(t *testing.T) {
	raw := json.RawMessage(`{
	  "objectsList": [
	    {"id": 7, "uniqueIdentifier": "inst-uuid", "name": "Hourly", "category": "Sync", "isEnabled": true}
	  ]
	}`)
	rows, err := summarizeSOARJobInstances(raw, "")
	if err != nil {
		t.Fatalf("summarizeSOARJobInstances: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want 1", rows)
	}
	if rows[0].ID != "7" || rows[0].UniqueIdentifier != "inst-uuid" {
		t.Fatalf("row = %#v", rows[0])
	}
}

func TestSOARJobInstanceSubcommands(t *testing.T) {
	soar := commandChild(rootCmd, "soar")
	if soar == nil {
		t.Fatal("soar command not registered")
	}
	job := commandChild(soar, "jobs")
	if job == nil {
		t.Fatal("soar jobs command not registered")
	}
	instance := commandChild(job, "instance")
	if instance == nil {
		t.Fatal("soar jobs instance command not registered")
	}
	for _, name := range []string{"list", "get", "run", "set", "create", "delete", "history"} {
		if commandChild(instance, name) == nil {
			t.Errorf("soar jobs instance %s command not registered", name)
		}
	}

	revision := commandChild(job, "revision")
	if revision == nil {
		t.Fatal("soar jobs revision command not registered")
	}
	for _, name := range []string{"list", "create", "rollback", "delete"} {
		if commandChild(revision, name) == nil {
			t.Errorf("soar jobs revision %s command not registered", name)
		}
	}
}

func TestSoarJobInstanceRowFromRaw_Modern(t *testing.T) {
	raw := json.RawMessage(`{
	  "name": "projects/000000000000/locations/us/instances/00000000-0000-0000-0000-000000000000/integrations/Example/jobs/1/jobInstances/2",
	  "id": 2,
	  "job": "Test Job",
	  "integration": "Example",
	  "displayName": "My Test Instance",
	  "enabled": true,
	  "intervalSeconds": 300,
	  "custom": true,
	  "lastRunStatus": "SUCCESS",
	  "lastRunTime": 1700000000000,
	  "uniqueIdentifier": "test-uuid-1234",
	  "parameters": [{"id": 1, "mandatory": true, "type": "STRING", "displayName": "API Key", "value": "***"}],
	  "script": "secret script body"
	}`)
	row, ok := soarJobInstanceRowFromRaw(raw)
	if !ok {
		t.Fatal("soarJobInstanceRowFromRaw returned ok=false for valid input")
	}
	if row.ID != "2" {
		t.Errorf("ID = %q, want %q", row.ID, "2")
	}
	if row.UniqueIdentifier != "test-uuid-1234" {
		t.Errorf("UniqueIdentifier = %q, want %q", row.UniqueIdentifier, "test-uuid-1234")
	}
	if row.ParameterCount != 1 {
		t.Errorf("ParameterCount = %d, want 1", row.ParameterCount)
	}

	// The row JSON must not leak the script body.
	encoded, _ := json.Marshal(row)
	if strings.Contains(string(encoded), "secret script body") {
		t.Fatalf("summary leaked script body: %s", encoded)
	}
}

func TestParseJobInstanceName(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantIntegration string
		wantJobID       string
		wantInstance    string
		wantOK          bool
	}{
		{
			name:            "full resource name",
			input:           "projects/123/locations/us/instances/abc/integrations/Foo/jobs/42/jobInstances/7",
			wantIntegration: "Foo",
			wantJobID:       "42",
			wantInstance:    "7",
			wantOK:          true,
		},
		{
			name:   "invalid path",
			input:  "something/random",
			wantOK: false,
		},
		{
			name:            "suffix only",
			input:           "integrations/X/jobs/Y/jobInstances/Z",
			wantIntegration: "X",
			wantJobID:       "Y",
			wantInstance:    "Z",
			wantOK:          true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			integration, jobID, instanceID, ok := parseJobInstanceName(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if integration != tc.wantIntegration {
				t.Errorf("integration = %q, want %q", integration, tc.wantIntegration)
			}
			if jobID != tc.wantJobID {
				t.Errorf("jobID = %q, want %q", jobID, tc.wantJobID)
			}
			if instanceID != tc.wantInstance {
				t.Errorf("instanceID = %q, want %q", instanceID, tc.wantInstance)
			}
		})
	}
}

func TestSummarizeSOARJobInstances_ModernShape(t *testing.T) {
	// Modern v1alpha returns a bare array with camelCase fields, not the
	// legacy objectsList envelope.
	raw := json.RawMessage(`[
	  {"id": 1, "displayName": "Instance A", "integration": "X", "enabled": true, "intervalSeconds": 60},
	  {"id": 2, "displayName": "Instance B", "integration": "Y", "enabled": false, "intervalSeconds": 300}
	]`)
	rows, err := summarizeSOARJobInstances(raw, "")
	if err != nil {
		t.Fatalf("summarizeSOARJobInstances: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	// Verify the bare-array path extracted valid rows.
	if rows[0].ID != "1" && rows[1].ID != "1" {
		t.Errorf("expected a row with ID=1 in %+v", rows)
	}
}
