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
// generated query in "query"; on a soft failure it instead returns "message"
// (e.g. "could not generate a query"), which the wrapper surfaces as an error.
type translateUDMResponse struct {
	Query   string `json:"query,omitempty"`
	Message string `json:"message,omitempty"`
}

// TranslateNLToUDM translates a natural-language question into a UDM search
// query string and returns ONLY that query (no search is run).
//
// Endpoint: POST {instance}:translateUdmQuery with body {"text": text}. The
// reply is {"query": "..."}; an empty/non-generatable result comes back as
// {"message": "..."}, which we surface as an error rather than returning "".
//
// DEVIATION: the wrapper has an undocumented retry loop bolted onto nl_search
// (not the translator). We keep translation a single, predictable call — the
// foundation's do() already retries genuine 429/5xx — and report a clear error
// when the model declines to produce a query, instead of silently returning "".
func (c *Client) TranslateNLToUDM(ctx context.Context, text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("chronicle: TranslateNLToUDM requires non-empty text")
	}

	// RPC-style method: ":translateUdmQuery" is appended directly to the
	// instance path with no separating slash (see SearchUDM for the same shape).
	path := c.instancePath(false) + ":translateUdmQuery"

	var resp translateUDMResponse
	if err := c.post(ctx, path, translateUDMRequest{Text: text}, &resp); err != nil {
		return "", err
	}
	if resp.Query == "" {
		msg := resp.Message
		if msg == "" {
			msg = "no UDM query generated"
		}
		return "", fmt.Errorf("chronicle: TranslateNLToUDM: %s", msg)
	}
	return resp.Query, nil
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
