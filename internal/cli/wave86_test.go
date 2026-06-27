package cli

import "testing"

// renamePairs is the v0.5.1 command-clarity rename map: each old name is kept as
// a hidden back-compat alias of the new canonical command. Keep this in lock-step
// with the Use/Aliases declarations and docs/design/cli-naming.md.
var renamePairs = []struct{ canonical, alias string }{}

// TestRenamedCommandsCanonicalAndAlias verifies every renamed top-level command is
// registered under its canonical name and that the old name resolves to the SAME
// command as a cobra alias — so existing invocations keep working unchanged.
func TestRenamedCommandsCanonicalAndAlias(t *testing.T) {
	for _, p := range renamePairs {
		canon, _, err := rootCmd.Find([]string{p.canonical})
		if err != nil || canon == nil || canon.Name() != p.canonical {
			t.Errorf("canonical command %q not registered (got %v, err %v)", p.canonical, canon, err)
			continue
		}
		alias, _, err := rootCmd.Find([]string{p.alias})
		if err != nil || alias == nil {
			t.Errorf("alias %q does not resolve (err %v)", p.alias, err)
			continue
		}
		if alias != canon {
			t.Errorf("alias %q resolves to %q, want canonical %q", p.alias, alias.Name(), p.canonical)
		}
		if !canon.HasAlias(p.alias) {
			t.Errorf("canonical %q is missing alias %q in its Aliases list", p.canonical, p.alias)
		}
	}
}

// TestRenamedGroupsNotCatalogRows verifies the renamed navigation-only groups stay
// OUT of the `commands` catalog (it lists only runnable verbs): neither the
// canonical group name nor its alias is a row. The old→new mapping is exposed via
// `capabilities --json` instead (TestCommandAliasesInCapabilities).
func TestRenamedGroupsNotCatalogRows(t *testing.T) {
	byPath := map[string]commandRow{}
	for _, r := range collectCommands(rootCmd, "") {
		byPath[r.Path] = r
	}
	for _, p := range renamePairs {
		if _, ok := byPath[p.canonical]; ok {
			t.Errorf("renamed group %q is a navigation parent and must not be a catalog row", p.canonical)
		}
		if _, ok := byPath[p.alias]; ok {
			t.Errorf("alias %q must not be a catalog row", p.alias)
		}
	}
}

// TestCommandAliasesInCapabilities verifies each rename's old→new mapping is
// discoverable in the capabilities alias map.
func TestCommandAliasesInCapabilities(t *testing.T) {
	aliases := collectCommandAliases(rootCmd)
	for _, p := range renamePairs {
		got, ok := aliases[p.alias]
		if !ok {
			t.Errorf("capabilities alias map missing %q", p.alias)
			continue
		}
		if got != p.canonical {
			t.Errorf("alias %q maps to %q, want %q", p.alias, got, p.canonical)
		}
	}
}
