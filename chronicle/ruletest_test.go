package chronicle

import (
	"encoding/json"
	"testing"
	"time"
)

func TestClassifyRuleTestChunk(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		check func(RuleTestChunk) bool
	}{
		{
			"progress zero is preserved", `{"progressPercent":0}`,
			func(c RuleTestChunk) bool { return c.ProgressPercent == 0 },
		},
		{
			"progress mid-scan", `{"progressPercent":55}`,
			func(c RuleTestChunk) bool { return c.ProgressPercent == 55 },
		},
		{
			"no progress field means -1", `{"detection":{"id":"d1"}}`,
			func(c RuleTestChunk) bool { return c.ProgressPercent == -1 && c.Detection != nil },
		},
		{
			"compilation error", `{"ruleCompilationError":{"message":"bad"}}`,
			func(c RuleTestChunk) bool { return c.CompilationError != nil && c.RuntimeError == nil },
		},
		{
			"runtime error", `{"ruleError":{"message":"boom"}}`,
			func(c RuleTestChunk) bool { return c.RuntimeError != nil && c.CompilationError == nil },
		},
		{
			"too many detections", `{"tooManyDetections":true}`,
			func(c RuleTestChunk) bool { return c.TooManyDetections },
		},
		{
			"unparseable chunk keeps raw and defaults", `not-json`,
			func(c RuleTestChunk) bool {
				return c.ProgressPercent == -1 && c.Detection == nil && string(c.Raw) == "not-json"
			},
		},
	}
	for _, tc := range cases {
		if got := classifyRuleTestChunk(json.RawMessage(tc.raw)); !tc.check(got) {
			t.Errorf("%s: classifyRuleTestChunk(%s) = %+v", tc.name, tc.raw, got)
		}
	}
}

func TestStreamTestRuleRejectsBadWindow(t *testing.T) {
	c := &Client{}
	now := time.Now()
	err := c.StreamTestRule(t.Context(), "rule r {}", now, now, 10, func(RuleTestChunk) error { return nil })
	if err == nil {
		t.Fatal("StreamTestRule must reject start >= end before any request")
	}
}
