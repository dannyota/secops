package cli

import (
	"encoding/json"
	"fmt"
)

// emitJSON prints v as indented JSON to stdout — the machine-readable output for
// the global --json flag. The shape is per-command (documented in usage.md).
func emitJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// emitGuardedResult prints the {action, dry_run, applied} result for a guarded
// mutation under --json (the dry-run / refuse / apply outcome), keeping stdout
// pure JSON. applied = the mutation actually ran.
func emitGuardedResult(action string, dryRun, applied bool) error {
	return emitJSON(struct {
		Action  string `json:"action"`
		DryRun  bool   `json:"dry_run"`
		Applied bool   `json:"applied"`
		OK      bool   `json:"ok"`
	}{Action: action, DryRun: dryRun, Applied: applied, OK: true})
}
