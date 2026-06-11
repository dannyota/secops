package soar

import (
	"encoding/json"
	"testing"
)

// Pin the wildcard-catalog wire shapes: actions (camel envelope, numeric id),
// transformers, and logical operators (whose envelope key stays snake_case
// even under format=camel).
func TestActionDefDecode(t *testing.T) {
	payload := `{
		"name": "projects/p/locations/l/instances/i/integrations/HTTP/actions/1529",
		"id": 1529,
		"displayName": "Post Data",
		"integration": "HTTP",
		"description": "POST a payload",
		"enabled": true,
		"async": false,
		"custom": true
	}`
	var a ActionDef
	if err := json.Unmarshal([]byte(payload), &a); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if a.PathID() != "1529" || a.Integration != "HTTP" || !a.Custom || !a.Enabled {
		t.Errorf("decoded = %+v", a)
	}
	if len(a.Raw) == 0 {
		t.Error("Raw must retain the full object")
	}

	// Without an id field the numeric tail of the resource name addresses it.
	var b ActionDef
	if err := json.Unmarshal([]byte(`{"name":"projects/p/locations/l/instances/i/integrations/HTTP/actions/7","displayName":"X"}`), &b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b.PathID() != "7" {
		t.Errorf("PathID without id = %q", b.PathID())
	}
}

func TestFlowFunctionEnvelopes(t *testing.T) {
	var tl transformersList
	if err := json.Unmarshal([]byte(`{"transformers":[{"id":22,"displayName":"trimChars","integration":"Core Functions","type":"BuiltIn","usageExample":"trimChars(\"abcd\")","enabled":true}]}`), &tl); err != nil {
		t.Fatalf("decode transformers: %v", err)
	}
	if len(tl.Items) != 1 || tl.Items[0].DisplayName != "trimChars" || tl.Items[0].UsageExample == "" {
		t.Errorf("transformers = %+v", tl.Items)
	}

	// The logical-operators envelope is snake_case on the live server.
	var ol logicalOperatorsList
	if err := json.Unmarshal([]byte(`{"logical_operators":[{"id":9,"displayName":"Not Empty","integration":"Core Functions","enabled":true}]}`), &ol); err != nil {
		t.Fatalf("decode logical_operators: %v", err)
	}
	if len(ol.Items) != 1 || ol.Items[0].DisplayName != "Not Empty" {
		t.Errorf("logical_operators = %+v", ol.Items)
	}
	// A camelCase envelope must also be accepted.
	ol = logicalOperatorsList{}
	if err := json.Unmarshal([]byte(`{"logicalOperators":[{"id":9,"displayName":"Empty"}]}`), &ol); err != nil {
		t.Fatalf("decode logicalOperators: %v", err)
	}
	if len(ol.ItemsCamel) != 1 {
		t.Errorf("camel envelope = %+v", ol.ItemsCamel)
	}
}
