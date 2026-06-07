package chronicle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Raw log search uses the project ID (string) form in its resource path
// (numeric=false), matching the wrapper, which builds the :searchRawLogs URL
// from the string project_id. See resource.go for why the form is explicit per
// endpoint.

// rawLogSearchTimeLayout is the timestamp format the searchRawLogs body wants:
// RFC3339 with microsecond precision and a literal Z (UTC). The wrapper formats
// with Python's "%Y-%m-%dT%H:%M:%S.%fZ"; Go's RFC3339Nano trims trailing zeros,
// so we use an explicit microsecond layout to match byte-for-byte.
const rawLogSearchTimeLayout = "2006-01-02T15:04:05.000000Z"

// RawLog is one matched raw (unparsed) log entry from a searchRawLogs call.
//
// Data is the freeform per-entry payload as returned by the API (it carries the
// raw log text alongside any server-provided metadata) and is kept as raw JSON
// rather than a fixed struct — raw log shapes vary by log type and the API
// surface here is alpha. LogType/Timestamp are lifted out when present for
// convenient access; both stay populated inside Data as well.
type RawLog struct {
	// LogType is the entry's log type display name (e.g. "OKTA"), when present.
	LogType string `json:"logType,omitempty"`
	// Timestamp is the entry's collection/event time, when present.
	Timestamp string `json:"timestamp,omitempty"`
	// Data is the complete entry object as returned by the API.
	Data json.RawMessage `json:"-"`
}

// UnmarshalJSON keeps the full entry in Data while lifting logType into its typed
// slot. logType is a nested object {"displayName":"…"} on a live match, but was a
// bare string in the older shape — accept either. A live match carries no
// top-level timestamp (the time lives inside the event), so Timestamp is lifted
// only if present.
func (r *RawLog) UnmarshalJSON(b []byte) error {
	r.Data = append(r.Data[:0], b...)
	var lifted struct {
		LogType   json.RawMessage `json:"logType"`
		Timestamp string          `json:"timestamp"`
	}
	// Ignore decode errors for the lifted view: Data is the source of truth and a
	// non-object entry should not fail the whole search.
	_ = json.Unmarshal(b, &lifted)
	r.Timestamp = lifted.Timestamp
	r.LogType = ""
	if len(lifted.LogType) > 0 {
		var s string
		var obj struct {
			DisplayName string `json:"displayName"`
		}
		if json.Unmarshal(lifted.LogType, &s) == nil {
			r.LogType = s
		} else if json.Unmarshal(lifted.LogType, &obj) == nil {
			r.LogType = obj.DisplayName
		}
	}
	return nil
}

// MarshalJSON emits the original entry payload so a RawLog round-trips verbatim.
func (r RawLog) MarshalJSON() ([]byte, error) {
	if len(r.Data) == 0 {
		return []byte("null"), nil
	}
	return r.Data, nil
}

// rawLogTimeRange is the baselineTimeRange sub-object of a searchRawLogs body.
type rawLogTimeRange struct {
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

// rawLogType is a logTypes[] element; the API filters by display name.
type rawLogType struct {
	DisplayName string `json:"displayName"`
}

// rawLogSearchRequest is the :searchRawLogs request body.
type rawLogSearchRequest struct {
	BaselineQuery           string          `json:"baselineQuery"`
	BaselineTimeRange       rawLogTimeRange `json:"baselineTimeRange"`
	CaseSensitive           bool            `json:"caseSensitive"`
	SnapshotQuery           string          `json:"snapshotQuery,omitempty"`
	MaxAggregationsPerField int             `json:"maxAggregationsPerField,omitempty"`
	PageSize                int             `json:"pageSize,omitempty"`
	PageToken               string          `json:"pageToken,omitempty"`
	LogTypes                []rawLogType    `json:"logTypes,omitempty"`
}

// rawLogSearchResponse is the merged result of one :searchRawLogs POST — a plain
// carrier (no longer decoded from JSON directly; see decodeRawLogResponse).
type rawLogSearchResponse struct {
	RawLogs       []RawLog
	Aggregations  json.RawMessage
	NextPageToken string
}

// rawLogChunk is one element of the server-streamed :searchRawLogs response. The
// HTTP body is a JSON ARRAY of these chunks; matches live under "matches" (the
// older shape used "rawLogs"), and aggregations are freeform.
type rawLogChunk struct {
	Matches       []RawLog        `json:"matches"`
	RawLogs       []RawLog        `json:"rawLogs"` // older key, if present
	Aggregations  json.RawMessage `json:"aggregations"`
	NextPageToken string          `json:"nextPageToken"`
}

func (ch rawLogChunk) entries() []RawLog {
	if len(ch.Matches) > 0 {
		return ch.Matches
	}
	return ch.RawLogs
}

// decodeRawLogResponse handles both the live server-streamed JSON array of chunks
// and the older single-object shape, returning the merged matches plus the last
// non-empty aggregations and next-page token.
func decodeRawLogResponse(raw json.RawMessage) (entries []RawLog, agg json.RawMessage, next string, err error) {
	var chunks []rawLogChunk
	if t := bytes.TrimSpace(raw); len(t) > 0 && t[0] == '[' {
		if e := json.Unmarshal(raw, &chunks); e != nil {
			return nil, nil, "", fmt.Errorf("chronicle: decode raw-log search: %w", e)
		}
	} else {
		var one rawLogChunk
		if e := json.Unmarshal(raw, &one); e != nil {
			return nil, nil, "", fmt.Errorf("chronicle: decode raw-log search: %w", e)
		}
		chunks = []rawLogChunk{one}
	}
	for _, ch := range chunks {
		entries = append(entries, ch.entries()...)
		if len(ch.Aggregations) > 0 {
			agg = ch.Aggregations
		}
		if ch.NextPageToken != "" {
			next = ch.NextPageToken
		}
	}
	return entries, agg, next, nil
}

// SearchRawLogs searches raw (unparsed) logs over [start, end) and returns all
// matched entries, optionally restricted to specific log types.
//
// query is a raw-log search expression. logTypes, when non-empty, limits the
// search to those log-type display names (e.g. []string{"OKTA"}). pageSize caps
// the entries fetched per page (0 lets the server choose); all pages are
// accumulated.
//
// Endpoint: POST {instance}:searchRawLogs with a baselineQuery /
// baselineTimeRange body (project ID form). start is inclusive, end exclusive,
// matching the API; timestamps are sent in UTC with microsecond precision.
//
// DEVIATION: the official Python wrapper issues a single POST and returns the
// raw response dict, ignoring nextPageToken — so its results silently truncate
// at one page. We round-trip pageToken and accumulate across pages (capped at
// 50 via the shared paginator) so large result sets come back whole. We also
// return typed []RawLog (full entry preserved as raw JSON) instead of an
// untyped map. Use SearchRawLogsPage for a single page plus aggregations.
func (c *Client) SearchRawLogs(ctx context.Context, query string, logTypes []string, start, end time.Time, pageSize int) ([]RawLog, error) {
	if query == "" {
		return nil, fmt.Errorf("chronicle: SearchRawLogs requires a non-empty query")
	}
	if !start.Before(end) {
		return nil, fmt.Errorf("chronicle: SearchRawLogs start (%s) must be before end (%s)",
			start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	}

	var all []RawLog
	err := paginate(50, func(token string) (string, error) {
		resp, err := c.searchRawLogsPage(ctx, query, logTypes, start, end, pageSize, token)
		if err != nil {
			return "", err
		}
		all = append(all, resp.RawLogs...)
		return resp.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// RawLogSearchPage is a single page of raw-log results, including the freeform
// aggregations blob and the token for the next page (empty when exhausted).
type RawLogSearchPage struct {
	RawLogs       []RawLog
	Aggregations  json.RawMessage
	NextPageToken string
}

// SearchRawLogsPage fetches a single page of raw-log results, exposing the
// per-field aggregations and the next-page token that SearchRawLogs hides.
//
// maxAggregationsPerField caps the distinct values returned per UDM field (0
// lets the server choose); caseSensitive toggles case-sensitive matching;
// snapshotQuery, when set, post-filters the page. Pass pageToken="" for the
// first page and the returned NextPageToken to continue.
//
// DEVIATION: surfaced as a distinct method (not in the wrapper) so callers who
// want aggregations or manual paging are not forced through the accumulate-all
// SearchRawLogs path.
func (c *Client) SearchRawLogsPage(ctx context.Context, query string, logTypes []string, start, end time.Time,
	pageSize, maxAggregationsPerField int, caseSensitive bool, snapshotQuery, pageToken string) (*RawLogSearchPage, error) {
	if query == "" {
		return nil, fmt.Errorf("chronicle: SearchRawLogsPage requires a non-empty query")
	}
	if !start.Before(end) {
		return nil, fmt.Errorf("chronicle: SearchRawLogsPage start (%s) must be before end (%s)",
			start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	}

	body := rawLogSearchRequest{
		BaselineQuery: query,
		BaselineTimeRange: rawLogTimeRange{
			StartTime: start.UTC().Format(rawLogSearchTimeLayout),
			EndTime:   end.UTC().Format(rawLogSearchTimeLayout),
		},
		CaseSensitive:           caseSensitive,
		SnapshotQuery:           snapshotQuery,
		MaxAggregationsPerField: maxAggregationsPerField,
		PageSize:                pageSize,
		PageToken:               pageToken,
	}
	for _, lt := range logTypes {
		if lt == "" {
			continue
		}
		body.LogTypes = append(body.LogTypes, rawLogType{DisplayName: lt})
	}

	// RPC-style method: ":searchRawLogs" is appended directly to the instance
	// path with no separating slash.
	path := c.instancePath(false) + ":searchRawLogs"

	var raw json.RawMessage
	if err := c.post(ctx, path, body, &raw); err != nil {
		return nil, err
	}
	entries, agg, next, err := decodeRawLogResponse(raw)
	if err != nil {
		return nil, err
	}
	return &RawLogSearchPage{RawLogs: entries, Aggregations: agg, NextPageToken: next}, nil
}

// searchRawLogsPage is the internal one-page fetch used by the accumulating
// SearchRawLogs (defaults: case-insensitive, no snapshot/aggregation tuning).
func (c *Client) searchRawLogsPage(ctx context.Context, query string, logTypes []string, start, end time.Time, pageSize int, pageToken string) (*rawLogSearchResponse, error) {
	body := rawLogSearchRequest{
		BaselineQuery: query,
		BaselineTimeRange: rawLogTimeRange{
			StartTime: start.UTC().Format(rawLogSearchTimeLayout),
			EndTime:   end.UTC().Format(rawLogSearchTimeLayout),
		},
		PageSize:  pageSize,
		PageToken: pageToken,
	}
	for _, lt := range logTypes {
		if lt == "" {
			continue
		}
		body.LogTypes = append(body.LogTypes, rawLogType{DisplayName: lt})
	}

	path := c.instancePath(false) + ":searchRawLogs"
	var raw json.RawMessage
	if err := c.post(ctx, path, body, &raw); err != nil {
		return nil, err
	}
	entries, agg, next, err := decodeRawLogResponse(raw)
	if err != nil {
		return nil, err
	}
	return &rawLogSearchResponse{RawLogs: entries, Aggregations: agg, NextPageToken: next}, nil
}
