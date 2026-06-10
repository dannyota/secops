package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"danny.vn/secops/internal/userdir"
)

// The agent-safety layer: a hard read-only mode and a local audit log of guard
// decisions. Both hook the few guard funnels every tenant mutation flows
// through (soarGuard, guardedSIEMMutation, the push derivation, and the legacy
// escape hatch), so no per-verb wiring is needed.

// readOnlyFlag is the global --read-only persistent flag (see root.go).
var readOnlyFlag bool

// readOnlyMode reports whether this session is hard read-only: the global
// --read-only flag, or SECOPS_READONLY set in the environment. Set the env var
// in the environment that LAUNCHES an autonomous agent so an investigation
// session cannot deploy — every guarded mutation degrades to a dry-run preview
// even when --yes is passed. (This is a guardrail against unintended mutation,
// not a security boundary: the credentials themselves still permit writes, and
// the legacy escape hatch's `--read` assertion on a POST is caller-trusted —
// see `soar legacy call`.)
//
// The env parse FAILS CLOSED: any value other than an explicit falsy
// (empty / 0 / false / no / off) enables the cap, so a mis-spelled truthy
// ("on", "enabled", "y") never silently leaves a session write-capable.
func readOnlyMode() bool {
	if readOnlyFlag {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SECOPS_READONLY"))) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// deriveGuard is the shared decision core of every guarded mutation: dry-run by
// default, --yes (or the interactive confirm) to apply, the read-only degrade,
// and the audit record for a confirmed mutation. Callers render their own
// preview/banner/output around the returned state — soarGuard,
// guardedSIEMMutation, and the cleanup JSON path all delegate here so guard
// semantics cannot drift between planes.
func deriveGuard(action string, dryRunFlag, yesFlag bool) (dryRun, assumeYes bool) {
	if readOnlyMode() {
		if yesFlag {
			noteReadOnly(action)
			auditMutation(action, "read-only")
		}
		return true, false
	}
	dryRun = dryRunFlag || !yesFlag
	assumeYes = yesFlag && !dryRunFlag
	if !dryRun && !assumeYes && confirmPush(action) {
		assumeYes = true
	}
	if assumeYes {
		auditMutation(action, "confirmed")
	}
	return dryRun, assumeYes
}

// noteReadOnly prints the stderr notice when read-only mode blocks a confirmed
// mutation (degraded to a dry-run preview, or refused outright).
func noteReadOnly(action string) {
	fmt.Fprintf(os.Stderr, "read-only mode: refusing live mutation %q — --yes ignored (unset SECOPS_READONLY / drop --read-only to apply)\n", action)
}

// auditMutation appends one JSONL guard-decision record to
// $SECOPSCTL_HOME/audit.jsonl (0600). Logged decisions: "confirmed" (the
// command proceeded to call the live API — the guard's decision, not the
// server outcome) and "read-only" (a confirmed mutation was degraded). Dry
// runs are not logged. Best-effort: a write failure becomes a stderr note,
// never a command failure.
func auditMutation(action, decision string) {
	rec, err := json.Marshal(struct {
		Time     string `json:"time"`
		Action   string `json:"action"`
		Decision string `json:"decision"`
	}{Time: time.Now().UTC().Format(time.RFC3339), Action: action, Decision: decision})
	if err != nil {
		return
	}
	path := filepath.Join(userdir.Dir(), "audit.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "note: audit log not written: %v\n", err)
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: audit log not written: %v\n", err)
		return
	}
	_, werr := f.Write(append(rec, '\n'))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		fmt.Fprintf(os.Stderr, "note: audit log not written: %v\n", werr)
	}
}
