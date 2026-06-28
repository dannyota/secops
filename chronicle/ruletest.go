package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// RuleTestResult holds the outcome of a RunTestRule dry-run. Detections are the
// matches the rule would produce over the window; CompilationErrors are YARA-L
// compile problems; Items is every raw stream element (detections, progress,
// errors) as the server sent them, for callers that want the full stream.
type RuleTestResult struct {
	Detections        []json.RawMessage
	CompilationErrors []json.RawMessage
	RuntimeErrors     []json.RawMessage
	Items             []json.RawMessage
}

// RunTestRule dry-runs YARA-L rule text against historical data over [start, end]
// WITHOUT creating the rule, returning the detections it would have produced. This
// goes beyond ValidateRule (which only compile-checks): it previews real matches,
// so a push pipeline can verify a rule before deploying it. Read-only (no rule is
// created or stored). maxResults is clamped to [1, 10000].
//
// Endpoint: POST {instance}/legacy:legacyRunTestRule, body {ruleText, timeRange:
// {startTime,endTime}, maxResults, scope}. The response is a JSON array of stream
// items; each is classified into Detections / CompilationErrors / all-Items.
func (c *Client) RunTestRule(ctx context.Context, ruleText string, start, end time.Time, maxResults int) (*RuleTestResult, error) {
	if !start.Before(end) {
		return nil, fmt.Errorf("chronicle: RunTestRule start (%s) must be before end (%s)",
			start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	}
	maxResults = min(max(maxResults, 1), 10000)

	body := map[string]any{
		"ruleText": ruleText,
		"timeRange": map[string]string{
			"startTime": start.UTC().Format("2006-01-02T15:04:05Z"),
			"endTime":   end.UTC().Format("2006-01-02T15:04:05Z"),
		},
		"maxResults": maxResults,
		"scope":      "",
	}

	var items []json.RawMessage
	if err := c.post(ctx, c.resourcePath("legacy:legacyRunTestRule", false), body, &items); err != nil {
		return nil, err
	}

	res := &RuleTestResult{Items: items}
	for _, it := range items {
		ch := classifyRuleTestChunk(it)
		switch {
		case len(ch.Detection) > 0:
			res.Detections = append(res.Detections, ch.Detection)
		case len(ch.CompilationError) > 0:
			res.CompilationErrors = append(res.CompilationErrors, ch.CompilationError)
		case len(ch.RuntimeError) > 0:
			res.RuntimeErrors = append(res.RuntimeErrors, ch.RuntimeError)
		}
	}
	return res, nil
}

// RuleTestChunk is one element of the streaming rule-test response. A progress
// chunk sets ProgressPercent >= 0; a result chunk carries a Detection; a compile
// problem carries a CompilationError. Raw is always the full element.
type RuleTestChunk struct {
	ProgressPercent   int             // 0..100 for a progress chunk, else -1
	Detection         json.RawMessage // non-empty when this chunk is a detection
	CompilationError  json.RawMessage // non-empty on a compile error (ruleCompilationError)
	RuntimeError      json.RawMessage // non-empty on a runtime error (ruleError)
	TooManyDetections bool            // server truncated detections at maxResults
	Raw               json.RawMessage // the full raw element
}

// classifyRuleTestChunk maps one raw stream element to a RuleTestChunk. The
// legacyRunTestRule stream interleaves four element kinds — progressPercent,
// detection, ruleCompilationError (compile), and ruleError (runtime) — plus a
// tooManyDetections truncation flag.
func classifyRuleTestChunk(raw json.RawMessage) RuleTestChunk {
	ch := RuleTestChunk{ProgressPercent: -1, Raw: raw}
	var probe struct {
		ProgressPercent      *int            `json:"progressPercent"`
		Detection            json.RawMessage `json:"detection"`
		RuleCompilationError json.RawMessage `json:"ruleCompilationError"`
		RuleError            json.RawMessage `json:"ruleError"`
		TooManyDetections    bool            `json:"tooManyDetections"`
	}
	if json.Unmarshal(raw, &probe) == nil {
		if probe.ProgressPercent != nil {
			ch.ProgressPercent = *probe.ProgressPercent
		}
		ch.Detection = probe.Detection
		ch.CompilationError = probe.RuleCompilationError
		ch.RuntimeError = probe.RuleError
		ch.TooManyDetections = probe.TooManyDetections
	}
	return ch
}

// StreamTestRule dry-runs YARA-L over [start, end] like RunTestRule, but decodes
// the response incrementally, invoking onChunk for each element AS IT ARRIVES
// (progress updates, detections, errors) so a caller can show live progress and
// first results without waiting for the whole window to scan. Read-only (no rule
// created or stored). maxResults is clamped to [1, 10000]. onChunk returning a
// non-nil error stops the stream and is returned.
//
// Endpoint: POST {instance}/legacy:legacyRunTestRule (the same streaming endpoint
// RunTestRule buffers), same body. No retry (non-idempotent streaming POST).
func (c *Client) StreamTestRule(ctx context.Context, ruleText string, start, end time.Time, maxResults int, onChunk func(RuleTestChunk) error) error {
	if !start.Before(end) {
		return fmt.Errorf("chronicle: StreamTestRule start (%s) must be before end (%s)",
			start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	}
	maxResults = min(max(maxResults, 1), 10000)
	body := map[string]any{
		"ruleText": ruleText,
		"timeRange": map[string]string{
			"startTime": start.UTC().Format("2006-01-02T15:04:05Z"),
			"endTime":   end.UTC().Format("2006-01-02T15:04:05Z"),
		},
		"maxResults": maxResults,
		"scope":      "",
	}
	return c.streamArray(ctx, c.resourcePath("legacy:legacyRunTestRule", false), body, func(raw json.RawMessage) error {
		return onChunk(classifyRuleTestChunk(raw))
	})
}
