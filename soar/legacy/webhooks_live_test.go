package legacy

import (
	"context"
	"encoding/json"
	"testing"
)

// TestLiveWebhooksReads exercises the read-only webhooks-management endpoints
// (safe). Runs under SECOPS_SOAR_SMOKE=1.
//
// ListWebhookCards and GetWebhookLogs (called with empty filters) need no prior
// tenant setup. GetWebhook and GetWebhookStatistics require an identifier, so
// they are only probed when the cards list yields one — derived in-test.
func TestLiveWebhooksReads(t *testing.T) {
	lc, ctx := liveClient(t)

	cards := readProbe(t, "webhooks/ListWebhookCards", func() (RawJSON, error) {
		return lc.ListWebhookCards(ctx)
	})
	// Empty filters return all logs with no prior setup required.
	readProbe(t, "webhooks/GetWebhookLogs", func() (RawJSON, error) {
		return lc.GetWebhookLogs(ctx, "", "")
	})

	// Derive a webhook identifier from the cards list (uuid string in the
	// "identifier" field) to probe the per-webhook reads, if any exist.
	id := firstWebhookIdentifier(cards)
	if id == "" {
		t.Log("no webhooks present; skipping GetWebhook / GetWebhookStatistics")
		return
	}
	readProbe(t, "webhooks/GetWebhook", func() (RawJSON, error) {
		return lc.GetWebhook(ctx, id)
	})
	readProbe(t, "webhooks/GetWebhookStatistics", func() (RawJSON, error) {
		return lc.GetWebhookStatistics(ctx, id)
	})
}

// firstWebhookIdentifier extracts the "identifier" of the first webhook card
// from a ListWebhookCards response (a JSON array of {identifier,name}).
func firstWebhookIdentifier(raw RawJSON) string {
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) != nil {
		return ""
	}
	for _, o := range arr {
		if id, _ := o["identifier"].(string); id != "" {
			return id
		}
	}
	return ""
}

// GROUP E (operational config) — webhooks. A webhook is a passive inbound HTTP
// endpoint: it does nothing until an external caller posts to it, so a throwaway
// one (never published, immediately deleted) is inert. Unlike the other
// operational surfaces (jobs/connectors/integrations/playbooks), it triggers no
// scheduled execution or data ingestion on its own.

// webhookIDByName returns the uuid identifier of the webhook card named `name`,
// or "" if absent. (Webhook ids are uuid strings, so this is bespoke rather than
// the int-keyed runLifecycle.)
func webhookIDByName(t *testing.T, ctx context.Context, lc *Client, name string) string {
	t.Helper()
	raw, err := lc.ListWebhookCards(ctx)
	for _, o := range objects(t, "webhook-cards", raw, err) {
		if strField("name")(o) == name {
			return strField("identifier")(o)
		}
	}
	return ""
}

// TestLiveWebhookCRUD runs the full create -> list -> read -> edit -> read ->
// delete -> list lifecycle on a throwaway webhook bound to one real environment.
// Write-gated; deletes the webhook on cleanup even on failure.
func TestLiveWebhookCRUD(t *testing.T) {
	lc, ctx := liveClient(t)
	requireWrite(t)
	env := firstEnvironment(t, ctx, lc)
	label := smokeLabel("webhook")

	// 1. baseline — absent.
	if id := webhookIDByName(t, ctx, lc, label); id != "" {
		t.Fatalf("smoke webhook %q unexpectedly already exists (id=%s)", label, id)
	}

	// 2. create. Webhook creation can be refused by the environment (e.g. a
	//    resource limit); when it is, skip the lifecycle rather than fail.
	if _, err := lc.CreateWebhook(ctx, map[string]any{
		"name": label, "description": "secopsctl smoke test", "defaultEnvironment": env,
	}); err != nil {
		t.Skipf("webhook create not permitted here; skipping lifecycle: %v", err)
	}

	// 3. list -> capture identifier; register cleanup immediately.
	id := webhookIDByName(t, ctx, lc, label)
	if id == "" {
		t.Fatalf("created webhook %q not found after create", label)
	}
	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		if _, err := lc.DeleteWebhook(ctx, id); err != nil {
			t.Logf("cleanup: could not delete throwaway webhook %q (%s): %v", label, id, err)
		}
	})

	// 4. read by id -> full object.
	raw, err := lc.GetWebhook(ctx, id)
	if err != nil {
		t.Fatalf("read webhook: %v", err)
	}
	var wh map[string]any
	if err := json.Unmarshal(raw, &wh); err != nil {
		t.Fatalf("decode webhook: %v", err)
	}
	if strField("name")(wh) != label {
		t.Fatalf("read#1: name mismatch: got %q want %q", strField("name")(wh), label)
	}

	// 5. edit — round-trip the read object with a new name (carries identifier +
	//    apiKey + mapping rules so the PUT is a faithful update).
	wh["name"] = label + "-edited"
	if _, ok := wh["identifier"]; !ok {
		wh["identifier"] = id
	}
	if _, err := lc.UpdateWebhook(ctx, wh); err != nil {
		t.Fatalf("update webhook: %v", err)
	}

	// 6. read -> verify the edit.
	if got := webhookIDByName(t, ctx, lc, label+"-edited"); got != id {
		t.Fatalf("read#2: edited webhook not found by new name (got id=%q want %q)", got, id)
	}

	// 7. delete.
	if _, err := lc.DeleteWebhook(ctx, id); err != nil {
		t.Fatalf("delete webhook: %v", err)
	}
	deleted = true

	// 8. list -> gone.
	if got := webhookIDByName(t, ctx, lc, label+"-edited"); got != "" {
		t.Fatalf("webhook %s still present after delete", id)
	}
}
