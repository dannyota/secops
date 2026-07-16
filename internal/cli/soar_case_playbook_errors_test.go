package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestFlexibleString(t *testing.T) {
	cases := []struct{ raw, want string }{
		{`"ACTION"`, "ACTION"},
		{`5`, "5"},
		{`0`, "0"},
	}
	for _, tc := range cases {
		var f flexibleString
		if err := json.Unmarshal([]byte(tc.raw), &f); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.raw, err)
		}
		if f.String() != tc.want {
			t.Errorf("flexibleString(%s) = %q, want %q", tc.raw, f, tc.want)
		}
	}
}

func TestWFStatusLabel(t *testing.T) {
	for code, want := range map[string]string{
		"0": "FAULTED",
		"1": "IN_PROGRESS",
		"2": "COMPLETED",
	} {
		if got := wfStatusLabel(code); got != want {
			t.Errorf("wfStatusLabel(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestIsBlockAndIsNoStatus(t *testing.T) {
	if !isBlock("BLOCK") || !isBlock("5") || isBlock("ACTION") {
		t.Error("isBlock must accept both the string and numeric forms")
	}
	if !isNoStatus("") || !isNoStatus("0") || !isNoStatus("NO_STATUS") || isNoStatus("2") {
		t.Error("isNoStatus mapping wrong")
	}
}

func TestSplitTrailingJSON(t *testing.T) {
	cases := []struct {
		name, msg, wantText, wantJSON string
	}{
		{"trailing object", `step failed {"error":"x"}`, "step failed", `{"error":"x"}`},
		{"nested object", `x {"a":{"b":1}}`, "x", `{"a":{"b":1}}`},
		{"escaped quotes stay balanced", `boom {"msg":"a \"q\" b"}`, "boom", `{"msg":"a \"q\" b"}`},
		{"pure json", `{"a":1}`, "", `{"a":1}`},
		{"no json", "plain text", "plain text", ""},
		{"invalid trailing braces", "text {not json}", "text {not json}", ""},
	}
	for _, tc := range cases {
		text, js := splitTrailingJSON(tc.msg)
		if text != tc.wantText || js != tc.wantJSON {
			t.Errorf("%s: splitTrailingJSON(%q) = (%q, %q), want (%q, %q)",
				tc.name, tc.msg, text, js, tc.wantText, tc.wantJSON)
		}
	}
}

func TestWriteWrapped(t *testing.T) {
	var b bytes.Buffer
	writeWrapped(&b, "0123456789 0123456789 0123456789 0123456789 0123456789", "", 0)
	want := "0123456789 0123456789 0123456789\n0123456789 0123456789\n"
	if b.String() != want {
		t.Errorf("writeWrapped = %q, want %q", b.String(), want)
	}

	b.Reset()
	writeWrapped(&b, "one", "> ", 80)
	if b.String() != "> one\n" {
		t.Errorf("prefixed single word = %q", b.String())
	}
}
