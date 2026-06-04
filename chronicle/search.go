package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// SearchUDM runs a point-in-time UDM event search over [start, end] and returns
// the matching events as raw JSON, one json.RawMessage per event.
//
// Each event mirrors the API's element shape, typically:
//
//	{"name": "...", "udm": {"metadata": {...}, "principal": {...}, ...}}
//
// The full response is {"events": [...], "moreDataAvailable": bool}; only the
// events are returned. If more data than maxEvents was available the extra rows
// are silently dropped server-side — raise maxEvents or narrow the time range.
//
// Endpoint: GET {instance}:udmSearch with query params query,
// timeRange.start_time, timeRange.end_time (RFC3339) and limit.
//
// DEVIATION: events are returned as []json.RawMessage rather than decoded into a
// map[string]any. UDM is a large, evolving, deeply-nested schema; keeping each
// event raw lets callers mirror it verbatim or unmarshal into their own types
// without this SDK chasing every UDM field.
//
// The project ID (string) form is used (numeric=false), matching the legacy
// tool, which ran UDM search through the SDK instance built from the string
// project_id. (Only curated rule-set categories/sets and parsers need the
// project NUMBER — see resource.go.)
func (c *Client) SearchUDM(ctx context.Context, query string, start, end time.Time, maxEvents int) ([]json.RawMessage, error) {
	if query == "" {
		return nil, fmt.Errorf("chronicle: SearchUDM requires a non-empty query")
	}
	if !start.Before(end) {
		return nil, fmt.Errorf("chronicle: SearchUDM start (%s) must be before end (%s)",
			start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	}

	q := url.Values{
		"query":                {query},
		"timeRange.start_time": {start.UTC().Format(time.RFC3339)},
		"timeRange.end_time":   {end.UTC().Format(time.RFC3339)},
	}
	if maxEvents > 0 {
		q.Set("limit", fmt.Sprintf("%d", maxEvents))
	}

	// RPC-style method: ":udmSearch" is appended directly to the instance path
	// with no separating slash.
	path := c.instancePath(false) + ":udmSearch"

	var resp struct {
		Events            []json.RawMessage `json:"events"`
		MoreDataAvailable bool              `json:"moreDataAvailable"`
	}
	if err := c.get(ctx, path, &resp, withQuery(q)); err != nil {
		return nil, err
	}
	return resp.Events, nil
}
