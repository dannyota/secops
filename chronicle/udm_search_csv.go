package chronicle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CSVQueryType is the legacyFetchUdmSearchCsv request's queryType enum. Note these
// tokens (UDM_QUERY / RAW_LOG_QUERY) differ from the SearchQuery QUERY_TYPE_* set.
type CSVQueryType string

const (
	CSVQueryTypeUnknown CSVQueryType = "UNKNOWN"
	CSVQueryTypeUDM     CSVQueryType = "UDM_QUERY"
	CSVQueryTypeRawLog  CSVQueryType = "RAW_LOG_QUERY"
)

// csvExportRequest is the legacy:legacyFetchUdmSearchCsv body. caseInsensitive
// carries no omitempty — a deliberate false must stay on the wire.
type csvExportRequest struct {
	BaselineQuery     string          `json:"baselineQuery"`
	BaselineTimeRange searchTimeRange `json:"baselineTimeRange"`
	Fields            csvFields       `json:"fields"`
	CaseInsensitive   bool            `json:"caseInsensitive"`
	QueryType         CSVQueryType    `json:"queryType,omitempty"`
}

// csvFields is the doubly-nested "fields":{"fields":[...]} column list.
type csvFields struct {
	Fields []string `json:"fields"`
}

// csvExportChunk is one streamed progress chunk of LegacyFetchUdmSearchCsvResponse.
// The CSV lives at csv.row — each element is one comma-joined line (header first).
type csvExportChunk struct {
	Progress                   float64              `json:"progress"`
	TooManyEvents              bool                 `json:"tooManyEvents"`
	Complete                   bool                 `json:"complete"`
	QueryValidationErrors      []json.RawMessage    `json:"queryValidationErrors"`
	RuntimeErrors              []json.RawMessage    `json:"runtimeErrors"`
	Csv                        *csvEntries          `json:"csv"`
	FailureCsvFieldValidations []csvFieldValidation `json:"failureCsvFieldValidations"`
}

type csvEntries struct {
	Row        []string `json:"row"`
	Timestamps []string `json:"timestamps"`
}

type csvFieldValidation struct {
	Field string `json:"field"`
}

// CSVExportResult is the assembled CSV export plus the stream metadata: completion,
// truncation, the per-row event timestamps, and any server-rejected fields.
type CSVExportResult struct {
	CSV           string   // header + data rows joined with "\n"
	Rows          []string // every csv.row across all chunks, in order (Rows[0] = header)
	Timestamps    []string // parallel csv.timestamps (event time per data row), may be empty
	Complete      bool     // a chunk reported complete=true
	TooManyEvents bool     // any chunk reported tooManyEvents=true (result truncated)
	InvalidFields []string // failureCsvFieldValidations[].field — fields the server rejected
}

// ExportUDMSearchCSV runs a UDM search over [start, end) and returns ALL matching
// rows projected onto the given column fields as a single CSV string (header plus
// one line per event). fields are the UI column labels the server maps to UDM
// fields (e.g. "timestamp", "user", "hostname", "process name", or any
// "udm.additional.*" path); at least one is required.
//
// Unlike SearchUDM (capped, point-in-time), this is the server-side export path:
// it streams the complete result set (the console caps it near 1,000,000 events;
// a clipped result sets TooManyEvents — see ExportUDMSearchCSVResult).
//
// Endpoint: POST {instance}/legacy:legacyFetchUdmSearchCsv (chronicle host,
// v1alpha; project ID form).
func (c *Client) ExportUDMSearchCSV(ctx context.Context, query string, start, end time.Time, fields []string, caseInsensitive bool) (string, error) {
	res, err := c.ExportUDMSearchCSVResult(ctx, query, start, end, fields, caseInsensitive)
	if err != nil {
		return "", err
	}
	return res.CSV, nil
}

// ExportUDMSearchCSVResult is ExportUDMSearchCSV with the full stream metadata.
func (c *Client) ExportUDMSearchCSVResult(ctx context.Context, query string, start, end time.Time, fields []string, caseInsensitive bool) (*CSVExportResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("chronicle: ExportUDMSearchCSV requires a non-empty query")
	}
	if !start.Before(end) {
		return nil, fmt.Errorf("chronicle: ExportUDMSearchCSV start (%s) must be before end (%s)",
			start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("chronicle: ExportUDMSearchCSV requires at least one field (column)")
	}

	body := csvExportRequest{
		BaselineQuery: query,
		BaselineTimeRange: searchTimeRange{
			StartTime: start.UTC().Format(rawLogSearchTimeLayout),
			EndTime:   end.UTC().Format(rawLogSearchTimeLayout),
		},
		Fields:          csvFields{Fields: fields},
		CaseInsensitive: caseInsensitive,
		QueryType:       CSVQueryTypeUDM,
	}

	var raw json.RawMessage
	if err := c.post(ctx, c.resourcePath("legacy:legacyFetchUdmSearchCsv", false), body, &raw); err != nil {
		return nil, err
	}
	chunks, err := decodeStreamChunks[csvExportChunk](raw)
	if err != nil {
		return nil, fmt.Errorf("chronicle: decode CSV export: %w", err)
	}

	res := &CSVExportResult{}
	for _, ch := range chunks {
		if len(ch.QueryValidationErrors) > 0 {
			return nil, fmt.Errorf("chronicle: CSV export query invalid: %s", string(ch.QueryValidationErrors[0]))
		}
		if len(ch.RuntimeErrors) > 0 {
			return nil, fmt.Errorf("chronicle: CSV export runtime error: %s", string(ch.RuntimeErrors[0]))
		}
		if ch.Csv != nil {
			res.Rows = append(res.Rows, ch.Csv.Row...)
			res.Timestamps = append(res.Timestamps, ch.Csv.Timestamps...)
		}
		res.Complete = res.Complete || ch.Complete
		res.TooManyEvents = res.TooManyEvents || ch.TooManyEvents
		for _, f := range ch.FailureCsvFieldValidations {
			res.InvalidFields = append(res.InvalidFields, f.Field)
		}
	}
	res.CSV = strings.Join(res.Rows, "\n")
	return res, nil
}

// decodeStreamChunks decodes a legacy streamed response — a JSON array of progress
// chunks, or (defensively) a single chunk object — into a slice of T. Mirrors
// decodeRawLogResponse's object-or-array tolerance.
func decodeStreamChunks[T any](raw json.RawMessage) ([]T, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var chunks []T
		if err := json.Unmarshal(trimmed, &chunks); err != nil {
			return nil, err
		}
		return chunks, nil
	}
	var one T
	if err := json.Unmarshal(trimmed, &one); err != nil {
		return nil, err
	}
	return []T{one}, nil
}
