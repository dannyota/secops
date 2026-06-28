package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

type integrationGetDetail struct {
	Identifier      string                  `json:"identifier"`
	DisplayName     string                  `json:"displayName"`
	Description     string                  `json:"description,omitempty"`
	Version         string                  `json:"version,omitempty"`
	UpdateAvailable bool                    `json:"updateAvailable,omitempty"`
	IsCustom        bool                    `json:"isCustom"`
	Certified       bool                    `json:"certified,omitempty"`
	HasActions      bool                    `json:"hasActions"`
	HasJobs         bool                    `json:"hasJobs"`
	Instances       []integrationInstDetail `json:"instances,omitempty"`
	Playbooks       []string                `json:"playbooks,omitempty"`
}

type integrationInstDetail struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Environment  string `json:"environment"`
	IsConfigured bool   `json:"isConfigured"`
}

func newSOARIntegrationGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <identifier>",
		Short: "Show integration detail: instances, version, playbook usage",
		Long: "Fetch a rich view of one installed integration: metadata, configured\n" +
			"instances across environments, and which playbooks use it. The\n" +
			"<identifier> is the integration identifier (e.g. 'GoogleChronicle').",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := newSOARLegacyClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			id := args[0]

			detail := integrationGetDetail{Identifier: id}

			mc, _ := newSOARClient()
			if mc != nil {
				if packs, lerr := mc.ListIntegrations(ctx); lerr == nil {
					for _, p := range packs {
						if strings.EqualFold(p.Identifier, id) {
							detail.DisplayName = p.DisplayName
							detail.Version = p.LatestVersion
							detail.UpdateAvailable = p.UpdateAvailable
							detail.IsCustom = p.Custom
							detail.Certified = p.Certified
							break
						}
					}
				}
			}

			rawDef, err := lc.GetIntegrationDefaultInstance(ctx, id)
			if err != nil {
				return fmt.Errorf("get integration %q: %w", id, err)
			}
			var doc struct {
				IntegrationName string `json:"integrationName"`
				Description     string `json:"description"`
				HasActions      bool   `json:"hasActions"`
				HasJobs         bool   `json:"hasJobs"`
			}
			_ = json.Unmarshal(rawDef, &doc)
			if detail.DisplayName == "" {
				detail.DisplayName = doc.IntegrationName
			}
			detail.Description = doc.Description
			detail.HasActions = doc.HasActions
			detail.HasJobs = doc.HasJobs

			insts, err := listIntegrationInstances(ctx, lc, id)
			if err == nil {
				for _, inst := range insts {
					detail.Instances = append(detail.Instances, integrationInstDetail{
						ID:           inst.Identifier,
						Name:         inst.InstanceName,
						Environment:  inst.EnvironmentIdentifier,
						IsConfigured: inst.IsConfigured,
					})
				}
			}

			if len(detail.Instances) > 0 {
				for _, inst := range detail.Instances {
					pbs, perr := lc.GetPlaybooksUsingInstance(ctx, inst.ID)
					if perr != nil {
						continue
					}
					var names []string
					_ = json.Unmarshal(pbs, &names)
					for _, n := range names {
						if !slices.Contains(detail.Playbooks, n) {
							detail.Playbooks = append(detail.Playbooks, n)
						}
					}
				}
			}

			if jsonOut {
				return emitJSON(detail)
			}
			printIntegrationDetail(cmd.OutOrStdout(), detail)
			return nil
		},
	}
	return markJSON(cmd)
}

func printIntegrationDetail(w io.Writer, d integrationGetDetail) {
	fmt.Fprintf(w, "%s (%s)\n", orDash(d.DisplayName), d.Identifier)
	if d.Description != "" {
		fmt.Fprintf(w, "  description: %s\n", truncate(d.Description, 120))
	}
	if d.Version != "" {
		ver := d.Version
		if d.UpdateAvailable {
			ver += " (update available)"
		}
		fmt.Fprintf(w, "  version:     %s\n", ver)
	}
	fmt.Fprintf(w, "  custom:      %v\n", d.IsCustom)
	if d.Certified {
		fmt.Fprintf(w, "  certified:   true\n")
	}

	var caps []string
	if d.HasActions {
		caps = append(caps, "actions")
	}
	if d.HasJobs {
		caps = append(caps, "jobs")
	}
	if len(caps) > 0 {
		fmt.Fprintf(w, "  capabilities: %s\n", strings.Join(caps, ", "))
	}

	if len(d.Instances) > 0 {
		fmt.Fprintf(w, "\n  Instances (%d):\n", len(d.Instances))
		for _, inst := range d.Instances {
			state := "unconfigured"
			if inst.IsConfigured {
				state = "configured"
			}
			fmt.Fprintf(w, "    %s  env=%s  %s  (%s)\n", inst.Name, inst.Environment, state, inst.ID)
		}
	}

	if len(d.Playbooks) > 0 {
		fmt.Fprintf(w, "\n  Used by playbooks (%d):\n", len(d.Playbooks))
		for _, pb := range d.Playbooks {
			fmt.Fprintf(w, "    - %s\n", pb)
		}
	}
}
