package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"danny.vn/secops/soar/legacy"
)

// Integration INSTANCES are not config-as-code reconcilable: an instance has
// Create + Delete but no Update endpoint, and the create body
// ({integrationIdentifier, environment}) does not round-trip from any read shape.
// So they are operated imperatively here (create/delete verbs), while reads stay
// on the raw `soar legacy call` lane. Both verbs are live-guarded (dry-run default).

// PushSOARIntegrationCreate creates a new, UNCONFIGURED (inert) instance of an
// already-installed integration in one environment. A fresh instance carries no
// credentials and runs no actions until configured, so create is low-blast. A
// singleton integration that allows only its default instance returns an error;
// that is surfaced, not retried. Guarded: dry-run previews; a real create needs
// assumeYes.
func PushSOARIntegrationCreate(ctx context.Context, lc *legacy.Client, integrationID, env string, dryRun, assumeYes bool, w io.Writer) error {
	liveBanner(w, "CREATE SOAR integration instance")
	fmt.Fprintf(w, "Integration: %s\n  environment=%s (new instance starts unconfigured/inert)\n\n", integrationID, env)
	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API calls made. Re-run without --dry-run to create.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintln(w, "Refusing to create without confirmation (pass --yes). Aborted.")
		return nil
	}

	raw, err := lc.CreateIntegrationInstance(ctx, map[string]any{
		"integrationIdentifier": integrationID, "environment": env,
	})
	if err != nil {
		return err
	}
	id := jsonField(raw, "identifier")
	fmt.Fprintf(w, "Done. Created integration instance %s (configure it before use).\n", orValue(id, "(id not echoed)"))
	return nil
}

// PushSOARIntegrationDelete deletes one integration instance. It resolves the full
// instance object first (DeleteIntegrationInstance takes the whole object as a
// body, not a bare id) by listing the integration's instances in the environment
// and matching the id, warns if playbooks depend on it, then deletes. Guarded;
// cannot be undone.
func PushSOARIntegrationDelete(ctx context.Context, lc *legacy.Client, integrationID, env, instanceID string, dryRun, assumeYes bool, w io.Writer) error {
	raw, err := lc.ListOptionalIntegrationInstances(ctx, map[string]any{
		"environments": []any{env}, "integrationIdentifier": integrationID,
	})
	if err != nil {
		return err
	}
	items, err := decodeRawList(raw)
	if err != nil {
		return fmt.Errorf("decode integration instances: %w", err)
	}
	var target json.RawMessage
	for _, it := range items {
		if jsonField(it, "identifier") == instanceID {
			target = it
			break
		}
	}
	if target == nil {
		return fmt.Errorf("integration instance %q not found for integration %q in environment %q", instanceID, integrationID, env)
	}

	liveBanner(w, "DELETE SOAR integration instance")
	fmt.Fprintf(w, "Instance: %s (integration %s, environment %s)\n", instanceID, integrationID, env)
	if used, derr := lc.GetPlaybooksUsingInstance(ctx, instanceID); derr == nil {
		if names, _ := decodeRawList(used); len(names) > 0 {
			fmt.Fprintf(w, "  WARNING: %d playbook(s) use this instance; deleting it will break them.\n", len(names))
		}
	}
	fmt.Fprintln(w)
	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API calls made. Re-run without --dry-run to delete.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintln(w, "Refusing to delete without confirmation (pass --yes). Aborted.")
		return nil
	}

	var obj any
	if err := json.Unmarshal(target, &obj); err != nil {
		return err
	}
	if _, err := lc.DeleteIntegrationInstance(ctx, obj); err != nil {
		return err
	}
	fmt.Fprintf(w, "Done. Deleted integration instance %s.\n", instanceID)
	return nil
}

// orValue returns s when non-empty, else fallback.
func orValue(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
