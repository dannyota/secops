package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"danny.vn/secops/soar"
	"danny.vn/secops/soar/legacy"
)

// PushSOARBulkClose bulk-closes the given SOAR cases (legacy external API). It is
// guarded: dry-run prints the preview and stops; a real close requires
// assumeYes. Returns the number of cases requested for closing (0 on dry-run).
func PushSOARBulkClose(ctx context.Context, lc *legacy.Client, ids []int, reason legacy.CloseReason, rootCause, comment string, dryRun, assumeYes bool, w io.Writer) (int, error) {
	if len(ids) == 0 {
		fmt.Fprintln(w, "Nothing to close -- no case ids given.")
		return 0, nil
	}

	liveBanner(w, fmt.Sprintf("BULK-CLOSE %d SOAR case(s)", len(ids)))
	fmt.Fprintf(w, "About to close %d case(s) (reason=%d, rootCause=%q):\n  %v\n\n",
		len(ids), int(reason), rootCause, ids)

	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API calls made. Re-run without --dry-run to close.")
		return 0, nil
	}
	if !assumeYes {
		fmt.Fprintf(w, "Refusing to close %d case(s) without confirmation (pass --yes). Aborted.\n", len(ids))
		return 0, nil
	}

	if _, err := lc.BulkCloseCases(ctx, legacy.BulkCloseRequest{
		CasesIDs: ids, CloseReason: reason, RootCause: rootCause, CloseComment: comment,
	}); err != nil {
		return 0, err
	}
	fmt.Fprintf(w, "Done. Requested close of %d case(s).\n", len(ids))
	return len(ids), nil
}

// PushSOARConnectorPatch applies an edited connector snapshot (the YAML produced
// by PullSOARConnectors) back to the live instance. It patches enabled,
// intervalSeconds, and parameters (masked secrets are passed through unchanged by
// the server). Guarded: dry-run previews; a real patch requires assumeYes.
func PushSOARConnectorPatch(ctx context.Context, c *soar.Client, file string, dryRun, assumeYes bool, w io.Writer) error {
	var snap connectorSnapshot
	if err := readYAMLFile(file, &snap); err != nil {
		return err
	}
	integration, connector, instance, ok := parseInstanceName(snap.Name, "connectors", "connectorInstances")
	if !ok {
		return fmt.Errorf("cannot parse integration/connector/instance from name %q", snap.Name)
	}

	liveBanner(w, "PATCH SOAR connector instance")
	fmt.Fprintf(w, "Instance: %s\n  enabled=%v intervalSeconds=%d (+%d parameter(s))\n\n",
		snap.DisplayName, snap.Enabled, snap.IntervalSeconds, len(snap.Parameters))
	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API calls made. Re-run without --dry-run to apply.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintln(w, "Refusing to patch without confirmation (pass --yes). Aborted.")
		return nil
	}

	body := map[string]any{
		"enabled":         snap.Enabled,
		"intervalSeconds": snap.IntervalSeconds,
		"parameters":      snap.Parameters,
	}
	if _, err := c.UpdateConnectorInstance(ctx, integration, connector, instance, body,
		"enabled", "intervalSeconds", "parameters"); err != nil {
		return err
	}
	fmt.Fprintf(w, "Done. Patched connector instance %s.\n", snap.DisplayName)
	return nil
}

// PushSOARJobPatch applies an edited job snapshot back to the live instance
// (enabled, cronSchedule, intervalSeconds, parameters). Guarded.
func PushSOARJobPatch(ctx context.Context, c *soar.Client, file string, dryRun, assumeYes bool, w io.Writer) error {
	var snap jobSnapshot
	if err := readYAMLFile(file, &snap); err != nil {
		return err
	}
	integration, job, instance, ok := parseInstanceName(snap.Name, "jobs", "jobInstances")
	if !ok {
		return fmt.Errorf("cannot parse integration/job/instance from name %q", snap.Name)
	}

	liveBanner(w, "PATCH SOAR job instance")
	fmt.Fprintf(w, "Instance: %s\n  enabled=%v cronSchedule=%q intervalSeconds=%d\n\n",
		snap.DisplayName, snap.Enabled, snap.CronSchedule, snap.IntervalSeconds)
	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API calls made. Re-run without --dry-run to apply.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintln(w, "Refusing to patch without confirmation (pass --yes). Aborted.")
		return nil
	}

	body := map[string]any{
		"enabled":         snap.Enabled,
		"cronSchedule":    snap.CronSchedule,
		"intervalSeconds": snap.IntervalSeconds,
		"parameters":      snap.Parameters,
	}
	if _, err := c.UpdateJobInstance(ctx, integration, job, instance, body,
		"enabled", "cronSchedule", "intervalSeconds", "parameters"); err != nil {
		return err
	}
	fmt.Fprintf(w, "Done. Patched job instance %s.\n", snap.DisplayName)
	return nil
}

// PushSOARPlaybookSave saves a playbook definition (a JSON file, typically one
// pulled by PullSOARPlaybooks and edited) via the v1alpha bridge. SavePlaybook
// mints a NEW version identifier and is a whole-body REPLACE. Guarded.
func PushSOARPlaybookSave(ctx context.Context, lc *legacy.Client, file string, dryRun, assumeYes bool, w io.Writer) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if !json.Valid(data) {
		return fmt.Errorf("%s is not valid JSON", file)
	}

	liveBanner(w, "SAVE SOAR playbook (whole-body replace; mints a new version)")
	fmt.Fprintf(w, "Source: %s (%d bytes)\n\n", file, len(data))
	if dryRun {
		fmt.Fprintln(w, "DRY RUN -- no API calls made. Re-run without --dry-run to save.")
		return nil
	}
	if !assumeYes {
		fmt.Fprintln(w, "Refusing to save without confirmation (pass --yes). Aborted.")
		return nil
	}

	if _, err := lc.SavePlaybook(ctx, json.RawMessage(data)); err != nil {
		return err
	}
	fmt.Fprintln(w, "Done. Playbook saved (re-pull to capture the new version identifier).")
	return nil
}

// parseInstanceName extracts (integration, definition, instance) from a SOAR
// resource name of the form .../integrations/<i>/<defKey>/<d>/<instKey>/<id>.
func parseInstanceName(name, defKey, instKey string) (integration, definition, instance string, ok bool) {
	parts := strings.Split(name, "/")
	idx := func(key string) int {
		for i, p := range parts {
			if p == key && i+1 < len(parts) {
				return i + 1
			}
		}
		return -1
	}
	ii, di, ni := idx("integrations"), idx(defKey), idx(instKey)
	if ii < 0 || di < 0 || ni < 0 {
		return "", "", "", false
	}
	return parts[ii], parts[di], parts[ni], true
}
