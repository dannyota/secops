package mirror

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValueRedactor(t *testing.T) {
	r, err := NewValueRedactor([]string{`sig=[^&"]+`, "  ", "# a comment"})
	if err != nil {
		t.Fatal(err)
	}
	body := json.RawMessage(`{"url":"https://example.com/x?sig=SECRETTOKEN&a=1","n":42,"keep":"plain"}`)
	out, err := r.RedactJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "SECRETTOKEN") {
		t.Errorf("secret not masked: %s", s)
	}
	if !strings.Contains(s, redactedMarker) {
		t.Errorf("marker not present: %s", s)
	}
	if !strings.Contains(s, `"keep":"plain"`) {
		t.Errorf("non-matching value altered: %s", s)
	}
	// int64-ish numbers survive (UseNumber, no float corruption).
	if !strings.Contains(s, `"n":42`) {
		t.Errorf("number altered: %s", s)
	}
}

func TestValueRedactorNilIsNoOp(t *testing.T) {
	var r *ValueRedactor // empty patterns → NewValueRedactor returns nil
	body := json.RawMessage(`{"url":"https://example.com?sig=x"}`)
	out, err := r.RedactJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(body) {
		t.Errorf("nil redactor altered body: %s", out)
	}

	none, err := NewValueRedactor([]string{"", "  ", "# only comments"})
	if err != nil {
		t.Fatal(err)
	}
	if none != nil {
		t.Errorf("expected nil redactor for no usable patterns, got %v", none)
	}
}

func TestValueRedactorInvalidPattern(t *testing.T) {
	if _, err := NewValueRedactor([]string{"("}); err == nil {
		t.Error("expected error for invalid regex")
	}
}
