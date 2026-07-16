package cli

import (
	"encoding/json"
	"errors"
	"testing"
)

// renderSystemInfo must surface a total failure as an error — preferModern
// only falls back to the legacy path on a non-nil return.
func TestRenderSystemInfoAllErrors(t *testing.T) {
	e := errors.New("unreachable")
	if err := renderSystemInfo(nil, e, nil, e, nil, e); err == nil {
		t.Fatal("want an error when every system call failed")
	}
}

func TestRenderSystemInfoPartialSuccess(t *testing.T) {
	old := jsonOut
	jsonOut = true
	defer func() { jsonOut = old }()
	ver := json.RawMessage(`{"payload":"0.0.0"}`)
	if err := renderSystemInfo(ver, nil, nil, errors.New("x"), nil, errors.New("y")); err != nil {
		t.Fatalf("partial success must render, got %v", err)
	}
}
