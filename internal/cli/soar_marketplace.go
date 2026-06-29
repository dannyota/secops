package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// printMarketplaceDetail renders a human-readable summary of a marketplace
// integration or content pack from its raw JSON. The GET endpoints return
// richer fields (title, installed, installedVersion, version, description)
// than the typed struct captures.
func printMarketplaceDetail(raw json.RawMessage, identifier, displayName string) {
	var detail struct {
		Title            string `json:"title"`
		Installed        *bool  `json:"installed"`
		InstalledVersion string `json:"installedVersion"`
		Version          string `json:"version"`
		Description      string `json:"description"`
		UpdateAvailable  *bool  `json:"updateAvailable"`
	}
	_ = json.Unmarshal(raw, &detail)

	name := detail.Title
	if name == "" {
		name = displayName
	}
	if name == "" {
		name = identifier
	}

	fmt.Printf("Identifier:  %s\n", identifier)
	fmt.Printf("Name:        %s\n", name)
	if detail.Installed != nil {
		fmt.Printf("Installed:   %v\n", *detail.Installed)
	}
	if detail.InstalledVersion != "" {
		fmt.Printf("Version:     %s (installed) / %s (latest)\n", detail.InstalledVersion, detail.Version)
	} else if detail.Version != "" {
		fmt.Printf("Version:     %s\n", detail.Version)
	}
	if detail.UpdateAvailable != nil && *detail.UpdateAvailable {
		fmt.Println("Update:      available")
	}
	if detail.Description != "" {
		desc := strings.TrimSpace(detail.Description)
		if len(desc) > 200 {
			desc = desc[:200] + "…"
		}
		fmt.Printf("Description: %s\n", desc)
	}
	fmt.Println("\n(--json for the full record)")
}

// newSOARMarketplaceCmd is the top-level `content-hub` group (UI "Content Hub"):
// browse + install/uninstall marketplace integrations and content packs. Content
// Hub is served on the SOAR host via the modern v1alpha API (soar/marketplace.go).
func init() { rootCmd.AddCommand(newSOARMarketplaceCmd()) }

func newSOARMarketplaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "content-hub",
		Short: "Content Hub: browse + install/uninstall marketplace integrations and content packs",
	}
	cmd.AddCommand(newSOARMarketplaceListCmd(), newSOARMarketplaceGetCmd(), newSOARContentPacksCmd(),
		newSOARFeaturedCmd(), newSOARMarketplaceDiffCmd(),
		newSOARMarketplaceInstallCmd(), newSOARMarketplaceUninstallCmd(), newSOARMarketplaceBrowseCmd())
	return cmd
}

// newSOARMarketplaceBrowseCmd prints a one-shot Content Hub overview — integration
// and content-pack totals with installed counts — and how to drill in / install.
func newSOARMarketplaceBrowseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "browse",
		Short: "Content Hub overview: integration + content-pack totals and installed counts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			ctx := baseContext()
			ints, err := c.ListMarketplaceIntegrations(ctx)
			if err != nil {
				return err
			}
			// The marketplace list does not populate IsInstalled; the authoritative
			// installed-integrations source is the installed-pack list.
			installed, _ := c.ListIntegrations(ctx)
			intInstalled := len(installed)
			// Content packs are best-effort — a read failure shouldn't sink the overview.
			packs, packErr := c.ListContentPacks(ctx)
			packInstalled := 0
			for _, p := range packs {
				if p.IsInstalled {
					packInstalled++
				}
			}
			if jsonOut {
				return emitJSON(map[string]any{
					"integrations":  map[string]int{"total": len(ints), "installed": intInstalled},
					"content_packs": map[string]int{"total": len(packs), "installed": packInstalled},
				})
			}
			fmt.Fprintln(os.Stdout, "Content Hub")
			fmt.Fprintf(os.Stdout, "  integrations:  %4d in catalog, %d installed\n", len(ints), intInstalled)
			switch {
			case packErr != nil:
				fmt.Fprintf(os.Stdout, "  content packs: (read failed: %v)\n", packErr)
			case packInstalled > 0:
				fmt.Fprintf(os.Stdout, "  content packs: %4d in catalog, %d installed\n", len(packs), packInstalled)
			default:
				fmt.Fprintf(os.Stdout, "  content packs: %4d in catalog\n", len(packs))
			}
			fmt.Fprintln(os.Stdout, "\nDrill in:  marketplace list [--installed] · marketplace contentpacks · marketplace featured list")
			fmt.Fprintln(os.Stdout, "Install:   marketplace install|uninstall --identifier <id>")
			return nil
		},
	}
	return markJSON(cmd)
}

// newSOARMarketplaceInstallCmd installs a marketplace integration by identifier
// (`marketplaceIntegrations/{id}:install`). Same surface as `integrations
// install`; this is the canonical Content-Hub home and the inverse of `uninstall`.
func newSOARMarketplaceInstallCmd() *cobra.Command {
	var identifier string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "install --identifier <id>",
		Short: "MUTATING (guarded): install a Content Hub marketplace integration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMarketplaceInstallToggle(identifier, dryRun, yes, true)
		},
	}
	f := cmd.Flags()
	f.StringVar(&identifier, "identifier", "", "marketplace integration identifier (from 'soar marketplace list') (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("identifier")
	return markJSON(cmd)
}

// newSOARMarketplaceUninstallCmd uninstalls a marketplace integration by identifier
// (`marketplaceIntegrations/{id}:uninstall`) — the inverse of `install`, and the
// path `integrations uninstall` lacks (that verb deletes only custom packs).
func newSOARMarketplaceUninstallCmd() *cobra.Command {
	var identifier string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "uninstall --identifier <id>",
		Short: "MUTATING (guarded): uninstall a Content Hub marketplace integration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMarketplaceInstallToggle(identifier, dryRun, yes, false)
		},
	}
	f := cmd.Flags()
	f.StringVar(&identifier, "identifier", "", "installed marketplace integration identifier (required)")
	f.BoolVar(&dryRun, "dry-run", false, "preview only (default behavior)")
	f.BoolVar(&yes, "yes", false, "apply for real / skip confirmation")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "yes")
	_ = cmd.MarkFlagRequired("identifier")
	return markJSON(cmd)
}

// runMarketplaceInstallToggle shares the guarded install/uninstall flow. install
// selects the verb; the SDK call is the only difference.
func runMarketplaceInstallToggle(identifier string, dryRun, yes, install bool) error {
	verb := "uninstall"
	if install {
		verb = "install"
	}
	target := fmt.Sprintf("marketplace %s %s", verb, identifier)
	dr, ay := soarGuard(target, dryRun, yes)
	if dr {
		if jsonOut {
			return emitGuardedResult(target, true, false)
		}
		fmt.Printf("DRY RUN — would %s marketplace integration %q. Re-run with --yes.\n", verb, identifier)
		return nil
	}
	if !ay {
		if jsonOut {
			return emitGuardedResult(target, false, false)
		}
		fmt.Printf("Refusing to %s without confirmation (pass --yes). Aborted.\n", verb)
		return nil
	}
	c, err := newSOARClient()
	if err != nil {
		return err
	}
	var out json.RawMessage
	if install {
		out, err = c.InstallMarketplaceIntegration(baseContext(), identifier, map[string]any{})
	} else {
		out, err = c.UninstallMarketplaceIntegration(baseContext(), identifier, map[string]any{})
	}
	if err != nil {
		return err
	}
	if jsonOut {
		return emitJSON(out)
	}
	done := "Uninstalled"
	if install {
		done = "Installed"
	}
	fmt.Printf("%s marketplace integration %q.\n", done, identifier)
	return nil
}

func newSOARMarketplaceListCmd() *cobra.Command {
	var onlyInstalled bool
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
			if jsonOut {
				raws := make([]json.RawMessage, len(items))
				for i := range items {
					raws[i] = items[i].Raw
				}
				return emitJSON(raws)
			}
			for _, m := range items {
				tag := ""
				if m.IsInstalled {
					tag = "  [installed]"
				}
				name := m.DisplayName
				if name == "" {
					name = m.Identifier
				}
				fmt.Fprintf(os.Stdout, "%-44s%s\n", name, tag)
			}
			fmt.Fprintf(os.Stdout, "\n%d marketplace integration(s)\n", len(items))
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&onlyInstalled, "installed", false, "show only installed integrations")
	return markJSON(cmd)
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
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(m.Raw)
			}
			printMarketplaceDetail(m.Raw, m.Identifier, m.DisplayName)
			return nil
		},
	}
	return markJSON(cmd)
}

// newSOARContentPacksCmd lists content packs by default (bare `contentpacks`) and
// hosts `contentpacks get <id>` to inspect one before install.
func newSOARContentPacksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contentpacks",
		Short: "List Content Hub content packs (read-only); `get <id>` to inspect one",
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
			if jsonOut {
				raws := make([]json.RawMessage, len(packs))
				for i := range packs {
					raws[i] = packs[i].Raw
				}
				return emitJSON(raws)
			}
			for _, p := range packs {
				tag := ""
				if p.IsInstalled {
					tag = "  [installed]"
				}
				name := p.DisplayName
				if name == "" {
					name = p.Identifier
				}
				fmt.Fprintf(os.Stdout, "%-44s%s\n", name, tag)
			}
			fmt.Fprintf(os.Stdout, "\n%d content pack(s)\n", len(packs))
			return nil
		},
	}
	cmd.AddCommand(newSOARContentPackGetCmd())
	return markJSON(cmd)
}

func newSOARContentPackGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <identifier>",
		Short: "Show one Content Hub content pack (read-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newSOARClient()
			if err != nil {
				return err
			}
			p, err := c.GetContentPack(baseContext(), args[0])
			if err != nil {
				return err
			}
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(p.Raw)
			}
			printMarketplaceDetail(p.Raw, p.Identifier, p.DisplayName)
			return nil
		},
	}
	return markJSON(cmd)
}
