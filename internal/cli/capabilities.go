package cli

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// `secopsctl capabilities` — one session-bootstrap probe that fuses `version`,
// `surfaces`, and `doctor` so an agent self-configures what it can do on THIS
// instance in a single call: tool build, per-plane auth health, read-only state,
// and which surfaces are validated vs blocked (so it avoids dead paths instead of
// discovering them by a failed call).

// newCapabilitiesCmd is registered under `status` (status.go) → `status capabilities`.
func newCapabilitiesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capabilities",
		Short: "Show tool version, auth health, surface status, and read-only state",
		Long: "One call that fuses `version`, `doctor`, and `surfaces`: the tool build, auth\n" +
			"health per plane (SIEM ADC vs SOAR AppKey), read-only state, and a summary of\n" +
			"which API surfaces are validated vs blocked. Intended as the first call of an\n" +
			"automated session so an agent knows — without trial and error — what works on\n" +
			"this instance. --offline skips the live auth/reachability probe.\n\n" +
			"Pair with `secopsctl commands --json` (per-verb flags/types) for the full\n" +
			"machine-readable surface.",
		Args: cobra.NoArgs,
		RunE: runCapabilities,
	}
	cmd.Flags().Bool("offline", false, "skip the live auth/reachability probe (version + surfaces + read-only only)")
	return markJSON(cmd)
}

// capSurfaces is the surface-status rollup an agent uses to steer.
type capSurfaces struct {
	Total     int            `json:"total"`
	ByStatus  map[string]int `json:"by_status"`
	Blocked   []string       `json:"blocked"`   // surfaces to avoid (status=blocked)
	Validated []string       `json:"validated"` // surfaces proven live
}

// capabilities is the --json bootstrap object.
type capabilities struct {
	Version  BuildInfo         `json:"version"`
	ReadOnly bool              `json:"read_only"`
	Health   []doctorCheck     `json:"health,omitempty"`
	HealthOK bool              `json:"health_ok"`
	Probed   bool              `json:"probed"` // false when --offline
	Surfaces capSurfaces       `json:"surfaces"`
	MCP      string            `json:"mcp_command"`
	Aliases  map[string]string `json:"command_aliases,omitempty"`
}

// collectCommandAliases maps every back-compat alias to its canonical command
// name, walking the whole tree. Group renames (e.g. `iocs`→`indicators`) live here
// rather than in the `commands` catalog, which lists only runnable verbs.
func collectCommandAliases(root *cobra.Command) map[string]string {
	out := map[string]string{}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			for _, a := range sub.Aliases {
				out[a] = sub.Name()
			}
			walk(sub)
		}
	}
	walk(root)
	if len(out) == 0 {
		return nil
	}
	return out
}

func runCapabilities(cmd *cobra.Command, _ []string) error {
	offline, _ := cmd.Flags().GetBool("offline")

	caps := capabilities{
		Version:  resolveBuildInfo(),
		ReadOnly: readOnlyMode(),
		Surfaces: rollupSurfaces(),
		HealthOK: true,
		MCP:      "secopsctl mcp install",
		Aliases:  collectCommandAliases(rootCmd),
	}
	if !offline {
		checks, ok, _ := healthChecks(baseContext())
		caps.Health, caps.HealthOK, caps.Probed = checks, ok, true
	}

	if jsonOut {
		return emitJSON(caps)
	}

	fmt.Printf("secopsctl %s (%s, %s %s/%s)\n", caps.Version.Version, caps.Version.Commit,
		caps.Version.GoVersion, caps.Version.OS, caps.Version.Arch)
	fmt.Printf("  read-only: %v\n", caps.ReadOnly)
	if caps.Probed {
		fmt.Println("  auth health:")
		for _, c := range caps.Health {
			mark := "✗"
			switch {
			case c.Skipped:
				mark = "-"
			case c.OK:
				mark = "✓"
			}
			detail := c.Detail
			if !c.OK && !c.Skipped {
				detail = c.Error
			}
			fmt.Printf("    %-13s %s  %s\n", c.label, mark, detail)
		}
	} else {
		fmt.Println("  auth health: (skipped — --offline)")
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintf(tw, "  surfaces: %d total\t", caps.Surfaces.Total)
	statuses := make([]string, 0, len(caps.Surfaces.ByStatus))
	for s := range caps.Surfaces.ByStatus {
		statuses = append(statuses, s)
	}
	sort.Strings(statuses)
	for _, s := range statuses {
		fmt.Fprintf(tw, "%s=%d  ", s, caps.Surfaces.ByStatus[s])
	}
	fmt.Fprintln(tw)
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(caps.Surfaces.Blocked) > 0 {
		fmt.Printf("  blocked (avoid): %v\n", caps.Surfaces.Blocked)
	}
	fmt.Printf("  MCP: `secopsctl mcp install` to register as an MCP server\n")
	return nil
}

// rollupSurfaces summarizes the surface registry: counts by status plus the
// blocked and validated name lists.
func rollupSurfaces() capSurfaces {
	rows := surfaceRows()
	out := capSurfaces{Total: len(rows), ByStatus: map[string]int{}}
	for _, r := range rows {
		out.ByStatus[r.Status]++
		switch r.Status {
		case "blocked":
			out.Blocked = append(out.Blocked, r.Name)
		case "validated":
			out.Validated = append(out.Validated, r.Name)
		}
	}
	sort.Strings(out.Blocked)
	sort.Strings(out.Validated)
	return out
}
