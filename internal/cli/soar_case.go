package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

// Lane 3 — imperative SOAR case actions. These are per-case verbs (no
// desired-state file), so they are a command tree, not a reconcile surface. Each
// request body is modeled on the Siemplify external-API schema
// (third_party/siemplify-swagger.json): a numeric caseId, an optional
// alertIdentifier, and the verb's payload. Every verb is a LIVE mutation, so it
// shares the dry-run / --yes / banner guard with `soar push`.

// caseVerbs builds the full per-case command tree (reads + guarded mutations).
// A case is ONE record; the same tree backs both the canonical top-level `cases`
// command and its hidden back-compat `soar case` alias. It is built fresh on each
// call so the two registrations never share cobra state. The verbs run on the
// SOAR host (AppKey, the reliable lane); caseId is the SOAR integer id, not the
// SIEM UUID — bridge a UUID to its id with `cases soar-id`.
func caseVerbs() []*cobra.Command {
	return []*cobra.Command{
		newCaseListCmd(),
		newCaseGetCmd(),
		newCaseRunActionCmd(),
		newCaseSimulationCmd(),
		newCaseChatCmd(),
		newCaseCustomFieldsCmd(),
		newCaseWallCmd(),
		newCaseContextCmd(),
		newCaseAssignCmd(),
		newCaseRenameCmd(),
		newCaseStageCmd(),
		newCaseTagCmd(false),
		newCaseTagCmd(true),
		newCaseDescribeCmd(),
		newCaseImportanceCmd(),
		newCasePriorityCmd(),
		newCaseCloseCmd(),
		newCaseReopenCmd(),
		newCaseMergeCmd(),
		newCaseCommentCmd(),
		newCaseIncidentCmd(),
		newCaseReportCmd(),
		newCaseAlertCmd(),
		newCaseValuesCmd(),
		func() *cobra.Command { c := newCaseSummarizeCmd(); c.Hidden = true; return c }(),
		newCaseCountsCmd(),
		newCaseOverviewCmd(),
		newCaseWorkloadCmd(),
		newCaseAgingCmd(),
		newCaseStatsCmd(),
		newCaseTaskCmd(),
		newCaseEvidenceCmd(),
		newCasePlaybookErrorsCmd(),
	}
}

// guardRunFlags wires the shared --dry-run/--yes apply gate onto a verb (the
// piece of caseGuardFlags that every guarded verb shares regardless of its
// id/alert flag shape).
func guardRunFlags(cmd *cobra.Command, dryRun, yes *bool) {
	f := cmd.Flags()
	f.BoolVar(dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
}

// caseBody seeds a request body with the case id and (when set) the alert
// identifier — the two fields nearly every case verb shares.
func caseBody(caseID int, alert string) map[string]any {
	body := map[string]any{"caseId": caseID}
	if alert != "" {
		body["alertIdentifier"] = alert
	}
	return body
}

// caseAction is the shared guarded executor: it previews the body under a LIVE
// banner, stops on a dry run, refuses without confirmation, then calls do.
// body is the request payload — a map or one of the typed soar/legacy request
// structs — rendered verbatim in the preview and the --json result.
func caseAction(action string, body any, dryRun, yes bool, do func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error)) error {
	dr, ay := soarGuard(action, dryRun, yes)

	// emit prints the machine-readable result under --json so the dry-run → --yes →
	// verify loop is scriptable. applied = the mutation actually ran.
	emit := func(applied bool) error {
		if !jsonOut {
			return nil
		}
		return emitJSON(struct {
			Action  string `json:"action"`
			Request any    `json:"request"`
			DryRun  bool   `json:"dry_run"`
			Applied bool   `json:"applied"`
			OK      bool   `json:"ok"`
		}{Action: action, Request: body, DryRun: dr, Applied: applied, OK: true})
	}

	if !jsonOut {
		w := os.Stdout
		bar := strings.Repeat("!", 72)
		fmt.Fprintln(w, bar)
		fmt.Fprintln(w, "!! LIVE SOAR case action against a PRODUCTION tenant !!")
		fmt.Fprintf(w, "!! Action: %s\n", action)
		fmt.Fprintln(w, bar)
		pretty, _ := json.MarshalIndent(body, "", "  ")
		fmt.Fprintf(w, "Request body:\n%s\n\n", pretty)
	}

	if dr {
		if !jsonOut {
			fmt.Fprintln(os.Stdout, "DRY RUN — no API call made. Re-run without --dry-run to apply.")
		}
		return emit(false)
	}
	if !ay {
		if !jsonOut {
			fmt.Fprintf(os.Stdout, "Refusing to %s without confirmation (pass --yes). Aborted.\n", action)
		}
		return emit(false)
	}
	// Build the client only when actually applying — dry runs need no credentials.
	lc, err := newSOARLegacyClient()
	if err != nil {
		return err
	}
	if _, err := do(baseContext(), lc); err != nil {
		return err
	}
	if !jsonOut {
		fmt.Fprintf(os.Stdout, "Done: %s.\n", action)
	}
	return emit(true)
}

// caseGuardFlags wires the shared --id/--alert/--dry-run/--yes flags onto a verb.
func caseGuardFlags(cmd *cobra.Command, caseID *int, alert *string, dryRun, yes *bool, withAlert bool) {
	f := cmd.Flags()
	f.IntVar(caseID, "id", 0, "SOAR case id (required)")
	if withAlert {
		f.StringVar(alert, "alert", "", "scope the action to one alert in the case instead of the whole case "+
			"(alert identifier from 'soar case get <id>'); omit to act on the case")
	}
	guardRunFlags(cmd, dryRun, yes)
	_ = cmd.MarkFlagRequired("id")
}
