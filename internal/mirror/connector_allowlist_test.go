package mirror

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConnectorAllowlistProjection(t *testing.T) {
	full := json.RawMessage(`{
	  "identifier": "connector-1",
	  "displayName": "Example Connector",
	  "connectorDefinitionName": "ExampleConnector",
	  "environment": {"name": "Default Environment"},
	  "integration": {"identifier": "ExamplePack", "displayName": "Example Pack"},
	  "isAllowlistSupported": true,
	  "allowList": ["Rule.ruleName = example"],
	  "parameters": [{"name": "Password", "value": "***"}]
	}`)

	obj, err := buildConnectorAllowlistObject(full)
	if err != nil {
		t.Fatalf("buildConnectorAllowlistObject: %v", err)
	}
	if obj.ServerID != "connector-1" || obj.Slug != "Example_Connector" {
		t.Fatalf("object identity = (%q,%q), want connector-1/Example_Connector", obj.ServerID, obj.Slug)
	}
	if strings.Contains(string(obj.Canonical), "Password") || strings.Contains(string(obj.Canonical), "displayName") {
		t.Fatalf("canonical should contain only allowList, got:\n%s", obj.Canonical)
	}

	dir := t.TempDir()
	if err := writeConnectorAllowlist(dir, obj); err != nil {
		t.Fatalf("writeConnectorAllowlist: %v", err)
	}
	written, err := os.ReadFile(filepath.Join(dir, "Example_Connector.json"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(written)
	for _, want := range []string{"Example Connector", "Default Environment", "Rule.ruleName = example", "_server"} {
		if !strings.Contains(out, want) {
			t.Fatalf("written allowlist missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Password") {
		t.Fatalf("written allowlist leaked connector parameters:\n%s", out)
	}
}

func TestConnectorAllowlistLoadCanonicalIgnoresContext(t *testing.T) {
	dir := t.TempDir()
	a := `{
	  "displayName": "One",
	  "environment": "Default Environment",
	  "allowList": ["Rule.ruleName = example"],
	  "_server": {"id": "connector-1"}
	}`
	b := `{
	  "displayName": "Renamed",
	  "environment": "Other",
	  "allowList": ["Rule.ruleName = example"],
	  "_server": {"id": "connector-1"}
	}`
	if err := os.WriteFile(filepath.Join(dir, "one.json"), []byte(a), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadConnectorAllowlists(dir)
	if err != nil {
		t.Fatalf("loadConnectorAllowlists: %v", err)
	}
	canonA := string(loaded[0].Canonical)
	if err := os.WriteFile(filepath.Join(dir, "one.json"), []byte(b), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err = loadConnectorAllowlists(dir)
	if err != nil {
		t.Fatalf("loadConnectorAllowlists renamed: %v", err)
	}
	if got := string(loaded[0].Canonical); got != canonA {
		t.Fatalf("canonical changed for context-only edit:\n%s\n---\n%s", canonA, got)
	}
}

func TestApplyConnectorAllowlistPreservesFullBody(t *testing.T) {
	full := json.RawMessage(`{
	  "identifier": "connector-1",
	  "displayName": "Example Connector",
	  "isAllowlistSupported": true,
	  "allowList": ["old"],
	  "parameters": [{"name": "Password", "value": "***"}]
	}`)
	canon, err := connectorAllowlistCanonical(json.RawMessage(`{"allowList":["new"]}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := applyConnectorAllowlist(full, canon)
	if err != nil {
		t.Fatalf("applyConnectorAllowlist: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	allow, err := parseConnectorAllowlistValues(got["allowList"])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(allow, []string{"new"}) {
		t.Fatalf("allowList = %#v", got["allowList"])
	}
	if params, ok := got["parameters"].([]any); !ok || len(params) != 1 {
		t.Fatalf("parameters not preserved: %#v", got["parameters"])
	}
}

func TestApplyConnectorAllowlistRejectsUnsupported(t *testing.T) {
	full := json.RawMessage(`{"identifier":"connector-1","displayName":"No Allowlist","isAllowlistSupported":false}`)
	canon, err := connectorAllowlistCanonical(json.RawMessage(`{"allowList":["Rule.ruleName = example"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyConnectorAllowlist(full, canon); err == nil {
		t.Fatal("applyConnectorAllowlist accepted non-empty allowList for unsupported connector")
	}
}

func TestConnectorAllowlistRejectsNonStringFilter(t *testing.T) {
	_, err := connectorAllowlistCanonical(json.RawMessage(`{"allowList":["ok", 1]}`))
	if err == nil {
		t.Fatal("connectorAllowlistCanonical accepted non-string allowList entry")
	}
}
