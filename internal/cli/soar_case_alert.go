package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

// `soar case alert` — per-ALERT triage inside a case. SOAR groups several alerts
// into one case; queue hygiene is often alert-level: close one false-positive
// alert without closing the case, re-prioritize a single alert, split a
// mis-grouped alert out (`move`, the inverse of `case merge`), or reopen one.
// Every verb takes the case id (--id) plus the alert identifier (--alert, the
// value `soar case get` prints per alert) and rides the standard guard. Request
// bodies are the typed soar/legacy structs, shared with the live write smoke.

func newCaseAlertCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alert <verb>",
		Short: "Manage per-alert triage inside a case: close, priority, move, reopen (guarded)",
		Long: "Act on a single alert WITHIN a case — close it (the case stays open),\n" +
			"change its priority, move it to another (or a new) case, or reopen it.\n" +
			"--alert takes the alert identifier shown by `soar case get`.",
	}
	cmd.AddCommand(
		newCaseAlertCloseCmd(),
		newCaseAlertPriorityCmd(),
		newCaseAlertMoveCmd(),
		newCaseAlertReopenCmd(),
		newCaseAlertRecommendCmd(),
	)
	return cmd
}

// alertGuardFlags wires the per-alert verb flag set: --id (the case, required),
// --alert (the alert identifier, required here — unlike the case verbs where it
// is an optional scope), and the standard --dry-run/--yes gate.
func alertGuardFlags(cmd *cobra.Command, caseID *int, alert *string, dryRun, yes *bool) {
	f := cmd.Flags()
	f.IntVar(caseID, "id", 0, "SOAR case id (required)")
	f.StringVar(alert, "alert", "", "alert identifier, as printed by 'soar case get' (required)")
	guardRunFlags(cmd, dryRun, yes)
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("alert")
}

// parseAlertCloseReason maps a close-reason name to the PascalCase token the
// alert-close family puts on the wire. Unlike case close, the alert close enum
// has no Unknown (the swagger documents Malicious, NotMalicious, Maintenance,
// Inconclusive — case-sensitive).
func parseAlertCloseReason(s string) (string, error) {
	cr, err := parseCloseReason(s)
	if err != nil || cr == legacy.CloseUnknown {
		return "", fmt.Errorf("invalid alert close reason %q (use malicious|not-malicious|maintenance|inconclusive)", s)
	}
	return cr.String(), nil
}

// parseAlertUsefulness maps the optional usefulness stat to its wire token
// (None | NotUseful | Useful, per the swagger description).
func parseAlertUsefulness(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return "", nil
	case "none":
		return "None", nil
	case "useful":
		return "Useful", nil
	case "not-useful", "notuseful", "not_useful":
		return "NotUseful", nil
	}
	return "", fmt.Errorf("invalid --usefulness %q (use none|useful|not-useful)", s)
}

func newCaseAlertCloseCmd() *cobra.Command {
	var (
		caseID                     int
		alert, reason              string
		rootCause, comment, useful string
		dryRun, yes                bool
	)
	cmd := &cobra.Command{
		Use:   "close --id N --alert <ident> --reason <enum>",
		Short: "Close one alert in a case (the case stays open)",
		Long: "Close a single alert without closing its case — the surgical false-positive\n" +
			"path. --reason: malicious | not-malicious | maintenance | inconclusive\n" +
			"(alerts take no 'unknown'). --usefulness feeds the alert-usefulness stats.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := parseAlertCloseReason(reason)
			if err != nil {
				return err
			}
			u, err := parseAlertUsefulness(useful)
			if err != nil {
				return err
			}
			body := legacy.CloseAlertRequest{
				SourceCaseID:    caseID,
				AlertIdentifier: alert,
				Reason:          r,
				RootCause:       rootCause,
				Comment:         comment,
				Usefulness:      u,
			}
			return caseAction(fmt.Sprintf("close alert %s in case %d (reason=%s)", alert, caseID, r), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.CloseAlert(ctx, body)
				})
		},
	}
	alertGuardFlags(cmd, &caseID, &alert, &dryRun, &yes)
	f := cmd.Flags()
	f.StringVar(&reason, "reason", "", "close reason: malicious | not-malicious | maintenance | inconclusive (required)")
	f.StringVar(&rootCause, "root-cause", "", "close root cause — list options with 'soar case values root-causes'")
	f.StringVar(&comment, "comment", "", "close comment (free-text note)")
	f.StringVar(&useful, "usefulness", "", "alert usefulness stat: none | useful | not-useful")
	_ = cmd.MarkFlagRequired("reason")
	return markJSON(cmd)
}

func newCaseAlertPriorityCmd() *cobra.Command {
	var (
		caseID          int
		alert, priority string
		dryRun, yes     bool
	)
	cmd := &cobra.Command{
		Use:   "priority --id N --alert <ident> --priority <level>",
		Short: "Change one alert's priority",
		Long: "Re-prioritize a single alert within its case. At apply time the alert's\n" +
			"name and current priority are resolved from the case (the request wants\n" +
			"both), so a wrong --alert fails on that read before any mutation.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := legacy.ParseCasePriority(priority)
			if err != nil {
				return err
			}
			// The preview shows the target; the alert's name and current priority
			// are resolved at APPLY time (a read), keeping dry runs credential-free
			// and failing cleanly on a wrong identifier before the mutation.
			preview := legacy.UpdateAlertPriorityRequest{
				CaseID: caseID, AlertIdentifier: alert, Priority: p,
			}
			action := fmt.Sprintf("set alert %s priority -> %s (case %d)", alert, p, caseID)
			return caseAction(action, preview, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					card, err := findCaseAlert(ctx, lc, caseID, alert)
					if err != nil {
						return nil, err
					}
					body := legacy.UpdateAlertPriorityRequest{
						CaseID: caseID,
						// The card's identifier is the server's canonical casing.
						AlertIdentifier:  card.Identifier,
						AlertName:        card.Name,
						PreviousPriority: legacy.CasePriority(card.Priority),
						Priority:         p,
					}
					return lc.UpdateAlertPriority(ctx, body)
				})
		},
	}
	alertGuardFlags(cmd, &caseID, &alert, &dryRun, &yes)
	cmd.Flags().StringVar(&priority, "priority", "", "target priority: informative|low|medium|high|critical (required)")
	_ = cmd.MarkFlagRequired("priority")
	return markJSON(cmd)
}

func newCaseAlertMoveCmd() *cobra.Command {
	var (
		caseID, to  int
		alert       string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "move --id N --alert <ident> [--to M]",
		Short: "Move one alert out of a case (to another case, or a new one)",
		Long: "Split a mis-grouped alert out of its case — the inverse of `soar case merge`.\n" +
			"With --to the alert moves into that existing case; without it the platform\n" +
			"creates a new case for the alert.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := legacy.MoveAlertRequest{
				AlertIdentifier: alert, SourceCaseID: caseID, DestinationCaseID: to,
			}
			dest := "a new case"
			if to != 0 {
				dest = fmt.Sprintf("case %d", to)
			}
			return caseAction(fmt.Sprintf("move alert %s from case %d -> %s", alert, caseID, dest), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.MoveAlertToNewCase(ctx, body)
				})
		},
	}
	alertGuardFlags(cmd, &caseID, &alert, &dryRun, &yes)
	cmd.Flags().IntVar(&to, "to", 0, "destination case id (omit to move the alert into a new case)")
	return markJSON(cmd)
}

func newCaseAlertReopenCmd() *cobra.Command {
	var (
		caseID      int
		alert       string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "reopen --id N --alert <ident>",
		Short: "Reopen one closed alert in a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := legacy.ReopenAlertRequest{CaseID: caseID, AlertIdentifier: alert}
			return caseAction(fmt.Sprintf("reopen alert %s in case %d", alert, caseID), body, dryRun, yes,
				func(ctx context.Context, lc *legacy.Client) (legacy.RawJSON, error) {
					return lc.ReopenAlert(ctx, body)
				})
		},
	}
	alertGuardFlags(cmd, &caseID, &alert, &dryRun, &yes)
	return markJSON(cmd)
}

// findCaseAlert fetches the case and returns the alert card matching the given
// identifier, with a clean client-side error when the case has no such alert.
func findCaseAlert(ctx context.Context, lc *legacy.Client, caseID int, identifier string) (*soarAlertCard, error) {
	raw, err := lc.GetCaseFullDetails(ctx, caseID)
	if err != nil {
		return nil, err
	}
	var cs soarCaseFull
	if err := json.Unmarshal(raw, &cs); err != nil {
		return nil, fmt.Errorf("decode case details: %w", err)
	}
	for i := range cs.Alerts {
		if strings.EqualFold(cs.Alerts[i].Identifier, identifier) {
			return &cs.Alerts[i], nil
		}
	}
	return nil, fmt.Errorf("case %d has no alert %q (list identifiers with `soar case get %d`)", caseID, identifier, caseID)
}
