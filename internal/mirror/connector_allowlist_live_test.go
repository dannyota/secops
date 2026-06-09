package mirror

import (
	"encoding/json"
	"reflect"
	"testing"

	"danny.vn/secops/internal/mirror/reconcile"
)

// TestLiveConnectorAllowlistWriteSmoke validates the derived connector-allowlist
// surface against a real connector without changing desired state: it writes the
// current allowList value back through the same Update closure used by reconcile
// push, then confirms the allowList still matches. It never prints connector
// identifiers or filters.
func TestLiveConnectorAllowlistWriteSmoke(t *testing.T) {
	lc, ctx := liveLegacyClient(t)
	requireSmokeWrite(t)

	s, ok := BuildSOARSurface("connector-allowlist", lc)
	if !ok {
		t.Fatal("connector-allowlist is not a registered engine surface")
	}
	res, err := s.List(ctx)
	if err != nil {
		t.Fatalf("list connector allowlists: %v", err)
	}
	if len(res.Objects) == 0 {
		t.Skip("no connector instances available")
	}

	var live reconcile.Object
	for _, obj := range res.Objects {
		if connectorAllowlistSupported(obj.Raw) {
			live = obj
			break
		}
	}
	if live.ServerID == "" {
		t.Skip("no connector instance supports allowList")
	}
	before, err := connectorAllowlistFromRaw(live.Raw)
	if err != nil {
		t.Fatalf("decode original allowList: %v", err)
	}

	restore := func() {
		refreshed, rerr := lc.GetConnector(ctx, live.ServerID)
		if rerr != nil {
			t.Logf("cleanup: could not re-read connector after write attempt: %v", rerr)
			return
		}
		current, cerr := connectorAllowlistFromRaw(refreshed)
		if cerr != nil {
			t.Logf("cleanup: could not decode connector allowList after write attempt: %v", cerr)
			return
		}
		if reflect.DeepEqual(current, before) {
			return
		}
		if _, serr := lc.SaveConnector(ctx, live.Raw); serr != nil {
			t.Logf("cleanup: could not restore original connector allowList: %v", serr)
		}
	}
	t.Cleanup(restore)

	if _, err := s.Update(ctx, live, live); err != nil {
		t.Fatalf("idempotent allowList update: %v", err)
	}

	afterRaw, err := lc.GetConnector(ctx, live.ServerID)
	if err != nil {
		t.Fatalf("re-read connector after allowList update: %v", err)
	}
	after, err := connectorAllowlistFromRaw(afterRaw)
	if err != nil {
		t.Fatalf("decode updated allowList: %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("allowList changed after idempotent write")
	}
}

func connectorAllowlistSupported(raw json.RawMessage) bool {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	return boolFieldValue(m["isAllowlistSupported"])
}

func connectorAllowlistFromRaw(raw json.RawMessage) ([]string, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return parseConnectorAllowlistValues(m["allowList"])
}
