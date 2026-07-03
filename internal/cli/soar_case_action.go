package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar"
)

func newCaseRunActionCmd() *cobra.Command {
	var (
		caseID       int
		action       string
		instance     string
		alert        string
		scope        string
		integration  string
		params       []string
		skipValidate bool
		dryRun       bool
		yes          bool
	)
	cmd := &cobra.Command{
		Use:   "run-action --id N --action <name> --instance <uuid>",
		Short: "MUTATING (guarded): execute an integration action on a case",
		Long: "Run a single integration action (e.g. HTTP_Post Data, Ping, Enrich IP)\n" +
			"against a case. This is the ad-hoc counterpart to a playbook step — any\n" +
			"installed action can be triggered directly.\n\n" +
			"  secopsctl soar case run-action --id 123 --action HTTP_Ping \\\n" +
			"    --instance <uuid> --alert <alert-group-id>\n\n" +
			"Script parameters are passed via --param key=value (repeatable); secrets\n" +
			"use env-var references (--param 'URL=env:WEBHOOK_URL'). The alert group\n" +
			"identifier comes from `soar case get <id> --json`\n" +
			"(alerts[].additionalProperties.alertGroupIdentifier).\n\n" +
			"For a MARKETPLACE integration's action (e.g. GoogleChronicle's Ping / Get Data\n" +
			"Tables), pass --integration <id> so the action is sent as <id>_<action>\n" +
			"(GoogleChronicle_Ping) — the qualified name the API resolves a script by; a bare\n" +
			"name fails. Built-in Scripts actions are already qualified (HTTP_Ping), so omit\n" +
			"--integration for those.\n\n" +
			"Returns the action result (resultCode, message, resultJsonObject).\n" +
			"Guarded: dry-run by default, --yes to apply.\n\n" +
			"The run is always scoped to an alert group — the server rejects a run that\n" +
			"carries none (a missing scope returns HTTP 500, an empty one 400). Pass --alert\n" +
			"with an alertGroupIdentifier from `soar case get --json`, or omit it and the\n" +
			"action is scoped to the case's alert automatically.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--id is required")
			}
			action = strings.TrimSpace(action)
			if action == "" {
				return fmt.Errorf("--action is required")
			}
			instance = strings.TrimSpace(instance)
			if instance == "" {
				return fmt.Errorf("--instance is required (the integration instance UUID)")
			}

			// Parse --param pairs and resolve env: references.
			scriptParams := map[string]string{}
			for _, p := range params {
				k, v, ok := strings.Cut(p, "=")
				if !ok || strings.TrimSpace(k) == "" {
					return fmt.Errorf("invalid --param %q: expected key=value", p)
				}
				k = strings.TrimSpace(k)
				v = strings.TrimSpace(v)
				if after, found := strings.CutPrefix(v, "env:"); found {
					envVal := os.Getenv(after)
					if envVal == "" {
						return fmt.Errorf("--param %q references env var %q which is empty or unset", k, after)
					}
					v = envVal
				}
				scriptParams[k] = v
			}

			paramsJSON, err := json.Marshal(scriptParams)
			if err != nil {
				return fmt.Errorf("encode script parameters: %w", err)
			}

			// Pre-flight: validate the provided params against the action's schema
			// (best-effort) so a missing mandatory param fails clean here, not as an
			// action-side error after a live call. Only for marketplace actions
			// (--integration set), where the schema is discoverable.
			if integration != "" && !skipValidate {
				if vc, verr := newSOARClient(); verr == nil {
					schema, ferr := fetchActionParamSchema(baseContext(), vc, integration, action)
					switch {
					case ferr != nil:
						fmt.Fprintf(os.Stderr, "warning: could not pre-validate parameters (%v); proceeding\n", ferr)
					case len(schema) > 0:
						errs, warns := validateRunActionParams(schema, scriptParams)
						for _, w := range warns {
							fmt.Fprintf(os.Stderr, "warning: %s\n", w)
						}
						if len(errs) > 0 {
							return fmt.Errorf("parameter validation failed for %s / %s:\n  - %s\n(pass --skip-validate to bypass)",
								integration, action, strings.Join(errs, "\n  - "))
						}
					}
				}
			}

			label := fmt.Sprintf("case %d run-action %s", caseID, action)
			dr, ay := soarGuard(label, dryRun, yes)

			alert = strings.TrimSpace(alert)
			fmt.Fprintf(os.Stdout, "Action:   %s\n", action)
			fmt.Fprintf(os.Stdout, "Case:     %d\n", caseID)
			fmt.Fprintf(os.Stdout, "Instance: %s\n", instance)
			if alert != "" {
				fmt.Fprintf(os.Stdout, "Alert:    %s\n", alert)
			} else {
				fmt.Fprintln(os.Stdout, "Alert:    (auto-resolved from the case at run time)")
			}
			if len(scriptParams) > 0 {
				fmt.Fprintf(os.Stdout, "Params:   %d key(s)", len(scriptParams))
				for k := range scriptParams {
					fmt.Fprintf(os.Stdout, " %s", k)
				}
				fmt.Fprintln(os.Stdout)
			}
			if dr {
				if jsonOut {
					return emitGuardedResult("soar case run-action", true, false)
				}
				fmt.Fprintln(os.Stdout, "DRY RUN — no action executed. Re-run with --yes to apply.")
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult("soar case run-action", false, false)
				}
				fmt.Fprintln(os.Stdout, "Refusing to apply without confirmation (pass --yes). Aborted.")
				return nil
			}

			ctx := baseContext()
			c, cerr := newSOARClient()
			if cerr != nil {
				return cerr
			}
			// executeManualAction REQUIRES a valid alertGroupIdentifiers: the server
			// NPEs (HTTP 500) when the field is omitted and 400s when it is empty, so a
			// run must carry a real group. Take --alert verbatim, else resolve from the
			// case's alerts.
			group, total, gerr := resolveCaseAlertGroup(ctx, c, strconv.Itoa(caseID), alert)
			if gerr != nil {
				return gerr
			}
			if alert == "" && total > 1 {
				fmt.Fprintf(os.Stderr, "note: case %d has %d alert groups; running against %q (pass --alert to choose another)\n", caseID, total, group)
			}
			body := buildManualActionBody(caseID, integration, action, scope, instance, string(paramsJSON), group)

			// Execute on the working path directly — no modern→legacy auto-fallback
			// (both hit the same surface; a 5xx is surfaced, not papered over). --legacy
			// selects the legacy lane explicitly.
			if forceLegacy {
				lc, lerr := newSOARLegacyClient()
				if lerr != nil {
					return lerr
				}
				raw, lerr := lc.ExecuteManualAction(ctx, body)
				if lerr != nil {
					return lerr
				}
				return renderActionResult(raw)
			}
			raw, merr := c.ExecuteManualAction(ctx, body)
			if merr != nil {
				return merr
			}
			return renderActionResult(raw)
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "id", 0, "SOAR case id (required) — from 'soar case list'")
	f.IntVar(&caseID, "case-id", 0, "deprecated alias of --id")
	_ = f.MarkHidden("case-id")
	f.StringVar(&action, "action", "", "integration action name, e.g. HTTP_Ping or HTTP_Post Data (required)")
	f.StringVar(&instance, "instance", "", "integration instance UUID (required; from 'integrations instances')")
	f.StringVar(&alert, "alert", "", "alertGroupIdentifier to scope the action (from 'soar case get --json'); "+
		"required by the server, so when omitted it is auto-resolved from the case's alert(s)")
	f.StringVar(&scope, "scope", "All entities", "entity scope")
	f.StringVar(&integration, "integration", "",
		"integration identifier for a marketplace action (e.g. GoogleChronicle) — the action is "+
			"sent as <integration>_<action> (GoogleChronicle_Ping); omit for built-in Scripts "+
			"actions whose name is already qualified (HTTP_Ping)")
	f.StringArrayVar(&params, "param", nil, "key=value script parameter (repeatable); use env:VAR for secrets")
	f.BoolVar(&skipValidate, "skip-validate", false,
		"skip the pre-flight check of --param against the action's schema (--integration actions only)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
}

// buildManualActionBody assembles the ExecuteManualAction request body. The action
// provider is always "Scripts" (the script-execution framework); what selects the
// integration is the action NAME, which the server expects in <integration>_<action>
// form (e.g. "GoogleChronicle_Ping"). qualifyActionName prefixes integration when it
// is set and not already present, so a marketplace action resolves; a built-in
// Scripts action (already "HTTP_Ping") is passed with integration empty. caseId is
// sent as a string to match the server contract. alert, when non-empty, scopes the
// run to one alert group.
func buildManualActionBody(caseID int, integration, action, scope, instance, paramsJSON, alert string) map[string]any {
	name := qualifyActionName(integration, action)
	body := map[string]any{
		"caseId":            strconv.Itoa(caseID),
		"actionProvider":    "Scripts",
		"actionName":        name,
		"scope":             scope,
		"isPredefinedScope": true,
		"targetEntities":    []any{},
		"properties": map[string]string{
			"ScriptName":                   name,
			"IntegrationInstance":          instance,
			"ScriptParametersEntityFields": paramsJSON,
		},
	}
	if alert = strings.TrimSpace(alert); alert != "" {
		body["alertGroupIdentifiers"] = []string{alert}
	}
	return body
}

// resolveCaseAlertGroup returns the alertGroupIdentifier a run-action body must
// carry. executeManualAction NPEs server-side (HTTP 500) when alertGroupIdentifiers
// is omitted and 400s when it is empty, so a valid group is mandatory. A non-empty
// ref is taken verbatim (the alertGroupIdentifier `soar case get` prints). When ref
// is empty the case's alerts are read and the first distinct group is used; total is
// the distinct-group count, so the caller can note when it picked among several.
func resolveCaseAlertGroup(ctx context.Context, c *soar.Client, caseID, ref string) (group string, total int, err error) {
	if ref = strings.TrimSpace(ref); ref != "" {
		return ref, 1, nil
	}
	alerts, err := c.ListCaseAlerts(ctx, caseID)
	if err != nil {
		return "", 0, err
	}
	var groups []string
	seen := map[string]bool{}
	for i := range alerts {
		g := strings.TrimSpace(alerts[i].AlertGroupIdentifier)
		if g != "" && !seen[g] {
			seen[g] = true
			groups = append(groups, g)
		}
	}
	if len(groups) == 0 {
		return "", 0, fmt.Errorf("case %s has no alert group to scope the action; pass --alert with an alertGroupIdentifier from `soar case get %s --json`", caseID, caseID)
	}
	return groups[0], len(groups), nil
}

// qualifyActionName returns the <integration>_<action> name the ExecuteManualAction
// API resolves a script by. integration empty (or already the action's prefix)
// leaves action unchanged, so a pre-qualified name ("GoogleChronicle_Ping",
// "HTTP_Ping") is never double-prefixed.
func qualifyActionName(integration, action string) string {
	integration = strings.TrimSpace(integration)
	action = strings.TrimSpace(action)
	if integration == "" || strings.HasPrefix(action, integration+"_") {
		return action
	}
	return integration + "_" + action
}

// fetchActionParamSchema returns the parameter schema for one integration action,
// resolving it by display name (or raw name). It lists the integration's actions,
// finds the match, and GETs its full definition (the LIST omits parameters). A
// missing action or read failure is returned as an error for the caller to treat as
// non-fatal (validation is best-effort, never a hard gate on a flaky read).
func fetchActionParamSchema(ctx context.Context, c *soar.Client, integration, action string) ([]playbookActionParam, error) {
	defs, err := c.ListActions(ctx, integration)
	if err != nil {
		return nil, err
	}
	var match *soar.ActionDef
	for i := range defs {
		if strings.EqualFold(defs[i].DisplayName, action) || strings.EqualFold(defs[i].Name, action) {
			match = &defs[i]
			break
		}
	}
	if match == nil {
		return nil, fmt.Errorf("action %q not found in integration %q", action, integration)
	}
	raw, err := c.GetActionDef(ctx, integration, match.PathID())
	if err != nil {
		return nil, err
	}
	rows := summarizeIntegrationActions(integration, wrapActionsEnvelope([]json.RawMessage{raw}))
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].Parameters, nil
}

// validateRunActionParams checks provided --param keys against an action's schema:
// a missing MANDATORY parameter is a hard error (returned in errs, aborting before
// the live call); a key absent from the schema is a soft warning (a likely typo, but
// the schema may be incomplete, so it never blocks). LIST optionalValues are NOT
// enforced — the server treats them as suggestions, not a closed set.
func validateRunActionParams(schema []playbookActionParam, provided map[string]string) (errs, warns []string) {
	known := make(map[string]bool, len(schema))
	for _, p := range schema {
		known[p.Name] = true
	}
	unknown := make([]string, 0)
	for k := range provided {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		warns = append(warns, fmt.Sprintf("parameter %q is not in the action's schema (typo? schema may be incomplete)", k))
	}
	var missing []string
	for _, p := range schema {
		if p.Mandatory {
			if _, ok := provided[p.Name]; !ok {
				missing = append(missing, p.Name)
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		errs = append(errs, "missing mandatory parameter(s): "+strings.Join(missing, ", "))
	}
	return errs, warns
}

// renderActionResult prints a human summary or JSON of an action execution result
// and returns a non-nil error when the action FAULTED, so a faulted run exits
// non-zero. The server reports a successful API call even when the action itself
// failed: the run-action call returns 200 with the action's own outcome in the body,
// so the outcome is read from resultName/status/resultValue (not the HTTP status).
func renderActionResult(raw json.RawMessage) error {
	var result struct {
		ResultCode  int             `json:"resultCode"`
		Message     string          `json:"message"`
		ResultName  string          `json:"resultName"`
		ResultValue json.RawMessage `json:"resultValue"`
		Status      string          `json:"status"`
	}
	parsed := json.Unmarshal(raw, &result) == nil
	// A faulted action is signalled by any of status=FAULTED, resultName=is_failed,
	// resultValue=false, or a non-zero resultCode.
	resultValue := strings.Trim(strings.TrimSpace(string(result.ResultValue)), `"`)
	faulted := parsed && (strings.EqualFold(result.Status, "FAULTED") ||
		strings.EqualFold(result.ResultName, "is_failed") ||
		strings.EqualFold(resultValue, "false") ||
		result.ResultCode != 0)

	switch {
	case jsonOut:
		if err := writeRawJSON(os.Stdout, raw); err != nil {
			return err
		}
	case parsed:
		label := "success"
		if faulted {
			label = "FAULTED"
		}
		fmt.Fprintf(os.Stdout, "result: %s (code %d)\n", label, result.ResultCode)
		if msg := strings.TrimSpace(result.Message); msg != "" {
			fmt.Fprintf(os.Stdout, "message: %s\n", msg)
		}
	default:
		fmt.Fprintf(os.Stdout, "raw result: %s\n", string(raw))
	}

	if faulted {
		msg := strings.TrimSpace(result.Message)
		if msg == "" {
			msg = fmt.Sprintf("action reported failure (resultName=%q status=%q)", result.ResultName, result.Status)
		}
		return fmt.Errorf("action faulted: %s", msg)
	}
	return nil
}
