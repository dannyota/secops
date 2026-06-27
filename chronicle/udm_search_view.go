package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// udm_search_view.go is the console's complete-results UDM search engine
// (legacy:legacyFetchUdmSearchView) — distinct from the simpler SearchUDM
// (:udmSearch). It returns the full event set (up to MaxEvents) plus detections,
// field aggregations, prevalence, an optional AI overview, and the headline match
// counts; or, with ReturnOperationIDOnly, just an async operation id to page via
// StreamSearch. Chronicle host, v1alpha, project ID form.

// UDMSearchViewOptions tunes one search-view fetch. The zero value is valid
// (server defaults apply); CaseInsensitive defaults to the console's true when you
// build it, so set it explicitly.
type UDMSearchViewOptions struct {
	MaxEvents                  int    // eventList.maxReturnedEvents; 0 => omit
	MaxValuesPerField          int    // fieldAggregations.maxValuesPerField; 0 => omit
	MaxDetections              int    // detectionOptions.detectionList.maxReturnedDetections; 0 => omit
	DetectionSnapshotQuery     string // detectionOptions.snapshotQuery (alert filter)
	FetchNonAlertingDetections bool   // detectionOptions.fetchNonAlertingDetections
	SnapshotQuery              string // top-level snapshotQuery (post-filter on baseline events)
	CaseInsensitive            bool   // caseInsensitive
	GenerateAIOverview         bool   // generateAiOverview
	Prevalence                 bool   // prevalence.getPrevalence
	PrevalenceBucketSeconds    int    // prevalence.bucketSize.resolutionInSeconds; 0 => omit bucketSize
	ReturnOperationIDOnly      bool   // returnOperationIdOnly => response carries only the operation id
}

// UDMSearchView is the assembled result of a search-view fetch.
type UDMSearchView struct {
	Events       []json.RawMessage     // last events-bearing chunk's events (UdmEventInfo, raw)
	Detections   []json.RawMessage     // last detections-bearing chunk (Collection, raw)
	Aggregations []UDMFieldAggregation // last field-aggregations chunk
	Prevalence   json.RawMessage       // UdmPrevalenceResponse, raw
	AIOverview   string                // aiOverview.aiSummary (Markdown)

	BaselineEventsCount  int // total events matching baselineQuery (the headline count)
	FilteredEventsCount  int // events also matching snapshotQuery (<= baseline)
	AvailableResultCount int // rows actually returned before pagination
	BaselineAlertsCount  int
	FilteredAlertsCount  int

	TooManyEvents    bool
	TooLargeResponse bool
	Complete         bool
	Progress         float64

	OperationID string // operation resource name (only when ReturnOperationIDOnly)
}

// UDMFieldAggregation is one field's value distribution across the result set.
type UDMFieldAggregation struct {
	FieldName          string                  `json:"fieldName"`
	BaselineEventCount int                     `json:"baselineEventCount"`
	EventCount         int                     `json:"eventCount"`
	TooManyValues      bool                    `json:"tooManyValues"`
	ValueCount         int                     `json:"valueCount"`
	AllValues          []UDMValueCount         `json:"allValues"`
	TopValues          []UDMValueCount         `json:"topValues"`
	BottomValues       []UDMValueCount         `json:"bottomValues"`
	AggregationType    UDMFieldAggregationType `json:"aggregationType"`
}

// UDMValueCount is one value of a field plus how many events carry it.
type UDMValueCount struct {
	Value              UDMFieldValue `json:"value"`
	BaselineEventCount int           `json:"baselineEventCount"`
	EventCount         int           `json:"eventCount"`
}

// UDMFieldValue is a protojson union — exactly one field is set. int64/uint64 come
// over the wire as strings (protojson), hence *string.
type UDMFieldValue struct {
	StringValue    *string  `json:"stringValue,omitempty"`
	Int32Value     *int32   `json:"int32Value,omitempty"`
	Uint32Value    *uint32  `json:"uint32Value,omitempty"`
	Int64Value     *string  `json:"int64Value,omitempty"`
	Uint64Value    *string  `json:"uint64Value,omitempty"`
	FloatValue     *float64 `json:"floatValue,omitempty"`
	DoubleValue    *float64 `json:"doubleValue,omitempty"`
	EnumValue      *string  `json:"enumValue,omitempty"`
	BoolValue      *bool    `json:"boolValue,omitempty"`
	BytesValue     *string  `json:"bytesValue,omitempty"`
	IsNull         *bool    `json:"isNull,omitempty"`
	TimestampValue *string  `json:"timestampValue,omitempty"`
}

// UDMFieldAggregationType is the aggregationType enum (string tokens, v1alpha).
type UDMFieldAggregationType string

const (
	UDMAggTypeUnspecified UDMFieldAggregationType = "UNSPECIFIED_FIELD_AGGREGATION_TYPE"
	UDMAggTypeUDM         UDMFieldAggregationType = "UDM_FIELD_AGGREGATION_TYPE"
	UDMAggTypeEntity      UDMFieldAggregationType = "ENTITY_FIELD_AGGREGATION_TYPE"
	UDMAggTypeDataTable   UDMFieldAggregationType = "DATA_TABLE_FIELD_AGGREGATION_TYPE"
	UDMAggTypeJoins       UDMFieldAggregationType = "JOINS_FIELD_AGGREGATION_TYPE"
	UDMAggTypeDetection   UDMFieldAggregationType = "DETECTION_FIELD_AGGREGATION_TYPE"
	UDMAggTypeCase        UDMFieldAggregationType = "CASE_FIELD_AGGREGATION_TYPE"
	UDMAggTypeCaseHistory UDMFieldAggregationType = "CASE_HISTORY_FIELD_AGGREGATION_TYPE"
)

// --- request body ----------------------------------------------------------

type udmSearchViewRequest struct {
	BaselineQuery         string                 `json:"baselineQuery"`
	BaselineTimeRange     searchTimeRange        `json:"baselineTimeRange"`
	SnapshotQuery         string                 `json:"snapshotQuery,omitempty"`
	EventList             *udmViewEventListOpts  `json:"eventList,omitempty"`
	FieldAggregations     *udmViewAggOpts        `json:"fieldAggregations,omitempty"`
	DetectionOptions      *udmViewDetectionOpts  `json:"detectionOptions,omitempty"`
	Prevalence            *udmViewPrevalenceOpts `json:"prevalence,omitempty"`
	CaseInsensitive       bool                   `json:"caseInsensitive"`
	GenerateAIOverview    bool                   `json:"generateAiOverview,omitempty"`
	ReturnOperationIDOnly bool                   `json:"returnOperationIdOnly,omitempty"`
}

type udmViewEventListOpts struct {
	MaxReturnedEvents int `json:"maxReturnedEvents,omitempty"`
}
type udmViewAggOpts struct {
	MaxValuesPerField int `json:"maxValuesPerField,omitempty"`
}
type udmViewDetectionOpts struct {
	SnapshotQuery              string                `json:"snapshotQuery,omitempty"`
	DetectionList              *udmViewDetectionList `json:"detectionList,omitempty"`
	FieldAggregations          *udmViewAggOpts       `json:"fieldAggregations,omitempty"`
	FetchNonAlertingDetections bool                  `json:"fetchNonAlertingDetections,omitempty"`
}
type udmViewDetectionList struct {
	MaxReturnedDetections int `json:"maxReturnedDetections,omitempty"`
}
type udmViewPrevalenceOpts struct {
	GetPrevalence bool               `json:"getPrevalence,omitempty"`
	BucketSize    *udmViewBucketSize `json:"bucketSize,omitempty"`
}
type udmViewBucketSize struct {
	ResolutionInSeconds int `json:"resolutionInSeconds,omitempty"`
}

// --- response chunk --------------------------------------------------------

type udmSearchViewChunk struct {
	Operation             string                `json:"operation"`
	Progress              float64               `json:"progress"`
	TooManyEvents         bool                  `json:"tooManyEvents"`
	TooLargeResponse      bool                  `json:"tooLargeResponse"`
	Complete              bool                  `json:"complete"`
	BaselineEventsCount   int                   `json:"baselineEventsCount"`
	AvailableResultCount  int                   `json:"availableResultCount"`
	FilteredEventsCount   int                   `json:"filteredEventsCount"`
	QueryValidationErrors []json.RawMessage     `json:"queryValidationErrors"`
	RuntimeErrors         []json.RawMessage     `json:"runtimeErrors"`
	Events                *udmEventList         `json:"events"`
	FieldAggregations     *udmFieldAggregations `json:"fieldAggregations"`
	Detections            *udmViewDetections    `json:"detections"`
	Prevalence            json.RawMessage       `json:"prevalence"`
	AIOverview            *udmViewAIOverview    `json:"aiOverview"`
}

type udmFieldAggregations struct {
	Fields   []UDMFieldAggregation `json:"fields"`
	Complete bool                  `json:"complete"`
}

type udmViewDetections struct {
	Detections          []json.RawMessage `json:"detections"`
	Complete            bool              `json:"complete"`
	BaselineAlertsCount int               `json:"baselineAlertsCount"`
	FilteredAlertsCount int               `json:"filteredAlertsCount"`
}

type udmViewAIOverview struct {
	AISummary string `json:"aiSummary"`
	Complete  bool   `json:"complete"`
}

// FetchUDMSearchView runs the full console search view over [start, end). With
// opts.ReturnOperationIDOnly it returns only an async operation id (drive results
// via StreamSearch); otherwise it returns the complete event set plus detections,
// aggregations, prevalence, AI overview, and match counts.
//
// Endpoint: POST {instance}/legacy:legacyFetchUdmSearchView (chronicle host,
// v1alpha; project ID form). The response is a streamed JSON array of progress
// chunks: events/detections/aggregations follow REPLACE-not-append semantics, so
// the last non-empty list of each wins and the terminal complete chunk omits them.
func (c *Client) FetchUDMSearchView(ctx context.Context, query string, start, end time.Time, opts UDMSearchViewOptions) (*UDMSearchView, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("chronicle: FetchUDMSearchView requires a non-empty query")
	}
	if !start.Before(end) {
		return nil, fmt.Errorf("chronicle: FetchUDMSearchView start (%s) must be before end (%s)",
			start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	}

	body := udmSearchViewRequest{
		BaselineQuery: query,
		BaselineTimeRange: searchTimeRange{
			StartTime: start.UTC().Format(rawLogSearchTimeLayout),
			EndTime:   end.UTC().Format(rawLogSearchTimeLayout),
		},
		SnapshotQuery:         opts.SnapshotQuery,
		CaseInsensitive:       opts.CaseInsensitive,
		GenerateAIOverview:    opts.GenerateAIOverview,
		ReturnOperationIDOnly: opts.ReturnOperationIDOnly,
	}
	if opts.MaxEvents > 0 {
		body.EventList = &udmViewEventListOpts{MaxReturnedEvents: opts.MaxEvents}
	}
	if opts.MaxValuesPerField > 0 {
		body.FieldAggregations = &udmViewAggOpts{MaxValuesPerField: opts.MaxValuesPerField}
	}
	if opts.MaxDetections > 0 || opts.DetectionSnapshotQuery != "" || opts.FetchNonAlertingDetections {
		d := &udmViewDetectionOpts{
			SnapshotQuery:              opts.DetectionSnapshotQuery,
			FetchNonAlertingDetections: opts.FetchNonAlertingDetections,
		}
		if opts.MaxDetections > 0 {
			d.DetectionList = &udmViewDetectionList{MaxReturnedDetections: opts.MaxDetections}
		}
		body.DetectionOptions = d
	}
	if opts.Prevalence {
		p := &udmViewPrevalenceOpts{GetPrevalence: true}
		if opts.PrevalenceBucketSeconds > 0 {
			p.BucketSize = &udmViewBucketSize{ResolutionInSeconds: opts.PrevalenceBucketSeconds}
		}
		body.Prevalence = p
	}

	var raw json.RawMessage
	if err := c.post(ctx, c.resourcePath("legacy:legacyFetchUdmSearchView", false), body, &raw); err != nil {
		return nil, err
	}
	chunks, err := decodeStreamChunks[udmSearchViewChunk](raw)
	if err != nil {
		return nil, fmt.Errorf("chronicle: decode search view: %w", err)
	}
	return assembleSearchView(chunks)
}

// assembleSearchView folds the streamed chunks into one UDMSearchView, applying
// the replace-not-append semantics (keep the last non-empty events / detections /
// aggregations) and taking the max of each count.
func assembleSearchView(chunks []udmSearchViewChunk) (*UDMSearchView, error) {
	v := &UDMSearchView{}
	for i := range chunks {
		ch := &chunks[i]
		if len(ch.QueryValidationErrors) > 0 {
			return nil, fmt.Errorf("chronicle: search view query invalid: %s", string(ch.QueryValidationErrors[0]))
		}
		if len(ch.RuntimeErrors) > 0 {
			return nil, fmt.Errorf("chronicle: search view runtime error: %s", string(ch.RuntimeErrors[0]))
		}
		if ch.Operation != "" {
			v.OperationID = ch.Operation
		}
		if ch.Events != nil && len(ch.Events.Events) > 0 {
			v.Events = ch.Events.Events
		}
		if ch.Detections != nil && len(ch.Detections.Detections) > 0 {
			v.Detections = ch.Detections.Detections
		}
		if ch.Detections != nil {
			v.BaselineAlertsCount = max(v.BaselineAlertsCount, ch.Detections.BaselineAlertsCount)
			v.FilteredAlertsCount = max(v.FilteredAlertsCount, ch.Detections.FilteredAlertsCount)
		}
		if ch.FieldAggregations != nil && len(ch.FieldAggregations.Fields) > 0 {
			v.Aggregations = ch.FieldAggregations.Fields
		}
		if len(ch.Prevalence) > 0 {
			v.Prevalence = ch.Prevalence
		}
		if ch.AIOverview != nil && ch.AIOverview.AISummary != "" {
			v.AIOverview = ch.AIOverview.AISummary
		}
		v.BaselineEventsCount = max(v.BaselineEventsCount, ch.BaselineEventsCount)
		v.FilteredEventsCount = max(v.FilteredEventsCount, ch.FilteredEventsCount)
		v.AvailableResultCount = max(v.AvailableResultCount, ch.AvailableResultCount)
		v.Progress = max(v.Progress, ch.Progress)
		v.TooManyEvents = v.TooManyEvents || ch.TooManyEvents
		v.TooLargeResponse = v.TooLargeResponse || ch.TooLargeResponse
		v.Complete = v.Complete || ch.Complete
	}
	return v, nil
}
