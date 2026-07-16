package mirror

import (
	"reflect"
	"testing"
)

func TestStripSecrets(t *testing.T) {
	in := map[string]any{
		"name":     "conn",
		"password": "hunter2",
		"Password": "case-insensitive-too",
		"nested": map[string]any{
			"api_key": "k",
			"keep":    "v",
		},
		"list": []any{
			map[string]any{"token": "t", "id": 7},
			"plain",
		},
	}
	got := stripSecrets(in).(map[string]any)
	want := map[string]any{
		"name":     "conn",
		"password": redactedMarker,
		"Password": redactedMarker,
		"nested": map[string]any{
			"api_key": redactedMarker,
			"keep":    "v",
		},
		"list": []any{
			map[string]any{"token": redactedMarker, "id": 7},
			"plain",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stripSecrets = %#v, want %#v", got, want)
	}
	if in["password"] != "hunter2" {
		t.Error("stripSecrets mutated its input")
	}
}

func TestStripSecretsPassesScalarsThrough(t *testing.T) {
	for _, v := range []any{"s", 42, true, nil} {
		if got := stripSecrets(v); !reflect.DeepEqual(got, v) {
			t.Errorf("stripSecrets(%#v) = %#v", v, got)
		}
	}
}
