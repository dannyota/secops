package soar

import (
	"encoding/json"
	"testing"
)

// TestConnectorParamsDecode verifies the tolerant parameters decode: the live
// array of descriptors and the older flat {name:value} map both round-trip, an
// absent/empty field yields no parameters, and a present-but-malformed value is a
// hard error (not a silently-empty parameter set that drops the connector config).
func TestConnectorParamsDecode(t *testing.T) {
	t.Run("array form", func(t *testing.T) {
		var ci ConnectorInstance
		if err := json.Unmarshal([]byte(`{"parameters":[{"name":"a","value":"1"}]}`), &ci); err != nil {
			t.Fatal(err)
		}
		if len(ci.Parameters) != 1 || ci.Parameters[0].Name != "a" || ci.Parameters[0].Value != "1" {
			t.Errorf("array form decoded wrong: %+v", ci.Parameters)
		}
	})
	t.Run("map form", func(t *testing.T) {
		var ci ConnectorInstance
		if err := json.Unmarshal([]byte(`{"parameters":{"a":"1"}}`), &ci); err != nil {
			t.Fatal(err)
		}
		if len(ci.Parameters) != 1 || ci.Parameters[0].Name != "a" || ci.Parameters[0].Value != "1" {
			t.Errorf("map form decoded wrong: %+v", ci.Parameters)
		}
	})
	t.Run("absent", func(t *testing.T) {
		var ci ConnectorInstance
		if err := json.Unmarshal([]byte(`{"identifier":"x"}`), &ci); err != nil {
			t.Fatal(err)
		}
		if ci.Parameters != nil {
			t.Errorf("absent parameters should decode to nil, got %+v", ci.Parameters)
		}
	})
	t.Run("explicit null", func(t *testing.T) {
		// "no parameters configured" is idiomatically null — must not fail the
		// whole list decode (one such record would otherwise take down ListConnectorInstances).
		var ci ConnectorInstance
		if err := json.Unmarshal([]byte(`{"identifier":"x","parameters":null}`), &ci); err != nil {
			t.Fatalf("null parameters should decode cleanly, got %v", err)
		}
		if ci.Parameters != nil {
			t.Errorf("null parameters should decode to nil, got %+v", ci.Parameters)
		}
	})
	t.Run("malformed is an error", func(t *testing.T) {
		var ci ConnectorInstance
		// A number is neither the array nor the map shape — must error, not silently drop.
		if err := json.Unmarshal([]byte(`{"parameters":42}`), &ci); err == nil {
			t.Error("malformed parameters should be an error, not a silent empty set")
		}
	})
}
