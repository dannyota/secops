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
	drift := detectSaveDrift(json.RawMessage(data), resp)
	if len(drift) > 0 {
		fmt.Fprintln(w, "ERROR: server silently modified the saved playbook:")
		for _, msg := range drift {
			fmt.Fprintf(w, "  - %s\n", msg)
		}
		fmt.Fprintln(w, "The save succeeded but the result differs from what was submitted. Re-pull to inspect.")
		return fmt.Errorf("playbook saved with server-side drift (%d issue(s))", len(drift))
	}
	fmt.Fprintln(w, "Done. Playbook saved (re-pull to capture the new version identifier).")
	return nil
}

// playbookShape is the minimal playbook shape for comparing submitted vs
// response bodies: steps (by instanceName) and relation count.
type playbookShape struct {
	Steps []struct {
		InstanceName string `json:"instanceName"`
		Identifier   string `json:"identifier"`
	} `json:"steps"`
	Relations []json.RawMessage `json:"stepsRelations"`
}

// detectSaveDrift compares the submitted playbook body against the server's
// response and returns human-readable descriptions of any drift. Checks:
// (1) steps present in submitted but absent from response (by instanceName),
// (2) relation count decrease. Returns nil when no drift is detected.
func detectSaveDrift(submitted, response json.RawMessage) []string {
	var sub, resp playbookShape
	if err := json.Unmarshal(submitted, &sub); err != nil {
		return nil
	}
	if err := json.Unmarshal(response, &resp); err != nil {
		return nil
	}

	var issues []string

	if len(sub.Steps) > 0 {
		respNames := make(map[string]struct{}, len(resp.Steps))
		for _, s := range resp.Steps {
			if s.InstanceName != "" {
				respNames[s.InstanceName] = struct{}{}
			}
		}
		for _, s := range sub.Steps {
			label := s.InstanceName
			if label == "" {
				label = s.Identifier
			}
			if label == "" {
				continue
			}
			if _, ok := respNames[label]; !ok {
				issues = append(issues, fmt.Sprintf("step %q was dropped", label))
			}
		}
	}

	if subRel, respRel := len(sub.Relations), len(resp.Relations); subRel > 0 && respRel < subRel {
		issues = append(issues, fmt.Sprintf("%d of %d relation(s) were dropped", subRel-respRel, subRel))
	}

	return issues
}
