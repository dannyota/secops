package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
)

// AI-assist case reads (Wave 56): the structured Gemini case summary, the
// priority-count queue metric, and per-alert AI recommendations — Google's own
// AI pre-digest, far cheaper for an agent than re-deriving a summary from the
// full case payload. Generation is asynchronous; these verbs poll until the
// result settles.

// aiPoll re-runs poll until settled() or the budget runs out (~5 minutes —
// generation can genuinely take minutes after a case changes). The caller has
// already made the initial request; a stderr heartbeat keeps long waits visible.
func aiPoll(poll func() error, settled func() bool) error {
	const (
		maxPolls = 100
		interval = 3 * time.Second
	)
	for i := range maxPolls {
		if settled() {
			return nil
		}
		if i > 0 && i%10 == 0 {
			fmt.Fprintf(os.Stderr, "still generating… (%s elapsed)\n", time.Duration(i)*interval)
		}
		time.Sleep(interval)
		if err := poll(); err != nil {
			return err
		}
	}
	return fmt.Errorf("generation did not settle after %s — re-run to keep polling", time.Duration(maxPolls)*interval)
}

func newCaseSummarizeCmd() *cobra.Command {
	var (
		caseID  int
		refresh bool
	)
	cmd := &cobra.Command{
		Use:   "summarize --id N [--refresh]",
		Short: "Read-only: the structured AI summary of a case (reasons, next steps)",
		Long: "Fetch (generating on first request) Google's AI summary of a case —\n" +
			"summary, reasons, and suggested next steps as structured fields. Generation\n" +
			"is asynchronous; the command polls until the summary settles. An existing or\n" +
			"in-flight summary is never re-kicked — pass --refresh to force a NEW\n" +
			"generation (e.g. after the case changed, or a prior generation errored).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			id := fmt.Sprintf("%d", caseID)
			var s *soar.CaseSummary
			poll := func(first bool) func() error {
				return func() error {
					res, err := c.GetOrCreateCaseSummary(ctx, id, first)
					if err != nil {
						return err
					}
					s = res
					return nil
				}
			}
			// Pure poll first: an existing (or in-flight) summary must not be
			// re-kicked — isFirstRequest=true starts a NEW generation. Only a
			// case with no summary state at all (or an explicit --refresh) gets
			// the initial kick — which is an AI generation, so read-only mode
			// refuses it (polling an existing summary stays free).
			if err := poll(false)(); err != nil {
				return err
			}
			if refresh || (!s.Settled() && s.State == "") {
				if err := refuseAIGenerationIfReadOnly(fmt.Sprintf("case %d summary generation", caseID)); err != nil {
					return err
				}
				if err := poll(true)(); err != nil {
					return err
				}
			}
			if err := aiPoll(poll(false), func() bool { return s.Settled() }); err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, s.Raw)
			}
			if s.State == soar.SummaryStateError {
				return fmt.Errorf("summary generation failed (state ERROR)")
			}
			fmt.Fprintf(os.Stdout, "Case %d — AI summary\n\n%s\n", caseID, orDash(s.Summary))
			if len(s.Reasons) > 0 {
				fmt.Fprintln(os.Stdout, "\nReasons:")
				for _, r := range s.Reasons {
					fmt.Fprintf(os.Stdout, "  - %s\n", r)
				}
			}
			if len(s.NextSteps) > 0 {
				fmt.Fprintln(os.Stdout, "\nNext steps:")
				for _, n := range s.NextSteps {
					fmt.Fprintf(os.Stdout, "  - %s\n", n)
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&caseID, "id", 0, "SOAR case id (required)")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "force a new generation (an existing summary is otherwise returned as-is)")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newCaseCountsCmd() *cobra.Command {
	var filter string
	cmd := &cobra.Command{
		Use:   "counts [--filter <expr>]",
		Short: "Read-only: case counts grouped by priority for a filter set",
		Long: "One-call triage-queue metric (cases:countPriorities). --filter is the\n" +
			"server-side expression the API requires, e.g. \"status=OPEN\".",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := baseContext()
			// Two-host surface: try the SOAR host (the cases collection's home),
			// fall back to the chronicle host where some case verbs answer
			// instead. Both errors surface on a dual failure — the wrong-host vs
			// outage diagnosis needs both signals.
			var soarErr error
			if c, err := newSOARClient(); err == nil {
				raw, err := c.CountCasePriorities(ctx, filter)
				if err == nil {
					return writeRawJSON(os.Stdout, raw)
				}
				soarErr = err
			}
			cc, err := newChronicleClient()
			if err != nil {
				return err
			}
			raw, err := cc.CountCasePriorities(ctx, filter)
			if err != nil {
				if soarErr != nil {
					return fmt.Errorf("soar host: %w (chronicle host also failed: %v)", soarErr, err) //nolint:errorlint // the chronicle error is annotation only
				}
				return err
			}
			return writeRawJSON(os.Stdout, raw)
		},
	}
	cmd.Flags().StringVar(&filter, "filter", "status=OPEN", `server-side filter (required by the API)`)
	return cmd
}

func newCaseAlertRecommendCmd() *cobra.Command {
	var (
		caseID int
		alert  string
	)
	cmd := &cobra.Command{
		Use:   "recommend --id N --alert <identifier-or-numeric-id>",
		Short: "Generate + fetch the AI recommendation for one alert in a case",
		Long: "Trigger AI recommendation generation for a case alert and poll until it\n" +
			"settles — Google's recommended next actions for the alert as structured\n" +
			"data. Each run starts a generation server-side (refused in read-only mode).\n" +
			"--alert takes the alert identifier `soar case get` prints (resolved to the\n" +
			"numeric caseAlert id the API wants) or the numeric id directly.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			id := fmt.Sprintf("%d", caseID)
			alertID, err := resolveCaseAlertID(ctx, c, id, alert)
			if err != nil {
				return err
			}
			if err := refuseAIGenerationIfReadOnly(fmt.Sprintf("alert %s recommendation generation", alert)); err != nil {
				return err
			}
			recID, err := c.CreateCaseAlertRecommendation(ctx, id, alertID)
			if err != nil {
				return err
			}
			var rec *soar.AlertRecommendation
			fetch := func() error {
				res, err := c.FetchCaseAlertRecommendation(ctx, id, alertID, recID)
				if err != nil {
					return err
				}
				rec = res
				return nil
			}
			if err := fetch(); err != nil {
				return err
			}
			if err := aiPoll(fetch, func() bool { return rec.Settled() }); err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, rec.Raw)
			}
			if rec.State == "FAILED" {
				return fmt.Errorf("recommendation generation failed (state FAILED)")
			}
			fmt.Fprintf(os.Stdout, "AI recommendation for alert %s (case %d):\n\n%s\n", alert, caseID, orDash(rec.Recommendation))
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "id", 0, "SOAR case id (required)")
	f.StringVar(&alert, "alert", "", "alert identifier (from `soar case get`) or numeric caseAlert id (required)")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("alert")
	return cmd
}

// resolveCaseAlertID maps an alert reference — a numeric caseAlert id, or the
// string identifier the legacy lane prints — to the numeric id the modern
// AI verbs key on, via the case's caseAlerts sub-collection.
func resolveCaseAlertID(ctx context.Context, c *soar.Client, caseID, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if _, err := strconv.Atoi(ref); err == nil {
		return ref, nil
	}
	alerts, err := c.ListCaseAlerts(ctx, caseID)
	if err != nil {
		return "", err
	}
	for i := range alerts {
		a := &alerts[i]
		if strings.EqualFold(a.Identifier, ref) || strings.EqualFold(a.AlertGroupIdentifier, ref) {
			return a.ID.String(), nil
		}
	}
	return "", fmt.Errorf("case %s has no alert %q (identifiers come from `soar case get %s`)", caseID, ref, caseID)
}
