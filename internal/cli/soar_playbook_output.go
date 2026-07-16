package cli

// soar_playbook_output.go — summary/results/result/python-logs command constructors and JSON print/scan helpers.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

func newSOARPlaybookSummaryCmd() *cobra.Command {
	var (
		caseID         int
		alert          string
		definition     string
		playbook       string
		fetchSteps     bool
		collapseBlocks bool
		showErrors     bool
		steps          bool
	)
	cmd := &cobra.Command{
		Use:   "summary --case-id N --playbook <name>",
		Short: "Read a playbook run summary for a case (surfaces faulted steps)",
		Long: "Fetch a playbook workflow-instance summary and surface its FAULTED steps —\n" +
			"each failed step's action, error message, and a Cloud Logging deep-link — so a\n" +
			"failed run is triaged in-tool without digging through the raw payload.\n\n" +
			"The easy form needs only the case id and a playbook NAME:\n" +
			"  secopsctl playbooks summary --case-id 123 --playbook \"My Playbook\"\n" +
			"`--playbook` is resolved to its definition id via `playbooks list`, and the\n" +
			"alert identifier is read from the case automatically (use `--alert` when a case\n" +
			"has more than one alert, or `--definition` to pass the id directly). Uses the\n" +
			"two-call pattern (list cards → get instance detail) for reliable results\n" +
			"across single- and multi-alert cases.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			if strings.TrimSpace(definition) == "" && strings.TrimSpace(playbook) != "" {
				if definition, err = resolvePlaybookDefinition(ctx, lc, playbook); err != nil {
					return err
				}
			}
			if strings.TrimSpace(alert) == "" {
				if alert, err = resolveCaseAlert(ctx, lc, caseID); err != nil {
					return err
				}
			}

			cardsBody := map[string]any{
				"caseId":          strconv.Itoa(caseID),
				"alertIdentifier": alert,
			}
			getCards := func(fn func(context.Context, any) (json.RawMessage, error)) (json.RawMessage, error) {
				return fn(ctx, cardsBody)
			}

			var cardsRaw json.RawMessage
			err = preferModern("playbooks summary (cards)",
				func() error {
					mc, merr := newSOARClient()
					if merr != nil {
						return merr
					}
					cardsRaw, merr = getCards(mc.GetWorkflowInstanceCards)
					return merr
				},
				func() error {
					cardsRaw, err = getCards(lc.GetWorkflowInstancesCards)
					return err
				},
			)
			if err != nil {
				return wrapWorkflowCards500(err, caseID, alert)
			}

			card, err := pickWorkflowCard(cardsRaw, definition)
			if err != nil {
				return err
			}
			if card == nil {
				if playbook != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "no playbook runs found for %q on case %d alert %s\n", playbook, caseID, truncate(alert, 40))
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "no playbook runs found on case %d alert %s\n", caseID, truncate(alert, 40))
				}
				return nil
			}

			if jsonOut && !fetchSteps {
				return writeRawJSON(os.Stdout, card.raw)
			}

			instanceBody := map[string]any{
				"caseId":                   strconv.Itoa(caseID),
				"alertIdentifier":          alert,
				"definitionIdentifier":     card.definitionID,
				"shouldFetchSteps":         fetchSteps,
				"collapseBlocks":           collapseBlocks,
				"loopsRequestedIterations": []any{},
			}

			var instanceRaw json.RawMessage
			err = preferModern("playbooks summary (instance)",
				func() error {
					mc, merr := newSOARClient()
					if merr != nil {
						return merr
					}
					instanceRaw, merr = mc.GetWorkflowInstance(ctx, instanceBody)
					return merr
				},
				func() error {
					instanceRaw, err = lc.GetWorkflowInstance(ctx, instanceBody)
					return err
				},
			)
			if err != nil {
				return err
			}

			if jsonOut {
				return writeRawJSON(os.Stdout, instanceRaw)
			}
			printWorkflowSummary(cmd.OutOrStdout(), instanceRaw, showErrors, steps)
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	f.StringVar(&playbook, "playbook", "", "playbook name — resolved to its definition id via 'playbooks list'")
	f.StringVar(&alert, "alert", "", "alert identifier (auto-resolved from the case when omitted)")
	f.StringVar(&definition, "definition", "", "playbook definition id (overrides --playbook)")
	f.BoolVar(&fetchSteps, "fetch-steps", true, "include step details")
	f.BoolVar(&collapseBlocks, "collapse-blocks", false, "collapse nested block details")
	f.BoolVar(&showErrors, "show-errors", false, "print full faulted-step error messages (default truncates)")
	f.BoolVar(&steps, "steps", false, "print the full per-step execution trace (every completed step, not just faulted ones) — for debugging a run that finished but did the wrong thing")
	_ = cmd.MarkFlagRequired("case-id")
	return markJSON(cmd)
}

type workflowCard struct {
	definitionID string
	name         string
	status       string
	raw          json.RawMessage
}

// pickWorkflowCard finds the matching card from the cards response. The
// response may be a top-level array or wrapped in {"payload": [...]}.
// When definition is set, matches by definitionIdentifier; otherwise returns
// the first card.
func pickWorkflowCard(cardsRaw json.RawMessage, definition string) (*workflowCard, error) {
	var cards []json.RawMessage
	if err := json.Unmarshal(cardsRaw, &cards); err != nil {
		var wrapper struct {
			Payload []json.RawMessage `json:"payload"`
		}
		if err2 := json.Unmarshal(cardsRaw, &wrapper); err2 == nil {
			cards = wrapper.Payload
		} else {
			return nil, fmt.Errorf("parse workflow cards: %w", err)
		}
	}
	if len(cards) == 0 {
		return nil, nil
	}
	for _, raw := range cards {
		m, ok := rawJSONObject(raw)
		if !ok {
			continue
		}
		id := rawScalarString(m["definitionIdentifier"])
		name := rawScalarString(m["name"])
		status := rawScalarString(m["status"])
		if definition != "" && !strings.EqualFold(id, definition) {
			continue
		}
		return &workflowCard{definitionID: id, name: name, status: status, raw: raw}, nil
	}
	return nil, nil
}

func newSOARPlaybookResultsCmd() *cobra.Command {
	var workflowInstanceID int
	cmd := &cobra.Command{
		Use:   "results --workflow-instance-id N",
		Short: "Read action results for a workflow instance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if workflowInstanceID <= 0 {
				return fmt.Errorf("--workflow-instance-id is required")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.PlaybookXGetActionResultsOfWFId(baseContext(), workflowInstanceID)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printActionResultsSummary(cmd.OutOrStdout(), raw)
			return nil
		},
	}
	cmd.Flags().IntVar(&workflowInstanceID, "workflow-instance-id", 0, "workflow instance id (required)")
	_ = cmd.MarkFlagRequired("workflow-instance-id")
	return markJSON(cmd)
}

func newSOARPlaybookResultCmd() *cobra.Command {
	var (
		caseID int
		id     string
	)
	cmd := &cobra.Command{
		Use:   "result --case-id N --action-result-id <id>",
		Short: "Read one action result from a case",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
			}
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("--action-result-id is required")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.ResourceGetActionResultsById(baseContext(), caseID, id)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printSingleActionResultSummary(cmd.OutOrStdout(), raw)
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	f.StringVar(&id, "action-result-id", "", "action result id (required)")
	_ = cmd.MarkFlagRequired("case-id")
	_ = cmd.MarkFlagRequired("action-result-id")
	return markJSON(cmd)
}

// wrapCloudLogging500 intercepts a legacy SOAR 500 from the Cloud Logging proxy
// (/logging/python) and returns a legible error with the correlation id and a
// pointer to the working triage alternative, instead of dumping the raw payload.
func wrapCloudLogging500(err error) error {
	var apiErr *legacy.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 500 {
		return err
	}
	cid := ""
	var body struct {
		CorrelationID string `json:"correlationId"`
	}
	if json.Unmarshal([]byte(apiErr.Body), &body) == nil && body.CorrelationID != "" {
		cid = body.CorrelationID
	}
	hint := "python execution logs are served from Cloud Logging and can 500 " +
		"on some instances (a backend/access condition, not a request-shape bug).\n" +
		"  Triage alternative: secopsctl playbooks summary --case-id N --playbook \"<name>\"\n" +
		"  (surfaces each faulted step's error + a per-step Logs Explorer deep-link)"
	if cid != "" {
		return fmt.Errorf("%s\n  correlation id (for SecOps support): %s", hint, cid)
	}
	return fmt.Errorf("%s", hint)
}

// wrapWorkflowCards500 intercepts the generic SOAR 500 (errorCode 2000) from
// the workflow-instance-cards call and returns a diagnostic hint. The API 500s
// when no workflow instance exists for the case+alert combo (playbook didn't
// fire, closed case, or multi-alert group mismatch).
func wrapWorkflowCards500(err error, caseID int, alert string) error {
	var apiErr *legacy.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 500 {
		return err
	}
	return fmt.Errorf("no workflow data for case %d / alert %s — the playbook may not "+
		"have run on this alert group, or the case is closed. The API returns a generic "+
		"500 for this condition (not a request-shape bug).\n"+
		"  Try: cases get %d --json to verify alert groups and playbook runs",
		caseID, truncate(alert, 40), caseID)
}

func newSOARPlaybookPythonLogsCmd() *cobra.Command {
	var (
		filter    string
		pageToken string
		sortOrder string
		pageSize  int
	)
	cmd := &cobra.Command{
		Use:   "python-logs",
		Short: "Read Python execution logs from SecOps (can 500; see `playbook summary` for triage)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			raw, err := lc.CloudLoggingGetPythonLogs(baseContext(), pythonLogsBody(filter, pageToken, sortOrder, pageSize))
			if err != nil {
				return wrapCloudLogging500(err)
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			printGenericItemsSummary(cmd.OutOrStdout(), "python log records", raw)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&filter, "filter", "", "SecOps log filter expression")
	f.IntVar(&pageSize, "page-size", 50, "maximum records to request")
	f.StringVar(&pageToken, "page-token", "", "page token from a previous response")
	f.StringVar(&sortOrder, "sort-order", "", "SecOps sort order")
	return markJSON(cmd)
}

// resolvePlaybookDefinition maps a playbook display name to its definition id via
// the live playbook list (case-insensitive exact match), so callers pass a name
// instead of the opaque GUID. It errors clearly on no/ambiguous match.
func resolvePlaybookDefinition(ctx context.Context, lc *legacy.Client, name string) (string, error) {
	cards, err := lc.ListPlaybooks(ctx, nil)
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	var ids []string
	for _, c := range cards {
		// Skip a blank identifier so a name match can't resolve to "" (which would
		// then surface as a generic 500 from the summary call).
		if id := strings.TrimSpace(c.Identifier); id != "" && strings.EqualFold(strings.TrimSpace(c.Name), name) {
			ids = append(ids, id)
		}
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("no playbook named %q (see `playbooks list`)", name)
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("%d playbooks named %q — pass --definition <id> to disambiguate", len(ids), name)
	}
}

// resolveCaseAlert returns the alert identifier a workflow-instance summary needs —
// alerts[].additionalProperties.alertGroupIdentifier from the full case detail. A
// single-alert case resolves automatically; a multi-alert case errors and lists
// the alerts so the caller can pass --alert.
func resolveCaseAlert(ctx context.Context, lc *legacy.Client, caseID int) (string, error) {
	raw, err := lc.GetCaseFullDetails(ctx, caseID)
	if err != nil {
		return "", err
	}
	var cd struct {
		Alerts []struct {
			Name                 string `json:"name"`
			HasWorkflows         bool   `json:"hasWorkflows"`
			AdditionalProperties struct {
				AlertGroupIdentifier string `json:"alertGroupIdentifier"`
			} `json:"additionalProperties"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(raw, &cd); err != nil {
		return "", fmt.Errorf("parse case %d details: %w", caseID, err)
	}
	// Dedupe by group id — grouped alerts share one alertGroupIdentifier, so a
	// "5 alerts" case is often a single workflow target.
	type alertRef struct {
		id    string
		hasWF bool
	}
	seen := map[string]bool{}
	var refs []alertRef
	for _, a := range cd.Alerts {
		id := strings.TrimSpace(a.AdditionalProperties.AlertGroupIdentifier)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		refs = append(refs, alertRef{id, a.HasWorkflows})
	}
	if len(refs) == 0 {
		return "", fmt.Errorf("case %d exposes no alert identifier — pass --alert <id>", caseID)
	}
	if len(refs) == 1 {
		return refs[0].id, nil
	}
	// Several distinct alert groups: prefer the playbook-bearing one(s). If exactly
	// one alert carries workflows, that's unambiguously the summary target.
	var withWF []alertRef
	for _, r := range refs {
		if r.hasWF {
			withWF = append(withWF, r)
		}
	}
	if len(withWF) == 1 {
		return withWF[0].id, nil
	}
	// Still ambiguous — list the actual --alert IDS (marking playbook-bearing ones),
	// not the (often identical) alert names.
	cands := refs
	if len(withWF) > 1 {
		cands = withWF
	}
	var lines []string
	for _, r := range cands {
		mark := ""
		if r.hasWF {
			mark = "  [has playbook]"
		}
		lines = append(lines, "  --alert "+r.id+mark)
	}
	return "", fmt.Errorf("case %d has %d alert groups — re-run with one of:\n%s", caseID, len(refs), strings.Join(lines, "\n"))
}
