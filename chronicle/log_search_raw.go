package chronicle

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

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

// RawLogIDsFromUDMEvents lifts each event's raw-log id (metadata.id) — the
// handle FindRawLogLines / legacyFindRawLogs takes to download the full raw bytes.
// Events with no metadata.id are skipped. Both event envelopes are accepted:
// {"udm":{"metadata":{…}}} from :udmSearch and {"event":{"metadata":{…}}} from
// the search-view engine (UdmEventInfo).
func RawLogIDsFromUDMEvents(events []json.RawMessage) []string {
	type udmMeta struct {
		Metadata struct {
			ID string `json:"id"`
		} `json:"metadata"`
	}
	ids := make([]string, 0, len(events))
	for _, e := range events {
		var d struct {
			UDM   udmMeta `json:"udm"`
			Event udmMeta `json:"event"`
		}
		if json.Unmarshal(e, &d) != nil {
			continue
		}
		switch {
		case d.UDM.Metadata.ID != "":
			ids = append(ids, d.UDM.Metadata.ID)
		case d.Event.Metadata.ID != "":
			ids = append(ids, d.Event.Metadata.ID)
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
