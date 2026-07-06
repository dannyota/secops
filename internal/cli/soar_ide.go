package cli

import "github.com/spf13/cobra"

// newSOARIDECmd groups the SOAR authoring tools (the console's Response → "IDE"):
// build a playbook definition and package a custom integration for upload.
func newSOARIDECmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ide <verb>",
		Short: "Build playbooks and package custom integrations for SOAR",
	}
	cmd.AddCommand(newSOARBuildPlaybookCmd(), newSOARPackageIntegrationCmd())
	return cmd
}
