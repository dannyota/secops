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
	job := commandChild(soar, "job")
	if job == nil {
		t.Fatal("soar job command not registered")
	}
	for _, name := range []string{"list", "run", "instance"} {
		if commandChild(job, name) == nil {
			t.Fatalf("soar job %s command not registered", name)
		}
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
