package cli

import (
	"fmt"
	"os"
)

// preferModern is the standard dispatch for a SOAR function that BOTH API
// generations can serve (the New-vs-Legacy axis): it runs the modern v1alpha path
// by default and auto-falls back to the legacy AppKey path on error. The global
// --legacy flag forces the legacy path only (modern skipped). surface names the
// command for the fallback message (e.g. "soar case list").
//
// This is the single place the New-vs-Legacy choice lives, so every dual-generation
// surface behaves identically and switches together via --legacy. Each fn must
// FETCH and EMIT and return an error — a read fn fetches before printing so a fetch
// failure falls back cleanly (no half-printed output).
//
// Promote a surface to this dispatch only once its modern path is live-validated
// (the registry Status reaches validated; see docs/ROADMAP Wave 13). Today
// `soar case list` is the validated consumer; surfaces whose modern path is not
// validated (the reconcile engine, the case verbs) stay on the reliable legacy
// path by design.
func preferModern(surface string, modernFn, legacyFn func() error) error {
	if forceLegacy {
		return legacyFn()
	}
	if err := modernFn(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: modern v1alpha path failed (%v) — falling back to legacy\n", surface, err)
		return legacyFn()
	}
	return nil
}
