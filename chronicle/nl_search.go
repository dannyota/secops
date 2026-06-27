package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Natural-language search: translate an English question into a UDM query, then
// (optionally) run it. Like UDM search and the rest of the per-instance surface,
// these endpoints use the project ID (string) form — numeric=false — matching
// the wrapper, which builds every instance URL from the string project_id.

// translateUDMRequest is the body for :translateUdmQuery — {"text": "..."}.
type translateUDMRequest struct {
	Text string `json:"text"`
}

// translateUDMResponse is the :translateUdmQuery reply. The API returns the
// generated query in "query"; an optional "timeRange" carries the window the
// model inferred from the text (e.g. "in the last hour"); on a soft failure it
// instead returns "message" (e.g. "could not generate a query"). "message" can
// also accompany a successful "query" as a low-confidence note.
type translateUDMResponse struct {
	Query     string           `json:"query,omitempty"`
	Message   string           `json:"message,omitempty"`
	TimeRange *searchTimeRange `json:"timeRange,omitempty"`
}

// NLToUDMResult is the full result of translating natural language to a UDM query.
type NLToUDMResult struct {
	Query     string        `json:"query"`               // generated UDM query (non-empty on success)
	Message   string        `json:"message,omitempty"`   // optional model note; may be set even on success
	TimeRange *TimeInterval `json:"timeRange,omitempty"` // model-inferred window; nil if no range named
}

// TranslateNLToUDM translates a natural-language question into a UDM search
// query string and returns ONLY that query (no search is run). For the model's
// inferred time range as well, use TranslateNLToUDMWithTimeRange.
func (c *Client) TranslateNLToUDM(ctx context.Context, text string) (string, error) {
	res, err := c.TranslateNLToUDMWithTimeRange(ctx, text)
	if err != nil {
		return "", err
	}
	return res.Query, nil
}

// TranslateNLToUDMWithTimeRange translates a natural-language question into a UDM
// query and also returns the time range the model inferred from the text (nil
// when the text named no window).
//
// Endpoint: POST {instance}:translateUdmQuery with body {"text": text}. The reply
// is {"query": "...", "timeRange": {...}}; an empty/non-generatable result comes
// back as {"message": "..."}, which we surface as an error rather than "".
//
// DEVIATION: the wrapper drops the model's timeRange (returns only the query) and
// bolts an undocumented retry loop onto nl_search. We additionally surface the
// timeRange + message, and keep translation a single predictable call — the
// foundation's do() already retries genuine 429/5xx.
func (c *Client) TranslateNLToUDMWithTimeRange(ctx context.Context, text string) (*NLToUDMResult, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("chronicle: TranslateNLToUDM requires non-empty text")
	}

	// RPC-style method: ":translateUdmQuery" is appended directly to the
	// instance path with no separating slash (see SearchUDM for the same shape).
	path := c.instancePath(false) + ":translateUdmQuery"

	var resp translateUDMResponse
	if err := c.post(ctx, path, translateUDMRequest{Text: text}, &resp); err != nil {
		return nil, err
	}
	if resp.Query == "" {
		msg := resp.Message
		if msg == "" {
			msg = "no UDM query generated"
		}
		return nil, fmt.Errorf("chronicle: TranslateNLToUDM: %s", msg)
	}
	res := &NLToUDMResult{Query: resp.Query, Message: resp.Message}
	if resp.TimeRange != nil {
		var ti TimeInterval
		if t, err := time.Parse(time.RFC3339, resp.TimeRange.StartTime); err == nil {
			ti.StartTime = t
		}
		if t, err := time.Parse(time.RFC3339, resp.TimeRange.EndTime); err == nil {
			ti.EndTime = t
		}
		if !ti.StartTime.IsZero() || !ti.EndTime.IsZero() {
			res.TimeRange = &ti
		}
	}
	return res, nil
}

// NLSearch translates a natural-language question to UDM and runs the resulting
// search over [start, end], returning matching events as raw JSON (one
// json.RawMessage per event), exactly like SearchUDM.
//
// limit caps the number of events; <=0 lets the server decide. This is the
// convenience composition of TranslateNLToUDM + SearchUDM; call them separately
// if you want to inspect or tweak the generated query before running it.
func (c *Client) NLSearch(ctx context.Context, text string, start, end time.Time, limit int) ([]json.RawMessage, error) {
	query, err := c.TranslateNLToUDM(ctx, text)
	if err != nil {
		return nil, err
	}
	return c.SearchUDM(ctx, query, start, end, limit)
}
