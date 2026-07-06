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

func TestDetectDroppedSteps(t *testing.T) {
	tests := []struct {
		name    string
		sub     any
		resp    any
		dropped []string
	}{
		{
			name: "no steps dropped",
			sub: map[string]any{"steps": []map[string]any{
				{"identifier": "aaa", "instanceName": "Step1"},
				{"identifier": "bbb", "instanceName": "Step2"},
			}},
			resp: map[string]any{"steps": []map[string]any{
				{"identifier": "aaa", "instanceName": "Step1"},
				{"identifier": "bbb", "instanceName": "Step2"},
			}},
			dropped: nil,
		},
		{
			name: "one step dropped by identifier",
			sub: map[string]any{"steps": []map[string]any{
				{"identifier": "aaa", "instanceName": "Step1"},
				{"identifier": "bbb", "instanceName": "Step2"},
				{"identifier": "ccc", "instanceName": "NewStep"},
			}},
			resp: map[string]any{"steps": []map[string]any{
				{"identifier": "aaa", "instanceName": "Step1"},
				{"identifier": "bbb", "instanceName": "Step2"},
			}},
			dropped: []string{"NewStep"},
		},
		{
			name: "multiple steps dropped",
			sub: map[string]any{"steps": []map[string]any{
				{"identifier": "aaa", "instanceName": "Step1"},
				{"identifier": "bbb", "instanceName": "StepB"},
				{"identifier": "ccc", "instanceName": "StepC"},
			}},
			resp: map[string]any{"steps": []map[string]any{
				{"identifier": "aaa", "instanceName": "Step1"},
			}},
			dropped: []string{"StepB", "StepC"},
		},
		{
			name: "new step with empty identifier uses instanceName",
			sub: map[string]any{"steps": []map[string]any{
				{"identifier": "aaa", "instanceName": "Step1"},
				{"identifier": "", "instanceName": "BrandNew"},
			}},
			resp: map[string]any{"steps": []map[string]any{
				{"identifier": "aaa", "instanceName": "Step1"},
			}},
			dropped: []string{"BrandNew"},
		},
		{
			name: "response has more steps — no drop",
			sub: map[string]any{"steps": []map[string]any{
				{"identifier": "aaa", "instanceName": "Step1"},
			}},
			resp: map[string]any{"steps": []map[string]any{
				{"identifier": "aaa", "instanceName": "Step1"},
				{"identifier": "bbb", "instanceName": "Step2"},
			}},
			dropped: nil,
		},
		{
			name:    "empty steps in submitted",
			sub:     map[string]any{"steps": []map[string]any{}},
			resp:    map[string]any{"steps": []map[string]any{}},
			dropped: nil,
		},
		{
			name:    "invalid JSON returns nil",
			sub:     "not json",
			resp:    "not json",
			dropped: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subJSON, _ := json.Marshal(tt.sub)
			respJSON, _ := json.Marshal(tt.resp)
			got := detectDroppedSteps(json.RawMessage(subJSON), json.RawMessage(respJSON))
			if !slices.Equal(got, tt.dropped) {
				t.Errorf("detectDroppedSteps() = %v, want %v", got, tt.dropped)
			}
		})
	}
}
