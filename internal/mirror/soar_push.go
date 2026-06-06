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
