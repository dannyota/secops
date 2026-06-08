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
