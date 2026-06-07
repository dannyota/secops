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
		var probe struct {
			Detection            json.RawMessage `json:"detection"`
			RuleCompilationError json.RawMessage `json:"ruleCompilationError"`
		}
		if err := json.Unmarshal(it, &probe); err != nil {
			continue
		}
		switch {
		case len(probe.Detection) > 0:
			res.Detections = append(res.Detections, probe.Detection)
		case len(probe.RuleCompilationError) > 0:
			res.CompilationErrors = append(res.CompilationErrors, probe.RuleCompilationError)
		}
	}
	return res, nil
}
