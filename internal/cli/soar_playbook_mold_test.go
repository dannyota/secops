package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSOARPlaybookMoldExtract(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "playbook.json")
	out := filepath.Join(dir, "molds", "lookup.json")
	body := `{
	  "name": "Source",
	  "templateName": null,
	  "steps": [
	    {"identifier": "s1", "name": "Nested", "type": 5},
	    {
	      "identifier": "s2",
	      "name": "Lookup",
	      "type": 0,
	      "integration": "Example",
	      "actionName": "Lookup Entity",
	      "parameters": []
	    }
	  ]
	}`
	if err := os.WriteFile(in, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newSOARPlaybookMoldExtractCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--file", in, "--step", "Lookup", "--out", out})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("mold extract: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var step map[string]any
	if err := json.Unmarshal(raw, &step); err != nil {
		t.Fatal(err)
	}
	if step["integration"] != "Example" || step["actionName"] != "Lookup Entity" {
		t.Fatalf("step action = (%q,%q)", step["integration"], step["actionName"])
	}
}
