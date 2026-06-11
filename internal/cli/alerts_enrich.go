package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"danny.vn/secops/chronicle"
)

// Enrichment-agent verbs (Wave 56): operate on a SIEM alert BEFORE (or
// without) case grouping — fetch its investigation context, list the
// integration actions executable against its entities, and run a batch of
// them. The pre-case half of alert triage (`soar case run-action` is the
// in-case half).

// enrichmentAgentRead runs one enrichment-agent read on the chronicle host and
// falls back to the SOAR host (the two-host rule: the docs file the resource
// under chronicle, but integration-flavored surfaces often answer on
// siemplify-soar instead).
func enrichmentAgentRead(verb, siemAlertID string) (json.RawMessage, error) {
	ctx := baseContext()
	var chronicleErr error
	if c, err := newChronicleClient(); err == nil {
		fetch := c.FetchAlertData
		if verb == "fetchActions" {
			fetch = c.FetchAlertActions
		}
		raw, err := fetch(ctx, siemAlertID)
		if err == nil {
			return raw, nil
		}
		chronicleErr = err
	}
	sc, err := newSOARClient()
	if err != nil {
		if chronicleErr != nil {
			return nil, chronicleErr
		}
		return nil, err
	}
	fetch := sc.FetchAlertData
	if verb == "fetchActions" {
		fetch = sc.FetchAlertActions
	}
	raw, err := fetch(ctx, siemAlertID)
	if err != nil && chronicleErr != nil {
		// Both hosts failed; the chronicle error is usually the meaningful one.
		return nil, fmt.Errorf("chronicle host: %w (soar host also failed: %v)", chronicleErr, err) //nolint:errorlint // the soar error is annotation only
	}
	return raw, err
}

func newAlertsEnrichCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enrich <alert-id>",
		Short: "Read-only: a SIEM alert's enrichment context (entities, events, indicator)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := enrichmentAgentRead("fetchAlertData", args[0])
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			return emitAlertEnrichData(raw)
		},
	}
	return markJSON(cmd)
}

// emitAlertEnrichData renders the alert context compactly.
func emitAlertEnrichData(raw json.RawMessage) error {
	var resp struct {
		CaseAlert struct {
			RuleGenerator string `json:"ruleGenerator"`
			Product       string `json:"product"`
			DisplayName   string `json:"displayName"`
		} `json:"caseAlert"`
		Entities []struct {
			EntityType   string `json:"entityType"`
			EntityID     string `json:"entityId"`
			IsSuspicious bool   `json:"isSuspicious"`
			IsInternal   bool   `json:"isInternal"`
		} `json:"entities"`
		Events   []json.RawMessage `json:"events"`
		Comments []string          `json:"comments"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("decode alert data: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Alert: %s  —  rule %s · %s\n", orDash(resp.CaseAlert.DisplayName),
		orDash(resp.CaseAlert.RuleGenerator), orDash(resp.CaseAlert.Product))
	fmt.Fprintf(os.Stdout, "\nEntities (%d):\n", len(resp.Entities))
	for _, e := range resp.Entities {
		flags := ""
		if e.IsSuspicious {
			flags += " suspicious"
		}
		if e.IsInternal {
			flags += " internal"
		}
		fmt.Fprintf(os.Stdout, "  %-12s %s%s\n", e.EntityType, e.EntityID, flags)
	}
	fmt.Fprintf(os.Stdout, "\n%d event(s), %d comment(s). Full payload with --json.\n", len(resp.Events), len(resp.Comments))
	return nil
}

func newAlertsActionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "actions <alert-id>",
		Short: "Read-only: integration actions executable against a SIEM alert's entities",
		Long: "List every integration action the enrichment agent can run against the\n" +
			"alert's entities, grouped per integration instance — the catalog `alerts\n" +
			"run-actions` executes from.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := enrichmentAgentRead("fetchActions", args[0])
			if err != nil {
				return err
			}
			if jsonOut {
				return writeRawJSON(os.Stdout, raw)
			}
			return emitAlertActions(raw)
		},
	}
	return markJSON(cmd)
}

// emitAlertActions renders the per-integration action catalog compactly.
func emitAlertActions(raw json.RawMessage) error {
	var resp struct {
		Integrations []struct {
			Integration         string `json:"integration"`
			IntegrationInstance string `json:"integrationInstance"`
			DisplayName         string `json:"displayName"`
			Actions             []struct {
				DisplayName string            `json:"displayName"`
				EntityTypes []string          `json:"entityTypes"`
				Parameters  []json.RawMessage `json:"parameters"`
			} `json:"actions"`
		} `json:"integrations"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("decode actions: %w", err)
	}
	if len(resp.Integrations) == 0 {
		fmt.Fprintln(os.Stdout, "no executable actions.")
		return nil
	}
	total := 0
	for _, in := range resp.Integrations {
		fmt.Fprintf(os.Stdout, "%s  (instance %s)\n", orDash(in.DisplayName), orDash(in.IntegrationInstance))
		for _, a := range in.Actions {
			fmt.Fprintf(os.Stdout, "  - %-40s entities: %v  params: %d\n", a.DisplayName, a.EntityTypes, len(a.Parameters))
			total++
		}
	}
	fmt.Fprintf(os.Stdout, "\n%d action(s) across %d integration(s). Parameter details with --json.\n", total, len(resp.Integrations))
	return nil
}

func newAlertsRunActionsCmd() *cobra.Command {
	var (
		file        string
		dryRun, yes bool
	)
	cmd := &cobra.Command{
		Use:   "run-actions <alert-id> --file <actions.json>",
		Short: "MUTATING (guarded): execute integration actions against a SIEM alert",
		Long: "Run a batch of enrichment-agent actions against the alert's entities. The\n" +
			"file is a JSON array of actions — {displayName, integration,\n" +
			"integrationInstance, targetEntities[], parameters{}} — built from the\n" +
			"`alerts actions` catalog. Guarded: dry-run by default, --yes to apply.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			var actions []chronicle.EnrichmentAction
			if err := json.Unmarshal(data, &actions); err != nil {
				return fmt.Errorf("%s: %w", file, err)
			}
			if len(actions) == 0 {
				return fmt.Errorf("%s carries no actions", file)
			}
			action := fmt.Sprintf("alerts run-actions %s (%d action(s) from %s)", args[0], len(actions), file)
			return guardedSIEMMutation(action, dryRun, yes, func() error {
				c, err := newChronicleClient()
				if err != nil {
					return err
				}
				raw, err := c.ExecuteAlertActions(baseContext(), args[0], actions)
				if err != nil {
					return err
				}
				// Per-action results print on the human path; under --json the
				// guard's envelope owns stdout (do() must stay silent there).
				if !jsonOut {
					return writeRawJSON(os.Stdout, raw)
				}
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&file, "file", "", "JSON array of actions to execute (required)")
	guardRunFlags(cmd, &dryRun, &yes)
	_ = cmd.MarkFlagRequired("file")
	return markJSON(cmd)
}
