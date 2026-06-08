package mirror

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPushSOARPlaybookSaveDryRunValidatesName(t *testing.T) {
	file := filepath.Join(t.TempDir(), "playbook.json")
	if err := os.WriteFile(file, []byte(`{"name":"Bad/Name","templateName":null}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := PushSOARPlaybookSave(context.Background(), nil, file, true, false, &bytes.Buffer{})
	if err == nil {
		t.Fatal("PushSOARPlaybookSave accepted unsafe playbook name in dry-run")
	}
	if !strings.Contains(err.Error(), "invalid playbook name") {
		t.Fatalf("error = %v, want invalid playbook name", err)
	}
}

func TestPushSOARPlaybookSaveDryRunDoesNotNeedClient(t *testing.T) {
	file := filepath.Join(t.TempDir(), "playbook.json")
	if err := os.WriteFile(file, []byte(`{"name":"Good Name","templateName":null}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := PushSOARPlaybookSave(context.Background(), nil, file, true, false, &bytes.Buffer{}); err != nil {
		t.Fatalf("PushSOARPlaybookSave dry-run: %v", err)
	}
}
