package cli

import (
	"fmt"
	"os"
)

// preferModern runs the modern v1alpha path by default, falling back to the legacy
// AppKey path on error — the Wave 13 "modern by default, legacy as fallback"
// pattern. The global --legacy flag forces the legacy path only (modern skipped).
//
// Each fn fetches AND emits and returns an error; a list/get fn must fetch before
// printing so a fetch failure falls back cleanly (no half-printed output). When
// modern fails, the error is noted on stderr and legacy runs.
func preferModern(modernFn, legacyFn func() error) error {
	if forceLegacy {
		return legacyFn()
	}
	if err := modernFn(); err != nil {
		fmt.Fprintf(os.Stderr, "modern v1alpha path failed (%v) — falling back to legacy\n", err)
		return legacyFn()
	}
	return nil
}
