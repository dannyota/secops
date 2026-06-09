package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"danny.vn/secops/internal/mirror"
	"danny.vn/secops/internal/mirror/reconcile"
)

// surfaceProse holds the per-surface write gotchas that are NOT derivable from the
// engine Capabilities — the behavior an operator should know before a push, keyed
// by CLI target name. The prune/etag lines are added generically by surfaceNote
// from Caps, so this map carries only the hand-written prose (don't duplicate
// prune-eligibility here). Keep each entry one or two terse sentences.
var surfaceProse = map[string]string{
	// Bespoke rule targets (not engine surfaces — no Caps; prose only).
	"rules-create":  "Creates live rules from *.yaral files that have no companion *.yaml; --enabled, --alerting, and --run-frequency set the initial deployment.",
	"rules-update":  "Updates live YARA-L text where a tracked *.yaral changed; etag-guarded.",
	"rules-deploy":  "Reconciles each tracked rule's deployment block (enabled / alerting / runFrequency); pass --rule to scope one rule.",
	"rules-disable": "Disables locally-tracked rules whose deployment.enabled is true.",
	"rules":         "YARA-L source plus a typed deployment subset per rule (not a single canonical body).",

	// SIEM engine surfaces.
	"reference_lists":     "No delete or archive API; entries and scope are normalized for stable round-trips.",
	"data_tables":         "Row writes are a wholesale destroy-and-replace (ReplaceDataTableRows), not a merge — the dry run is the safety check.",
	"parsers":             "Parsers are immutable (new version + activate). `push parsers` activates immediately — a live ingestion cutover. Dry-test first with `parsers run`; roll back with `parsers activate <id>`.",
	"feeds":               "Secret scalars are redacted on pull; new credentials can use secret_ref from env / Secret Manager at push time, never literal YAML. GCS V2 / Storage-Transfer feeds supported.",
	"forwarders":          "Full pull / push / drift symmetry, so `pull all` mirrors forwarders and the drift gate stays clean.",
	"dashboards":          "CUSTOM dashboards only. `access` is immutable — change it with `dashboards duplicate`, not push. Charts are replaced wholesale on update.",
	"rule_exclusions":     "Detection-exclusion rules: scoped UDM filters that suppress matches.",
	"metric_definitions":  "Additive: create / patch only. Its textDefinition is YARA-L 2.0, so it diffs and pushes like a rule.",
	"scheduled_reports":   "Scheduled dashboard delivery (recurrence / recipients / format); complements the `dashboards` surface.",
	"datataps":            "Stream UDM events to Pub/Sub. The server PATCH is unimplemented, so an update is done as delete-old + create-new.",
	"error_notifications": "Ingestion-health alerts (zero-ingest / size / normalization-delay) routed to Cloud Monitoring channels.",
	"federation_groups":   "MSSP / multi-tenant only: groups subtenant instances. Writes touch live access — extra care.",

	// Imperative / read-only pull targets.
	"curated":       "Curated (Google-managed) rule-set deployment state; `push curated` reconciles deployments.yaml, while `curated set` toggles one deployment.",
	"curated_rules": "Read-only Google / Mandiant-managed detections; --filter narrows the pull.",
}

// pruneNoOp reports whether --prune cannot delete on a surface with these caps,
// and a short reason for the help text. It mirrors the engine's prune-skip
// predicate (engine.go: NoDelete || !PruneEligible || Delete==nil): prune is
// effective only on a PruneEligible surface, so anything else is a no-op. Keeping
// this one helper as the source of truth stops the runtime notice, the SOAR Short
// line, and surfaceNote from drifting apart.
func pruneNoOp(caps reconcile.Capabilities) (reason string, noop bool) {
	if caps.PruneEligible {
		return "", false
	}
	if caps.NoDelete {
		return "no delete API", true
	}
	return "not prune-eligible (no clean delete-by-id)", true
}

// surfaceCaps returns the reconcile Capabilities for a target if it is an engine
// surface (SIEM or SOAR), reading them with a nil client (Caps are static; the
// client is only used by the CUD closures at run time).
func surfaceCaps(target string) (reconcile.Capabilities, bool) {
	if s, ok := mirror.BuildSIEMSurface(target, nil); ok {
		return s.Caps, true
	}
	if s, ok := mirror.BuildSOARSurface(target, nil); ok {
		return s.Caps, true
	}
	return reconcile.Capabilities{}, false
}

// surfaceNote composes the target-specific help paragraph: the surface family's
// plane / version, the engine Capabilities that bear on a push (--prune, etag),
// and the hand-written prose gotcha. Returns "" when nothing is known about the
// target, so callers can append unconditionally.
func surfaceNote(target string) string {
	// rules-* targets all map to the single "rules" registry family.
	famName := target
	if strings.HasPrefix(target, "rules-") {
		famName = "rules"
	}

	var b strings.Builder
	if fams := mirror.SurfaceFamilyByName(famName); len(fams) > 0 {
		f := fams[0]
		fmt.Fprintf(&b, "  plane: %s · host: %s · auth: %s", f.Plane, f.Host, f.Auth)
		if f.APIVersion != "" {
			fmt.Fprintf(&b, " · %s", f.APIVersion)
		}
		b.WriteString("\n")
	}
	if caps, ok := surfaceCaps(target); ok {
		if reason, noop := pruneNoOp(caps); noop {
			fmt.Fprintf(&b, "  --prune: no-op — %s; live-only objects are reported, never deleted\n", reason)
		} else {
			b.WriteString("  --prune: deletes live-only objects (guarded; gated on a complete pull)\n")
		}
		if caps.NoEtag {
			b.WriteString("  etag: none — no optimistic-concurrency guard on update\n")
		}
	}
	if p := surfaceProse[target]; p != "" {
		b.WriteString("  " + p + "\n")
	}
	if b.Len() == 0 {
		return ""
	}
	return "\nTarget `" + target + "`:\n" + b.String()
}

// attachTargetHelp makes `<cmd> <target> --help` append the target's behavior note
// to the standard help, so surface-specific write semantics are visible at the
// point of use instead of only in a design doc. validTargets bounds which
// positional tokens are recognized (so a flag value or typo prints nothing extra).
func attachTargetHelp(cmd *cobra.Command, validTargets []string) {
	valid := make(map[string]struct{}, len(validTargets))
	for _, t := range validTargets {
		valid[t] = struct{}{}
	}
	base := cmd.HelpFunc() // the inherited default; captured before we override it
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		base(c, args)
		// Find a known target token. The `<cmd> <target> --help` form passes it in
		// args; the `help <cmd> <target>` form passes empty args (cobra never parses
		// the target command's flags), so fall back to os.Args. The valid-set gate
		// keeps an unrelated token (a flag value) from matching.
		toks := append(append(append([]string{}, args...), c.Flags().Args()...), os.Args[1:]...)
		for _, a := range toks {
			if _, ok := valid[a]; ok {
				fmt.Fprint(c.OutOrStdout(), surfaceNote(a))
				return
			}
		}
	})
}
