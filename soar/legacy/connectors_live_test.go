package legacy

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// newUUIDv4 generates a random v4 UUID. POST /connectors is an upsert keyed by
// identifier, so a NEW connector instance needs a client-assigned id.
func newUUIDv4(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("uuid: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// TestLiveConnectorsReads exercises the read-only connector endpoints (safe).
// Runs under SECOPS_SOAR_SMOKE=1.
//
// The zero-arg list endpoints succeed on a fresh tenant (they return whatever
// connector instances/definitions exist, possibly empty). When at least one
// connector instance is present, its string identifier is derived from the
// cards list and used to exercise the per-instance reads (GetConnector and its
// statistics); both are pure reads.
func TestLiveConnectorsReads(t *testing.T) {
	lc, ctx := liveClient(t)

	readProbe(t, "connectors/ListConnectorCards", func() (RawJSON, error) { return lc.ListConnectorCards(ctx) })
	readProbe(t, "connectors/ListConnectorTemplateCards", func() (RawJSON, error) { return lc.ListConnectorTemplateCards(ctx) })

	// Derive a connector instance identifier from the cards list, then exercise
	// the per-instance reads. Skip silently if the tenant has no connectors.
	raw, err := lc.ListConnectorCards(ctx)
	cards := objects(t, "connectors/ListConnectorCards", raw, err)
	if len(cards) == 0 {
		return
	}
	id := strField("identifier")(cards[0])
	if id == "" {
		return
	}
	readProbe(t, "connectors/GetConnector", func() (RawJSON, error) { return lc.GetConnector(ctx, id) })
	readProbe(t, "connectors/GetConnectorStatistics", func() (RawJSON, error) { return lc.GetConnectorStatistics(ctx, id) })
}

// GROUP E (operational config) — connectors. A connector instance is an ingestion
// source, so a throwaway one is created DISABLED (isEnabled:false): a disabled
// connector never runs and pulls no data. The create body is built from the
// connector TEMPLATE (GetConnectorTemplate), which supplies the integration key,
// default parameters and required fields; only displayName/environment/enabled
// change. FetchConnectorSampleData (which RUNS a connector) is never called.
//
// NOTE: POST /connectors is an upsert keyed by an existing identifier — on
// deployments where it only updates (it answers "Connector instance is not found"
// for a new id), there is no create path and the lifecycle skips cleanly.

// connectorIDByName returns the identifier of the connector card whose display
// name (or name) matches, or "" if absent.
func connectorIDByName(t *testing.T, ctx context.Context, lc *Client, name string) string {
	t.Helper()
	raw, err := lc.ListConnectorCards(ctx)
	for _, o := range objects(t, "connector-cards", raw, err) {
		if strField("displayName")(o) == name || strField("name")(o) == name {
			return strField("identifier")(o)
		}
	}
	return ""
}

// TestLiveConnectorCRUD runs create -> list -> read -> edit -> read -> delete ->
// list on a throwaway DISABLED connector instance built from a connector template.
// Write-gated; deletes the connector on cleanup even on failure.
func TestLiveConnectorCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	requireWrite(t)
	env := firstEnvironment(t, ctx, lc)

	raw, err := lc.ListConnectorTemplateCards(ctx)
	tcards := objects(t, "connector-template-cards", raw, err)
	if len(tcards) == 0 {
		t.Skip("no connector templates to instantiate")
	}
	card := tcards[0]
	raw, err = lc.GetConnectorTemplate(ctx, map[string]any{
		"integration":             card["integration"],
		"connectorDefinitionName": card["connectorDefinitionName"],
	})
	if err != nil {
		t.Fatalf("get connector template: %v", err)
	}
	var inst map[string]any
	if err := json.Unmarshal(raw, &inst); err != nil {
		t.Fatalf("decode connector template: %v", err)
	}

	// Turn the template into a new, DISABLED instance. POST /connectors upserts by
	// identifier, so a new instance carries a client-assigned UUID.
	label := smokeLabel("connector")
	newID := newUUIDv4(t)
	inst["displayName"] = label
	inst["environment"] = env
	inst["isEnabled"] = false // never runs
	inst["identifier"] = newID
	inst["isNew"] = true

	// 1. baseline — absent.
	if id := connectorIDByName(t, ctx, lc, label); id != "" {
		t.Fatalf("smoke connector %q unexpectedly already exists (id=%s)", label, id)
	}

	// 2. create. Where POST /connectors only updates (no create path), it answers
	//    "not found" for the new id — treat that as an environmental skip.
	if _, err := lc.SaveConnector(ctx, inst); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			t.Skipf("connector create not supported here (POST /connectors is update-only): %v", err)
		}
		t.Fatalf("create connector: %v", err)
	}

	// 3. list -> capture identifier; register cleanup immediately.
	id := connectorIDByName(t, ctx, lc, label)
	if id == "" {
		t.Fatalf("created connector %q not found after create", label)
	}
	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		if _, err := lc.DeleteConnector(ctx, id); err != nil {
			t.Logf("cleanup: could not delete throwaway connector %q (%s): %v", label, id, err)
		}
	})

	// 4. read by id; confirm it is disabled (inert).
	raw, err = lc.GetConnector(ctx, id)
	if err != nil {
		t.Fatalf("read connector: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode connector: %v", err)
	}
	if en, _ := got["isEnabled"].(bool); en {
		t.Fatalf("created connector is ENABLED; expected disabled (inert)")
	}

	// 5. edit — rename via SaveConnector (round-trip the read object), still off.
	got["displayName"] = label + "-edited"
	got["isEnabled"] = false
	if _, err := lc.SaveConnector(ctx, got); err != nil {
		t.Fatalf("update connector: %v", err)
	}

	// 6. read -> verify the edit.
	if connectorIDByName(t, ctx, lc, label+"-edited") != id {
		t.Fatalf("read#2: edit not reflected for connector %s", id)
	}

	// 7. delete.
	if _, err := lc.DeleteConnector(ctx, id); err != nil {
		t.Fatalf("delete connector: %v", err)
	}
	deleted = true

	// 8. list -> gone.
	if connectorIDByName(t, ctx, lc, label+"-edited") != "" {
		t.Fatalf("connector %s still present after delete", id)
	}
}
