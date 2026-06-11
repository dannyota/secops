package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newCaseRunActionCmd() *cobra.Command {
	var (
		caseID   int
		action   string
		instance string
		alert    string
		scope    string
		params   []string
		dryRun   bool
		yes      bool
	)
	cmd := &cobra.Command{
		Use:   "run-action --case-id N --action <name> --instance <uuid>",
		Short: "MUTATING (guarded): execute an integration action on a case",
		Long: "Run a single integration action (e.g. HTTP_Post Data, Ping, Enrich IP)\n" +
			"against a case. This is the ad-hoc counterpart to a playbook step — any\n" +
			"installed action can be triggered directly.\n\n" +
			"  secopsctl soar case run-action --case-id 123 --action HTTP_Ping \\\n" +
			"    --instance <uuid> --alert <alert-group-id>\n\n" +
			"Script parameters are passed via --param key=value (repeatable); secrets\n" +
			"use env-var references (--param 'URL=env:WEBHOOK_URL'). The alert group\n" +
			"identifier comes from `soar case get <id> --json`\n" +
			"(alerts[].additionalProperties.alertGroupIdentifier).\n\n" +
			"Returns the action result (resultCode, message, resultJsonObject).\n" +
			"Guarded: dry-run by default, --yes to apply.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if caseID <= 0 {
				return fmt.Errorf("--case-id is required")
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

			body := map[string]any{
				"caseId":            caseID,
				"actionProvider":    "Scripts",
				"actionName":        action,
				"scope":             scope,
				"isPredefinedScope": true,
				"targetEntities":    []any{},
				"properties": map[string]string{
					"ScriptName":                   action,
					"IntegrationInstance":          instance,
					"ScriptParametersEntityFields": string(paramsJSON),
				},
			}
			if alert = strings.TrimSpace(alert); alert != "" {
				body["alertGroupIdentifiers"] = []string{alert}
			}

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
	f.IntVar(&caseID, "case-id", 0, "SOAR case id (required)")
	f.StringVar(&action, "action", "", "integration action name, e.g. HTTP_Ping or HTTP_Post Data (required)")
	f.StringVar(&instance, "instance", "", "integration instance UUID (required; from `soar integration instances`)")
	f.StringVar(&alert, "alert", "", "alert group identifier (from `soar case get --json`)")
	f.StringVar(&scope, "scope", "All entities", "entity scope")
	f.StringArrayVar(&params, "param", nil, "key=value script parameter (repeatable); use env:VAR for secrets")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	return markJSON(cmd)
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
