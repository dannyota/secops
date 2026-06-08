package chronicle

import (
	"context"
	"encoding/base64"
	"net/url"
	"strconv"
)

// Log is one telemetry log entry from logTypes/{logType}/logs.list — the direct,
// search-free way to sample a log type's RAW (ingested) logs for parser
// development. Data is the base64-encoded raw bytes.
//
// NOTE: logs.list returns logs ORDERED BY RESOURCE NAME, not by time, and this
// fetches only the first page — so it is a SAMPLE of the log type's logs, not
// necessarily the most recent. Pass a collectionTime filter to bound by time.
type Log struct {
	Name           string `json:"name"`           // …/logTypes/{logType}/logs/{log}
	Data           string `json:"data"`           // base64-encoded raw log bytes
	LogEntryTime   string `json:"logEntryTime"`   // RFC3339, when the log was emitted
	CollectionTime string `json:"collectionTime"` // RFC3339, when SecOps collected it
}

// ListLogs lists a page of a log type's raw logs (GET logTypes/{logType}/logs).
// Unlike the raw-log search, this is a plain list — no query needed. pageSize
// bounds the page; filter is an AIP-160 filter on collectionTime only (e.g.
// `collectionTime.seconds >= 123 AND collectionTime.seconds <= 456`). Logs are
// ordered by resource name (not time) and only the first page is returned, so the
// result is a SAMPLE; use filter to bound by time. Each Log.Data is base64 bytes.
//
// DEVIATION: parsers/logs use the project NUMBER form (numeric=true), matching
// ListParsers and the console request.
func (c *Client) ListLogs(ctx context.Context, logType string, pageSize int, filter string) ([]Log, error) {
	sub := "logTypes/" + url.PathEscape(logType) + "/logs"
	q := url.Values{}
	if pageSize > 0 {
		q.Set("pageSize", strconv.Itoa(pageSize))
	}
	if filter != "" {
		q.Set("filter", filter)
	}
	var resp struct {
		Logs          []Log  `json:"logs"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := c.get(ctx, c.resourcePath(sub, true), &resp, withQuery(q)); err != nil {
		return nil, err
	}
	return resp.Logs, nil
}

// FetchSampleLogLines lists up to limit recent raw logs for a log type and decodes
// each to a full RawLogLine (base64 Data → text), ready to feed `parsers run
// --logs`. This is the simplest raw-log path: a direct list, no search.
func (c *Client) FetchSampleLogLines(ctx context.Context, logType string, limit int, filter string) ([]RawLogLine, error) {
	logs, err := c.ListLogs(ctx, logType, limit, filter)
	if err != nil {
		return nil, err
	}
	out := make([]RawLogLine, 0, len(logs))
	for _, l := range logs {
		text := l.Data
		if dec, derr := base64.StdEncoding.DecodeString(l.Data); derr == nil {
			text = string(dec)
		}
		out = append(out, RawLogLine{
			Text:      text,
			LogType:   logType,
			Timestamp: l.LogEntryTime,
		})
	}
	return out, nil
}
