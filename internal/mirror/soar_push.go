package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

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
	if err := legacy.ValidatePlaybookForSave(json.RawMessage(data)); err != nil {
		return err
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

	resp, err := lc.SavePlaybook(ctx, json.RawMessage(data))
	if err != nil {
		return err
	}
	if dropped := detectDroppedSteps(json.RawMessage(data), resp); len(dropped) > 0 {
		fmt.Fprintf(os.Stderr, "warning: server dropped %d step(s) — the following steps were not saved:\n", len(dropped))
		for _, name := range dropped {
			fmt.Fprintf(os.Stderr, "  - %s\n", name)
		}
		fmt.Fprintln(os.Stderr, "This typically happens when a step has an identifier the server does not recognize (e.g. a new step added to a previously-saved playbook).")
	}
	fmt.Fprintln(w, "Done. Playbook saved (re-pull to capture the new version identifier).")
	return nil
}

// playbookSteps is the minimal step shape needed to compare submitted vs
// response step sets. Only instanceName and identifier are read.
type playbookSteps struct {
	Steps []struct {
		InstanceName string `json:"instanceName"`
		Identifier   string `json:"identifier"`
	} `json:"steps"`
}

// detectDroppedSteps compares the steps in submitted against those in response
// (both are playbook JSON bodies) and returns the instanceNames of any steps
// present in submitted but absent from response. Comparison is by identifier
// (the server's key); when an identifier is empty (new step), instanceName is
// used as fallback. Returns nil when no steps were dropped or when either body
// cannot be parsed.
func detectDroppedSteps(submitted, response json.RawMessage) []string {
	var sub, resp playbookSteps
	if err := json.Unmarshal(submitted, &sub); err != nil {
		return nil
	}
	if err := json.Unmarshal(response, &resp); err != nil {
		return nil
	}
	if len(sub.Steps) == 0 || len(resp.Steps) >= len(sub.Steps) {
		return nil
	}

	// Build a set of identifiers (and instanceNames) present in the response.
	respIDs := make(map[string]struct{}, len(resp.Steps))
	respNames := make(map[string]struct{}, len(resp.Steps))
	for _, s := range resp.Steps {
		if s.Identifier != "" {
			respIDs[s.Identifier] = struct{}{}
		}
		if s.InstanceName != "" {
			respNames[s.InstanceName] = struct{}{}
		}
	}

	var dropped []string
	for _, s := range sub.Steps {
		if s.Identifier != "" {
			if _, ok := respIDs[s.Identifier]; ok {
				continue
			}
		} else if s.InstanceName != "" {
			if _, ok := respNames[s.InstanceName]; ok {
				continue
			}
		}
		label := s.InstanceName
		if label == "" {
			label = s.Identifier
		}
		if label == "" {
			label = "(unnamed step)"
		}
		dropped = append(dropped, label)
	}
	return dropped
}
