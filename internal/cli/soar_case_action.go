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
			"Guarded: dry-run by default, --yes to apply.",
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

			body := buildManualActionBody(caseID, integration, action, scope, instance, string(paramsJSON), alert)

			label := fmt.Sprintf("case %d run-action %s", caseID, action)
			dr, ay := soarGuard(label, dryRun, yes)

			fmt.Fprintf(os.Stdout, "Action:   %s\n", action)
			fmt.Fprintf(os.Stdout, "Case:     %d\n", caseID)
			fmt.Fprintf(os.Stdout, "Instance: %s\n", instance)
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
				fmt.Fprintln(os.Stdout, "DRY RUN -- no API call made. Re-run with --yes to apply.")
				return nil
			}
			if !ay {
				if jsonOut {
					return emitGuardedResult("soar case run-action", false, false)
				}
				fmt.Fprintln(os.Stdout, "Refusing to apply without confirmation (pass --yes). Aborted.")
				return nil
			}

			return preferModern("soar case run-action",
				func() error {
					mc, merr := newSOARClient()
					if merr != nil {
						return merr
					}
					raw, merr := mc.ExecuteManualAction(baseContext(), body)
					if merr != nil {
						return merr
					}
					return renderActionResult(raw)
				},
				func() error {
					lc, lerr := newSOARLegacyClient()
					if lerr != nil {
						return lerr
					}
					raw, lerr := lc.ExecuteManualAction(baseContext(), body)
					if lerr != nil {
						return lerr
					}
					return renderActionResult(raw)
				},
			)
		},
	}
	f := cmd.Flags()
	f.IntVar(&caseID, "id", 0, "SOAR case id (required) — from 'soar case list'")
	f.IntVar(&caseID, "case-id", 0, "deprecated alias of --id")
	_ = f.MarkHidden("case-id")
	f.StringVar(&action, "action", "", "integration action name, e.g. HTTP_Ping or HTTP_Post Data (required)")
	f.StringVar(&instance, "instance", "", "integration instance UUID (required; from 'soar integration instances')")
	f.StringVar(&alert, "alert", "", "alert group identifier (from 'soar case get --json')")
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

// renderActionResult prints a human summary or JSON of an action execution result.
func renderActionResult(raw json.RawMessage) error {
	if jsonOut {
		return writeRawJSON(os.Stdout, raw)
	}
	var result struct {
		ResultCode  int    `json:"resultCode"`
		Message     string `json:"message"`
		ResultName  string `json:"resultName"`
		Integration string `json:"integration"`
	}
	if err := json.Unmarshal(raw, &result); err == nil {
		status := "success"
		if result.ResultCode != 0 {
			status = "failed"
		}
		fmt.Fprintf(os.Stdout, "result: %s (code %d)\n", status, result.ResultCode)
		if result.Message != "" {
			fmt.Fprintf(os.Stdout, "message: %s\n", strings.TrimSpace(result.Message))
		}
	} else {
		fmt.Fprintf(os.Stdout, "raw result: %s\n", string(raw))
	}
	return nil
}
