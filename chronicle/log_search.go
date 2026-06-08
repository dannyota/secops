package chronicle

import (
	"bytes"
	"context"
	"encoding/base64"
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
	// ID is the raw-log id (the top-level "id"), the handle FindRawLogsByIDs takes
	// to download the FULL log bytes (the in-match snippet is truncated to 80 chars).
	ID string `json:"id,omitempty"`
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
		ID        string          `json:"id"`
	}
	// Ignore decode errors for the lifted view: Data is the source of truth and a
	// non-object entry should not fail the whole search.
	_ = json.Unmarshal(b, &lifted)
	r.Timestamp = lifted.Timestamp
	r.ID = lifted.ID
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

// rawLogChunk is one element of the :searchRawLogs response. The documented
// v1alpha body is a single object {matches, progress, timeline, aggregations,
// nextPageToken}; in practice the host streams a JSON ARRAY of these objects.
// decodeRawLogResponse handles both. Matches live under "matches" (documented
// key; an older shape used "rawLogs"); aggregations are kept freeform.
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
// raw response dict as an untyped map. We return typed []RawLog (full entry
// preserved as raw JSON) and accumulate any pages the server reports (capped at
// 50 via the shared paginator). Note the documented request body has no
// pageToken and documents nextPageToken only as a "more matches available"
// indicator, so paging is best-effort: the streamed response normally delivers
// every match in one POST. Use SearchRawLogsPage for a single page plus
// aggregations.
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
	pageSize, maxAggregationsPerField int, caseSensitive bool, snapshotQuery, pageToken string,
) (*RawLogSearchPage, error) {
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

// RawLogLine is one full, untruncated raw (unparsed) log line — the decoded
// logBytes plus its provenance. This is what a parser-development workflow needs:
// the exact bytes the platform ingested, ready to feed `parsers run --logs`.
type RawLogLine struct {
	Text          string `json:"text"`                    // the complete raw log line (logBytes, base64-decoded)
	LogType       string `json:"logType,omitempty"`       // log-type token, when present
	SourceProduct string `json:"sourceProduct,omitempty"` // source product, when present
	Timestamp     string `json:"timestamp,omitempty"`     // ingestion/collection time, when present
}

// findRawLogsResponse is the legacyFindRawLogs (by ids) response: per-id groups,
// each carrying the full RawLog entries. Only the fields needed for a raw line are
// modeled; logBytes is a base64-encoded ("bytes format") field.
type findRawLogsResponse struct {
	RawLogs []struct {
		RawLogs []struct {
			LogBytes      string `json:"logBytes"`
			SourceProduct string `json:"sourceProduct"`
			Timestamp     string `json:"timestamp"`
			Type          string `json:"type"`
		} `json:"rawLogs"`
	} `json:"rawLogs"`
}

// findRawLogIDBatch bounds how many raw-log ids go in one legacyFindRawLogs GET, so
// the request URL stays well under any length limit (ids are ~44-char base64).
const findRawLogIDBatch = 25

// FindRawLogLines downloads the FULL raw log lines for the given raw-log ids
// (legacyFindRawLogs) and decodes them: logBytes is base64-decoded to text (falling
// back to the verbatim string if it is not base64). The ids are fetched in batches
// so a large request can't blow the URL length. Read-only.
func (c *Client) FindRawLogLines(ctx context.Context, ids []string) ([]RawLogLine, error) {
	var out []RawLogLine
	for start := 0; start < len(ids); start += findRawLogIDBatch {
		end := min(start+findRawLogIDBatch, len(ids))
		raw, err := c.FindRawLogsByIDs(ctx, ids[start:end])
		if err != nil {
			return nil, err
		}
		var resp findRawLogsResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("chronicle: decode raw logs: %w", err)
		}
		for _, g := range resp.RawLogs {
			for _, l := range g.RawLogs {
				text := l.LogBytes
				if dec, derr := base64.StdEncoding.DecodeString(l.LogBytes); derr == nil {
					text = string(dec)
				}
				out = append(out, RawLogLine{
					Text:          text,
					LogType:       l.Type,
					SourceProduct: l.SourceProduct,
					Timestamp:     l.Timestamp,
				})
			}
		}
	}
	return out, nil
}

// RawLogIDsFromUDMEvents lifts each event's raw-log id (udm.metadata.id) — the
// handle FindRawLogLines / legacyFindRawLogs takes to download the full raw bytes.
// Events with no metadata.id are skipped. The events are the raw element shape
// :udmSearch returns ({"name":…,"udm":{"metadata":{…}}}).
func RawLogIDsFromUDMEvents(events []json.RawMessage) []string {
	ids := make([]string, 0, len(events))
	for _, e := range events {
		var d struct {
			UDM struct {
				Metadata struct {
					ID string `json:"id"`
				} `json:"metadata"`
			} `json:"udm"`
		}
		if json.Unmarshal(e, &d) == nil && d.UDM.Metadata.ID != "" {
			ids = append(ids, d.UDM.Metadata.ID)
		}
	}
	return ids
}

// FetchRawLogLines returns up to limit recent FULL raw log lines matching a UDM
// search query — the bytes a parser developer needs. It runs :udmSearch (which
// accepts the `metadata.log_type = "…"` / `metadata.event_type = "…"` predicates
// that the raw-log-search filter does NOT), takes each event's raw-log id
// (udm.metadata.id), and downloads the COMPLETE bytes via legacyFindRawLogs
// (base64-decoding logBytes to text).
//
// This is the path the console uses: a log type whose parser is missing/broken
// normalizes to GENERIC_EVENT (still parsed=true), so a raw-log `parsed = false`
// filter misses it — but a UDM search on metadata.log_type finds it.
//
// udmQuery is a UDM search expression (e.g. `metadata.log_type = "KONG_GATEWAY"`,
// optionally `… AND metadata.event_type = "GENERIC_EVENT"`). start is inclusive,
// end exclusive.
func (c *Client) FetchRawLogLines(ctx context.Context, udmQuery string, start, end time.Time, limit int) ([]RawLogLine, error) {
	if limit <= 0 {
		limit = 100
	}
	events, _, err := c.SearchUDMPage(ctx, udmQuery, start, end, limit)
	if err != nil {
		return nil, err
	}
	ids := RawLogIDsFromUDMEvents(events)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return c.FindRawLogLines(ctx, ids)
}
