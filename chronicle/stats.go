package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// Statistics aggregation over UDM events. Mirrors the wrapper's stats module,
// which issues a GET against the instance ":udmSearch" RPC with a stats-format
// query and post-processes the response's "stats" block into columns + rows.
//
// The project ID (string) form is used (numeric=false), matching the wrapper,
// which builds every instance URL from the string project_id. See resource.go.

// statsValue is a single scalar cell from a stats column. It is a tagged union
// in the API: exactly one of the *Val fields is set per value object.
//
//	{"int64Val": "12"} | {"doubleVal": 1.5} | {"stringVal": "x"} |
//	{"timestampVal": "2026-01-01T00:00:00Z"}
//
// int64Val arrives as a JSON string; doubleVal as a JSON number.
type statsValue struct {
	Int64Val     *string  `json:"int64Val,omitempty"`
	DoubleVal    *float64 `json:"doubleVal,omitempty"`
	StringVal    *string  `json:"stringVal,omitempty"`
	TimestampVal *string  `json:"timestampVal,omitempty"`
}

// scalar returns the cell as a json.RawMessage preserving the underlying type:
// a JSON number for int64Val/doubleVal, a JSON string for stringVal/timestampVal,
// or null when the union is empty. This keeps mixed-type columns honest — callers
// decode into whatever Go type they expect.
func (v statsValue) scalar() json.RawMessage {
	switch {
	case v.Int64Val != nil:
		// int64Val is a quoted integer over the wire; emit it as a bare JSON
		// number so consumers can decode into int64/float64 directly.
		return json.RawMessage(*v.Int64Val)
	case v.DoubleVal != nil:
		b, _ := json.Marshal(*v.DoubleVal)
		return json.RawMessage(b)
	case v.StringVal != nil:
		b, _ := json.Marshal(*v.StringVal)
		return json.RawMessage(b)
	case v.TimestampVal != nil:
		b, _ := json.Marshal(*v.TimestampVal)
		return json.RawMessage(b)
	default:
		return json.RawMessage("null")
	}
}

// statsCellRaw is one entry in a column's "values" array. It is either a single
// scalar ({"value": {...}}) or a list ({"list": {"values": [{...}, ...]}}), the
// latter produced by aggregations like array_distinct.
type statsCellRaw struct {
	Value *statsValue `json:"value,omitempty"`
	List  *struct {
		Values []statsValue `json:"values"`
	} `json:"list,omitempty"`
}

// raw returns the cell as a json.RawMessage: a scalar for single values, a JSON
// array for list cells, or null for an empty/unknown cell.
func (c statsCellRaw) raw() json.RawMessage {
	switch {
	case c.Value != nil:
		return c.Value.scalar()
	case c.List != nil:
		out := make([]json.RawMessage, 0, len(c.List.Values))
		for _, v := range c.List.Values {
			out = append(out, v.scalar())
		}
		b, _ := json.Marshal(out)
		return json.RawMessage(b)
	default:
		return json.RawMessage("null")
	}
}

// statsColumnRaw is one column in the API's stats.results array: a column name
// plus a parallel slice of cell values (column-major layout).
type statsColumnRaw struct {
	Column string         `json:"column"`
	Values []statsCellRaw `json:"values"`
}

// StatsResult is a stats aggregation transposed into row-major form.
//
// Columns preserves the API's column order. Each Row maps column name → cell
// value, where the value is a json.RawMessage so mixed-type and list-valued
// columns survive intact (a JSON number, string, or array). TotalRows is the
// row count (the longest column; short columns are null-padded).
//
// DEVIATION: the wrapper coerces every cell into a concrete Python type
// (int/float/str/datetime/list) at parse time, which is lossy and forces all
// callers onto its typing choices. We keep cells as raw JSON so a caller can
// decode each into exactly the Go type the column warrants.
type StatsResult struct {
	Columns   []string                     `json:"columns"`
	Rows      []map[string]json.RawMessage `json:"rows"`
	TotalRows int                          `json:"totalRows"`
}

// statsResponse is the relevant slice of the :udmSearch response for a stats
// query. Only the "stats.results" column array is consumed.
type statsResponse struct {
	Stats *struct {
		Results []statsColumnRaw `json:"results"`
	} `json:"stats"`
}

// GetStats runs a stats-format UDM query over [start, end] and returns the
// aggregation as typed columns + rows.
//
// query must be a Chronicle search in stats syntax (containing a `match`/`outcome`
// or similar stats projection). maxValues caps values returned per field (the
// API "limit"; wrapper default 60 when ≤ 0). timeout bounds this single request;
// when > 0 it overrides the client's default deadline via the context.
//
// Endpoint: GET {instance}:udmSearch with query params query,
// timeRange.start_time, timeRange.end_time (RFC3339 .000000Z, matching the
// wrapper) and limit. Non-2xx → *APIError. A response without a stats block is a
// non-stats query and yields an error.
func (c *Client) GetStats(ctx context.Context, query string, start, end time.Time, maxValues int, timeout time.Duration) (*StatsResult, error) {
	if query == "" {
		return nil, fmt.Errorf("chronicle: GetStats requires a non-empty query")
	}
	if !start.Before(end) {
		return nil, fmt.Errorf("chronicle: GetStats start (%s) must be before end (%s)",
			start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	}
	if maxValues <= 0 {
		maxValues = 60 // wrapper default
	}

	// The wrapper formats stats times with microsecond precision and a literal
	// trailing Z; honor that exactly.
	const statsTimeFmt = "2006-01-02T15:04:05.000000Z"
	q := url.Values{
		"query":                {query},
		"timeRange.start_time": {start.UTC().Format(statsTimeFmt)},
		"timeRange.end_time":   {end.UTC().Format(statsTimeFmt)},
		"limit":                {fmt.Sprintf("%d", maxValues)},
	}

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// RPC-style method: ":udmSearch" appends directly to the instance path.
	path := c.instancePath(false) + ":udmSearch"

	var resp statsResponse
	if err := c.get(ctx, path, &resp, withQuery(q)); err != nil {
		return nil, err
	}
	if resp.Stats == nil {
		return nil, fmt.Errorf("chronicle: GetStats: no stats in response (not a stats query?)")
	}
	return processStats(resp.Stats.Results), nil
}

// processStats transposes column-major API results into row-major StatsResult.
// The row count is the longest column; shorter columns are null-padded so every
// row has every column key.
func processStats(cols []statsColumnRaw) *StatsResult {
	res := &StatsResult{Columns: []string{}, Rows: []map[string]json.RawMessage{}}
	if len(cols) == 0 {
		return res
	}

	names := make([]string, 0, len(cols))
	cells := make(map[string][]json.RawMessage, len(cols))
	maxRows := 0
	for _, col := range cols {
		names = append(names, col.Column)
		vals := make([]json.RawMessage, 0, len(col.Values))
		for _, cell := range col.Values {
			vals = append(vals, cell.raw())
		}
		cells[col.Column] = vals
		if len(vals) > maxRows {
			maxRows = len(vals)
		}
	}

	res.Columns = names
	res.TotalRows = maxRows
	res.Rows = make([]map[string]json.RawMessage, 0, maxRows)
	for i := range maxRows {
		row := make(map[string]json.RawMessage, len(names))
		for _, name := range names {
			if vals := cells[name]; i < len(vals) {
				row[name] = vals[i]
			} else {
				row[name] = json.RawMessage("null")
			}
		}
		res.Rows = append(res.Rows, row)
	}
	return res
}
