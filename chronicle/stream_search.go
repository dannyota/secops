package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// stream_search.go pages results of an already-created async UDM-search operation
// (the operation id from FetchUDMSearchView with ReturnOperationIDOnly), and lists
// recent/running search operations. Chronicle host, v1alpha only (the :streamSearch
// verb does not exist on v1/v1beta).
//
// NOTE: the :streamSearch response body could not be captured offline (gzipped
// stream); field names below are taken from the v1alpha docs + the search-view
// response shape. The index inclusivity (eventIndexStart/End) and per-event
// element shape are confirmed by a small live probe before relying on them.

// ResultsType selects which entity results a search returns (entity-graph queries).
type ResultsType string

const (
	ResultsTypeUnspecified ResultsType = "RESULTS_TYPE_UNSPECIFIED"
	ResultsTypeTimed       ResultsType = "TIMED"
	ResultsTypeTimeless    ResultsType = "TIMELESS"
)

// OperationStatus is google.rpc.Status — a failed operation's error.
type OperationStatus struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Details []json.RawMessage `json:"details,omitempty"`
}

// Error renders an OperationStatus as an error string.
func (s *OperationStatus) Error() string {
	return fmt.Sprintf("operation failed (code %d): %s", s.Code, s.Message)
}

// StreamSearchPage is one window of an async search operation's results.
type StreamSearchPage struct {
	OperationName        string            // operation.name
	Done                 bool              // operation.done (LRO finished)
	Complete             bool              // response.complete (result streaming finished)
	Progress             float64           // response.progress, 0..1
	BaselineEventsCount  int               // total events matching the baseline query
	FilteredEventsCount  int               // events matching the snapshot query (<= baseline)
	AvailableResultCount int               // rows materialized pre-pagination
	TooManyEvents        bool              // result set truncated server-side
	Events               []json.RawMessage // response.events.events[] — each a raw UdmEventInfo
}

type streamSearchEnvelope struct {
	Operation streamOperation `json:"operation"`
}

type streamOperation struct {
	Name     string              `json:"name"`
	Metadata json.RawMessage     `json:"metadata"`
	Done     bool                `json:"done"`
	Error    *OperationStatus    `json:"error,omitempty"`
	Response *streamViewResponse `json:"response,omitempty"`
}

// streamViewResponse is operation.response (a LegacyFetchUdmSearchViewResponse).
type streamViewResponse struct {
	Type                  string            `json:"@type,omitempty"`
	Progress              float64           `json:"progress"`
	Complete              bool              `json:"complete"`
	TooManyEvents         bool              `json:"tooManyEvents"`
	TooLargeResponse      bool              `json:"tooLargeResponse"`
	BaselineEventsCount   int               `json:"baselineEventsCount"`
	FilteredEventsCount   int               `json:"filteredEventsCount"`
	AvailableResultCount  int               `json:"availableResultCount"`
	QueryValidationErrors []json.RawMessage `json:"queryValidationErrors"`
	RuntimeErrors         []json.RawMessage `json:"runtimeErrors"`
	Events                *udmEventList     `json:"events"`
}

// StreamSearch fetches the window [startIdx, endIdx] of an async UDM-search
// operation's results. operationID accepts a bare "s-udm-<uuid>", an
// "operations/s-udm-<uuid>", or a full resource name. Pass startIdx=0 to request
// the entire result set (the server treats 0 as "unset").
//
// Endpoint: GET {instance}/operations/{op}:streamSearch?eventIndexStart&eventIndexEnd
// &paginationEnabled=true&pageRequest=false (chronicle host, v1alpha; project ID form).
func (c *Client) StreamSearch(ctx context.Context, operationID string, startIdx, endIdx int) (*StreamSearchPage, error) {
	op := normalizeOperationID(operationID)
	if op == "" {
		return nil, fmt.Errorf("chronicle: StreamSearch requires an operation id")
	}
	q := url.Values{
		"paginationEnabled": {"true"},
		"pageRequest":       {"false"},
		"eventIndexEnd":     {fmt.Sprintf("%d", endIdx)},
	}
	if startIdx > 0 {
		q.Set("eventIndexStart", fmt.Sprintf("%d", startIdx))
	}
	path := c.instancePath(false) + "/operations/" + op + ":streamSearch"

	var raw json.RawMessage
	if err := c.get(ctx, path, &raw, withQuery(q)); err != nil {
		return nil, err
	}
	// Single {"operation":{…}} object normally; decode defensively as a stream and
	// keep the last envelope (latest streamed state replaces earlier ones).
	envs, err := decodeStreamChunks[streamSearchEnvelope](raw)
	if err != nil {
		return nil, fmt.Errorf("chronicle: decode streamSearch: %w", err)
	}
	if len(envs) == 0 {
		return &StreamSearchPage{}, nil
	}
	opn := envs[len(envs)-1].Operation
	if opn.Error != nil {
		return nil, opn.Error
	}
	page := &StreamSearchPage{OperationName: opn.Name, Done: opn.Done}
	if r := opn.Response; r != nil {
		if len(r.QueryValidationErrors) > 0 {
			return nil, fmt.Errorf("chronicle: streamSearch query invalid: %s", string(r.QueryValidationErrors[0]))
		}
		if len(r.RuntimeErrors) > 0 {
			return nil, fmt.Errorf("chronicle: streamSearch runtime error: %s", string(r.RuntimeErrors[0]))
		}
		page.Complete = r.Complete
		page.Progress = r.Progress
		page.BaselineEventsCount = r.BaselineEventsCount
		page.FilteredEventsCount = r.FilteredEventsCount
		page.AvailableResultCount = r.AvailableResultCount
		page.TooManyEvents = r.TooManyEvents
		if r.Events != nil {
			page.Events = r.Events.Events
		}
	}
	return page, nil
}

// SearchOperation is one search longrunning operation. Metadata and Response are
// @type-tagged Any payloads, kept raw (the two op families carry different types).
type SearchOperation struct {
	Name     string           `json:"name"`
	Metadata json.RawMessage  `json:"metadata"`
	Done     bool             `json:"done"`
	Error    *OperationStatus `json:"error,omitempty"`
	Response json.RawMessage  `json:"response,omitempty"`
}

type listOperationsResponse struct {
	Operations    []SearchOperation `json:"operations"`
	NextPageToken string            `json:"nextPageToken"`
}

// ListSearchOperations lists recent/running search operations (filter
// name:"operations/s-"), accumulating all pages.
//
// Endpoint: GET {instance}/operations?filter=name:"operations/s-"&pageSize=200.
func (c *Client) ListSearchOperations(ctx context.Context) ([]SearchOperation, error) {
	var all []SearchOperation
	err := paginate(50, func(pageToken string) (string, error) {
		q := url.Values{
			"filter":   {`name:"operations/s-"`},
			"pageSize": {"200"},
		}
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		var resp listOperationsResponse
		if err := c.get(ctx, c.instancePath(false)+"/operations", &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.Operations...)
		return resp.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// normalizeOperationID strips any "operations/" prefix or full-resource-name
// prefix, returning the bare operation id (e.g. "s-udm-<uuid>").
func normalizeOperationID(id string) string {
	id = strings.TrimSpace(id)
	if i := strings.LastIndex(id, "/operations/"); i >= 0 {
		return id[i+len("/operations/"):]
	}
	return strings.TrimPrefix(id, "operations/")
}
