package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
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
	Uint64Val    *string  `json:"uint64Val,omitempty"`
	DoubleVal    *float64 `json:"doubleVal,omitempty"`
	BoolVal      *bool    `json:"boolVal,omitempty"`
	StringVal    *string  `json:"stringVal,omitempty"`
	TimestampVal *string  `json:"timestampVal,omitempty"`
	NullVal      *bool    `json:"nullVal,omitempty"`
	// DateVal is a {year,month,day} object; kept as raw JSON so a date-typed column
	// survives intact rather than collapsing to null.
	DateVal json.RawMessage `json:"dateVal,omitempty"`
}

// scalar returns the cell as a json.RawMessage preserving the underlying type:
// a JSON number for int64Val/doubleVal, a JSON string for stringVal/timestampVal,
// or null when the union is empty. This keeps mixed-type columns honest — callers
// decode into whatever Go type they expect.
func (v statsValue) scalar() json.RawMessage {
	switch {
	case v.Int64Val != nil:
		// int64Val/uint64Val are quoted integers over the wire; emit them as a bare
		// JSON number so consumers can decode into int64/float64 directly.
		return json.RawMessage(*v.Int64Val)
	case v.Uint64Val != nil:
		return json.RawMessage(*v.Uint64Val)
	case v.DoubleVal != nil:
		b, _ := json.Marshal(*v.DoubleVal)
		return json.RawMessage(b)
	case v.BoolVal != nil:
		b, _ := json.Marshal(*v.BoolVal)
		return json.RawMessage(b)
	case v.StringVal != nil:
		b, _ := json.Marshal(*v.StringVal)
		return json.RawMessage(b)
	case v.TimestampVal != nil:
		b, _ := json.Marshal(*v.TimestampVal)
		return json.RawMessage(b)
	case len(v.DateVal) > 0:
		return v.DateVal
	default:
		// nullVal, or an unset/unhandled union member (bytesVal/protoVal).
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
	// Warnings carries non-fatal runtime notices the backend returned alongside the
	// rows (e.g. a default-row-limit truncation), so a partial result is never shown
	// as complete. Empty unless the execute path reported a WARNING.
	Warnings []string `json:"warnings,omitempty"`
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
	if err := validateStatsWindow("GetStats", query, start, end); err != nil {
		return nil, err
	}
	if maxValues <= 0 {
		maxValues = 60 // wrapper default
	}

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

// executeStatsResponse is the relevant slice of the dashboardQueries:execute
// response for a stats query: the top-level "results" column array plus any
// "queryRuntimeErrors" the backend reports in a 200 body.
type executeStatsResponse struct {
	Results            []statsColumnRaw `json:"results"`
	QueryRuntimeErrors []struct {
		ErrorTitle       string `json:"errorTitle"`
		ErrorDescription string `json:"errorDescription"`
		ErrorSeverity    string `json:"errorSeverity"`
	} `json:"queryRuntimeErrors"`
}

// RunStatsQuery runs a stats/aggregation query over [start, end] via the
// dashboardQueries:execute endpoint (POST) and returns the aggregation as typed
// columns + rows — the same execution dashboard charts use.
//
// Unlike GetStats (a GET against :udmSearch, suited to event-field statistics), this
// path accepts the full `match:`/`outcome:` aggregation grammar a dashboard chart
// uses, so it is the way to validate that exact YARA-L from the CLI. clearCache
// forces a read from the database rather than the query cache.
//
// The window is sent as the query input's time_window (absolute start/end). A SEVERE
// queryRuntimeError carried in the 200 body is surfaced as an error; a WARNING (e.g.
// a row-limit notice) is non-fatal and the rows are still returned.
func (c *Client) RunStatsQuery(ctx context.Context, query string, start, end time.Time, clearCache bool) (*StatsResult, error) {
	if err := validateStatsWindow("RunStatsQuery", query, start, end); err != nil {
		return nil, err
	}

	input, err := json.Marshal(map[string]any{
		"timeWindow": map[string]string{
			"startTime": start.UTC().Format(statsTimeFmt),
			"endTime":   end.UTC().Format(statsTimeFmt),
		},
	})
	if err != nil {
		return nil, err
	}

	var cc *bool
	if clearCache {
		cc = &clearCache
	}
	raw, err := c.ExecuteQuery(ctx, query, input, nil, cc)
	if err != nil {
		return nil, err
	}

	var resp executeStatsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("chronicle: RunStatsQuery: decode response: %w", err)
	}
	// A runtime error is carried in a 200 body. Anything not explicitly a WARNING
	// (or unspecified) is treated as fatal — defaulting unknown severities to fatal
	// avoids reporting a real failure as a clean empty result. WARNINGs (e.g. a
	// row-limit truncation) are non-fatal but surfaced so a partial result is never
	// shown as complete.
	var warnings []string
	for _, e := range resp.QueryRuntimeErrors {
		msg := runtimeErrorMessage(e.ErrorTitle, e.ErrorDescription)
		switch strings.ToUpper(strings.TrimSpace(e.ErrorSeverity)) {
		case "WARNING", "ERROR_SEVERITY_UNSPECIFIED", "":
			if msg != "" {
				warnings = append(warnings, msg)
			}
		default:
			return nil, fmt.Errorf("chronicle: RunStatsQuery: %s", msg)
		}
	}
	res := processStats(resp.Results)
	res.Warnings = warnings
	return res, nil
}

// statsTimeFmt formats stats-query window bounds with microsecond precision and a
// literal trailing Z, matching the wrapper and preserving sub-second --from/--to
// boundaries (plain RFC3339 would truncate them to whole seconds).
const statsTimeFmt = "2006-01-02T15:04:05.000000Z"

// validateStatsWindow rejects an empty query or a non-increasing [start, end)
// window, with the calling method's name in the error.
func validateStatsWindow(fn, query string, start, end time.Time) error {
	if query == "" {
		return fmt.Errorf("chronicle: %s requires a non-empty query", fn)
	}
	if !start.Before(end) {
		return fmt.Errorf("chronicle: %s start (%s) must be before end (%s)",
			fn, start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	}
	return nil
}

// runtimeErrorMessage joins a queryRuntimeError's title and description into one
// line, tolerating either being empty.
func runtimeErrorMessage(title, desc string) string {
	switch {
	case title != "" && desc != "":
		return title + ": " + desc
	case desc != "":
		return desc
	default:
		return title
	}
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
