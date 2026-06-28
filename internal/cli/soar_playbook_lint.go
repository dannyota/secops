package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/soar/legacy"
)

type playbookLintFinding struct {
	Playbook string `json:"playbook"`
	Severity string `json:"severity"`
	Check    string `json:"check"`
	Message  string `json:"message"`
}

func newSOARPlaybookLintCmd() *cobra.Command {
	var (
		name string
		all  bool
	)
	cmd := &cobra.Command{
		Use:   "lint (--name <playbook> | --all)",
		Short: "Static analysis: detect broken block refs, missing integrations, bad triggers",
		Long: "Analyze playbook definitions for common problems:\n" +
			"  - broken block refs (step references a nested playbook that doesn't exist)\n" +
			"  - missing integration instances (step uses an unconfigured integration)\n" +
			"  - raw placeholders in JSON action params (FR-35 risk)\n" +
			"  - whitespace-poisoned trigger condition values\n\n" +
			"--all runs across every playbook; --name targets one.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && name == "" {
				return fmt.Errorf("pass --name <playbook> or --all")
			}
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()

			cards, err := lc.ListPlaybooks(ctx, nil)
			if err != nil {
				return err
			}

			knownPlaybooks := map[string]string{}
			for _, c := range cards {
				knownPlaybooks[strings.ToLower(c.Name)] = c.Identifier
			}

			var targets []legacy.PlaybookCard
			if all {
				targets = cards
			} else {
				for _, c := range cards {
					if strings.EqualFold(c.Name, name) {
						targets = append(targets, c)
					}
				}
				if len(targets) == 0 {
					return fmt.Errorf("no playbook named %q", name)
				}
			}

			knownIntegrations := map[string]bool{}
			rawInteg, err := lc.ListInstalledIntegrations(ctx)
			if err == nil {
				var integList []struct {
					Identifier string `json:"identifier"`
				}
				if json.Unmarshal(rawInteg, &integList) == nil {
					for _, i := range integList {
						if i.Identifier != "" {
							knownIntegrations[i.Identifier] = true
						}
					}
				}
			}

			var findings []playbookLintFinding
			for _, card := range targets {
				raw, err := lc.GetPlaybook(ctx, card.Identifier)
				if err != nil {
					findings = append(findings, playbookLintFinding{
						Playbook: card.Name,
						Severity: "error",
						Check:    "fetch",
						Message:  fmt.Sprintf("failed to fetch: %v", err),
					})
					continue
				}
				findings = append(findings, lintPlaybook(card.Name, raw, knownPlaybooks, knownIntegrations)...)
			}

			if jsonOut {
				return emitJSON(findings)
			}
			if len(findings) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No issues found.")
				return nil
			}
			printLintFindings(cmd.OutOrStdout(), findings)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "playbook name to lint")
	f.BoolVar(&all, "all", false, "lint all playbooks")
	cmd.MarkFlagsMutuallyExclusive("name", "all")
	return markJSON(cmd)
}

func lintPlaybook(pbName string, raw json.RawMessage, knownPlaybooks map[string]string, knownIntegrations map[string]bool) []playbookLintFinding {
	var doc struct {
		Steps   []json.RawMessage `json:"steps"`
		Trigger json.RawMessage   `json:"trigger"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}

	var findings []playbookLintFinding
	add := func(sev, check, msg string) {
		findings = append(findings, playbookLintFinding{
			Playbook: pbName,
			Severity: sev,
			Check:    check,
			Message:  msg,
		})
	}

	findings = append(findings, lintTrigger(pbName, doc.Trigger)...)

	for _, stepRaw := range doc.Steps {
		var step struct {
			Name              string          `json:"name"`
			Type              any             `json:"type"`
			Integration       string          `json:"integration"`
			ActionName        string          `json:"actionName"`
			AutoSkipOnFailure *bool           `json:"autoSkipOnFailure"`
			Parameters        json.RawMessage `json:"parameters"`
		}
		if json.Unmarshal(stepRaw, &step) != nil {
			continue
		}

		stepType := displayJSONScalar(step.Type)

		if stepType == "BLOCK" || stepType == "Block" {
			blockName := strings.ToLower(step.Name)
			if _, ok := knownPlaybooks[blockName]; !ok {
				add("error", "broken-block-ref",
					fmt.Sprintf("step %q references block %q which does not exist in the live playbook list", step.Name, step.Name))
			}
		}

		if step.Integration != "" && len(knownIntegrations) > 0 {
			if !knownIntegrations[step.Integration] {
				sev := "warning"
				if step.AutoSkipOnFailure != nil && *step.AutoSkipOnFailure {
					sev = "info"
				}
				add(sev, "missing-integration",
					fmt.Sprintf("step %q uses integration %q which has no configured instance", step.Name, step.Integration))
			}
		}

		findings = append(findings, lintStepParams(pbName, step.Name, step.Parameters)...)
	}

	return findings
}

var placeholderInJSONRe = regexp.MustCompile(`\[(?:Alert|Event|Case|Entity)\.[^\]]+\]`)

func lintStepParams(pbName, stepName string, params json.RawMessage) []playbookLintFinding {
	if len(params) == 0 {
		return nil
	}
	var paramList []struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
	}
	if json.Unmarshal(params, &paramList) != nil {
		return nil
	}

	var findings []playbookLintFinding
	for _, p := range paramList {
		val, ok := p.Value.(string)
		if !ok || val == "" {
			continue
		}
		trimmed := strings.TrimSpace(val)
		if (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[{")) && placeholderInJSONRe.MatchString(val) {
			findings = append(findings, playbookLintFinding{
				Playbook: pbName,
				Severity: "warning",
				Check:    "placeholder-in-json",
				Message:  fmt.Sprintf("step %q param %q contains a placeholder inside JSON — placeholder transforms may not JSON-escape the value (FR-35)", stepName, p.Name),
			})
		}
	}
	return findings
}

func lintTrigger(pbName string, triggerRaw json.RawMessage) []playbookLintFinding {
	if len(triggerRaw) == 0 || string(triggerRaw) == "null" {
		return nil
	}
	var trigger struct {
		Conditions []struct {
			FieldName string `json:"fieldName"`
			Value     string `json:"value"`
		} `json:"conditions"`
	}
	if json.Unmarshal(triggerRaw, &trigger) != nil {
		return nil
	}
	var findings []playbookLintFinding
	for _, cond := range trigger.Conditions {
		if cond.Value != strings.TrimSpace(cond.Value) ||
			strings.Contains(cond.Value, "\r") ||
			strings.Contains(cond.Value, "\n") {
			findings = append(findings, playbookLintFinding{
				Playbook: pbName,
				Severity: "warning",
				Check:    "whitespace-trigger",
				Message:  fmt.Sprintf("trigger condition %q has whitespace/newline characters in value — may cause silent match failures", cond.FieldName),
			})
		}
	}
	return findings
}

func printLintFindings(w io.Writer, findings []playbookLintFinding) {
	current := ""
	for _, f := range findings {
		if f.Playbook != current {
			if current != "" {
				fmt.Fprintln(w)
			}
			current = f.Playbook
			fmt.Fprintf(w, "%s:\n", f.Playbook)
		}
		fmt.Fprintf(w, "  [%s] %s: %s\n", f.Severity, f.Check, f.Message)
	}
	fmt.Fprintf(w, "\n%d finding(s)\n", len(findings))
}
