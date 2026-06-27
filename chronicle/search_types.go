package chronicle

import "encoding/json"

// search_types.go holds the small types shared across the UDM search-view, CSV
// export, and streamSearch surfaces (udm_search_view.go, udm_search_csv.go,
// stream_search.go). Per-surface request/response structs live with their method.

// searchTimeRange is the {startTime,endTime} interval the legacy UDM search-view
// and CSV-export request bodies expect: RFC3339 microsecond UTC strings
// (rawLogSearchTimeLayout, "2006-01-02T15:04:05.000000Z"), start inclusive / end
// exclusive — the same wire shape the official wrapper sends.
type searchTimeRange struct {
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

// udmEventList is the UdmEventList payload shared by the search-view response and
// the streamSearch operation response: the matched events plus the streaming
// completion / truncation flags. Each event is kept raw (the UDM []json.RawMessage
// DEVIATION from SearchUDM) so callers unmarshal into their own types.
//
// Streaming semantics: the server may send several progress chunks; once Complete
// is true the events are omitted, and a re-sent list REPLACES (not appends to) the
// prior one — so consumers keep the last events-bearing list rather than
// concatenating across chunks.
type udmEventList struct {
	Events        []json.RawMessage `json:"events"`
	Rows          []json.RawMessage `json:"rows,omitempty"` // ResultRow[] (stats/datatable shape)
	ColumnNames   *columnNames      `json:"columnNames,omitempty"`
	Progress      float64           `json:"progress"`
	TooManyEvents bool              `json:"tooManyEvents"`
	Complete      bool              `json:"complete"`
}

type columnNames struct {
	Names []string `json:"names"`
}
