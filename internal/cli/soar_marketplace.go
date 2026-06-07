package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newSOARMarketplaceCmd groups the Content-Hub (marketplace) read verbs. Content
// Hub is served on the SOAR host via the modern v1alpha API (soar/marketplace.go);
// these are the user-facing reads over it. Install/uninstall stay SDK-only for now
// (heavy, non-self-cleaning mutations).
func newSOARMarketplaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "marketplace",
		Short: "Browse the Content Hub (marketplace integrations + content packs)",
	}
	cmd.AddCommand(newSOARMarketplaceListCmd(), newSOARMarketplaceGetCmd(), newSOARContentPacksCmd())
	return cmd
}

func newSOARMarketplaceListCmd() *cobra.Command {
	var (
		asJSON        bool
		onlyInstalled bool
	)
	cmd := &cobra.Command{
		Use:   "list [--installed]",
		Short: "List Content Hub marketplace integrations (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			items, err := c.ListMarketplaceIntegrations(baseContext())
			if err != nil {
				return err
			}
			if onlyInstalled {
				filtered := items[:0]
				for _, m := range items {
					if m.IsInstalled {
						filtered = append(filtered, m)
					}
				}
				items = filtered
			}
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(items)
			}
			for _, m := range items {
				tag := ""
				if m.IsInstalled {
					tag = "  [installed]"
				}
				fmt.Fprintf(os.Stdout, "%-44s %s%s\n", m.Identifier, m.DisplayName, tag)
			}
			fmt.Fprintf(os.Stdout, "\n%d marketplace integration(s)\n", len(items))
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&onlyInstalled, "installed", false, "show only installed integrations")
	f.BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

func newSOARMarketplaceGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <identifier>",
		Short: "Show one marketplace integration (read-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			m, err := c.GetMarketplaceIntegration(baseContext(), args[0])
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(m.Raw)
		},
	}
	return cmd
}

func newSOARContentPacksCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "contentpacks",
		Short: "List Content Hub content packs (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			packs, err := c.ListContentPacks(baseContext())
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(packs)
			}
			for _, p := range packs {
				tag := ""
				if p.IsInstalled {
					tag = "  [installed]"
				}
				fmt.Fprintf(os.Stdout, "%-44s %s%s\n", p.Identifier, p.DisplayName, tag)
			}
			fmt.Fprintf(os.Stdout, "\n%d content pack(s)\n", len(packs))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}
