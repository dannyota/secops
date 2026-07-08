package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

func TestDetectSaveDrift(t *testing.T) {
	tests := []struct {
		name   string
		sub    any
		resp   any
		issues []string
	}{
		{
			name: "no drift",
			sub: map[string]any{
				"steps": []map[string]any{
					{"identifier": "aaa", "instanceName": "Step1"},
					{"identifier": "bbb", "instanceName": "Step2"},
				},
				"stepsRelations": []map[string]any{{"fromStep": "aaa", "toStep": "bbb"}},
			},
			resp: map[string]any{
				"steps": []map[string]any{
					{"identifier": "aaa", "instanceName": "Step1"},
					{"identifier": "bbb", "instanceName": "Step2"},
				},
				"stepsRelations": []map[string]any{{"fromStep": "aaa", "toStep": "bbb"}},
			},
		},
		{
			name: "one step dropped",
			sub: map[string]any{"steps": []map[string]any{
				{"identifier": "aaa", "instanceName": "Step1"},
				{"identifier": "ccc", "instanceName": "NewStep"},
			}},
			resp: map[string]any{"steps": []map[string]any{
				{"identifier": "aaa", "instanceName": "Step1"},
			}},
			issues: []string{`step "NewStep" was dropped`},
		},
		{
			name: "server reassigned identifiers — no false positive",
			sub: map[string]any{"steps": []map[string]any{
				{"identifier": "aaa", "instanceName": "Step1"},
				{"identifier": "bbb", "instanceName": "NewStep"},
			}},
			resp: map[string]any{"steps": []map[string]any{
				{"identifier": "xxx", "instanceName": "Step1"},
				{"identifier": "yyy", "instanceName": "NewStep"},
			}},
		},
		{
			name: "relation dropped",
			sub: map[string]any{
				"steps":          []map[string]any{{"identifier": "a", "instanceName": "S1"}},
				"stepsRelations": []map[string]any{{"fromStep": "a", "toStep": "b"}, {"fromStep": "b", "toStep": "c"}},
			},
			resp: map[string]any{
				"steps":          []map[string]any{{"identifier": "a", "instanceName": "S1"}},
				"stepsRelations": []map[string]any{{"fromStep": "a", "toStep": "b"}},
			},
			issues: []string{"1 of 2 relation(s) were dropped"},
		},
		{
			name: "steps and relations dropped",
			sub: map[string]any{
				"steps": []map[string]any{
					{"identifier": "a", "instanceName": "S1"},
					{"identifier": "b", "instanceName": "S2"},
				},
				"stepsRelations": []map[string]any{{"fromStep": "a", "toStep": "b"}},
			},
			resp: map[string]any{
				"steps":          []map[string]any{{"identifier": "a", "instanceName": "S1"}},
				"stepsRelations": []map[string]any{},
			},
			issues: []string{`step "S2" was dropped`, "1 of 1 relation(s) were dropped"},
		},
		{
			name:   "invalid JSON returns nil",
			sub:    "not json",
			resp:   "not json",
			issues: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subJSON, _ := json.Marshal(tt.sub)
			respJSON, _ := json.Marshal(tt.resp)
			got := detectSaveDrift(json.RawMessage(subJSON), json.RawMessage(respJSON))
			if !slices.Equal(got, tt.issues) {
				t.Errorf("detectSaveDrift() = %v, want %v", got, tt.issues)
			}
		})
	}
}
