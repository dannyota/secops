package mirror

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPromoteRuleRefusesTrackedFile asserts promote refuses a .yaral that already
// has a companion .yaml (a tracked rule) before any API call — so a nil client is
// never dereferenced.
func TestPromoteRuleRefusesTrackedFile(t *testing.T) {
	dir := t.TempDir()
	yaral := filepath.Join(dir, "r.yaral")
	if err := os.WriteFile(yaral, []byte("rule r {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "r.yaml"), []byte("rule_id: ru_x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := PromoteRule(context.Background(), nil, yaral, DefaultRulesCreateDeploymentOptions(), true, false, os.Stdout)
	if err == nil {
		t.Fatal("expected refusal for a file with a companion .yaml")
	}
	if n != 0 {
		t.Errorf("promoted = %d, want 0", n)
	}
	if !strings.Contains(err.Error(), "companion") {
		t.Errorf("error %q should mention the companion conflict", err)
	}
}
