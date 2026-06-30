package chronicle

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// Log-type, UDM-mapping, and query-validation helpers. These mirror the
// official wrapper's log_types.py, udm_mapping.py, and validate.py modules.
//
// Every endpoint here builds its URL from the string project_id (the wrapper's
// instance_id), so all resource paths use numeric=false. The RPC-style methods
// (logs:classify, :findUdmFieldValues, :validateQuery) hang directly off the
// instance with no separating slash (see resource.go / instancePath).

// LogType is a Chronicle ingestion log type (e.g. "OKTA", "WINEVTLOG").
//
// Name is the resource name projects/.../logTypes/<id>; the id used by the
// ingestion and rules APIs is the trailing segment (see LogTypeID). DisplayName
// is the human-readable description.
type LogType struct {
	Name               string          `json:"name,omitempty"`
	DisplayName        string          `json:"displayName,omitempty"`
	ProductSource      string          `json:"productSource,omitempty"`
	IsCustom           bool            `json:"isCustom,omitempty"`
	Golden             bool            `json:"golden,omitempty"`
	CustomLogTypeLabel string          `json:"customLogTypeLabel,omitempty"`
	HasCustomParser    bool            `json:"hasCustomParser,omitempty"`
	ParserType         string          `json:"parserType,omitempty"`
	LastIngestedTime   string          `json:"lastIngestedTime,omitempty"`
	FeedCount          int             `json:"feedCount,omitempty"`
	CollectionTime     string          `json:"collectionTime,omitempty"`
	Raw                json.RawMessage `json:"-"`
}

func (l *LogType) UnmarshalJSON(b []byte) error {
	type alias LogType
	if err := json.Unmarshal(b, (*alias)(l)); err != nil {
		return err
	}
	l.Raw = append(l.Raw[:0:0], b...)
	return nil
}

// LogTypeID returns the trailing <id> segment of the log type's resource Name
// (e.g. "OKTA"). For a bare id (no slashes) it returns the input unchanged.
func (l *LogType) LogTypeID() string {
	if l == nil || l.Name == "" {
		return ""
	}
	return l.Name[strings.LastIndex(l.Name, "/")+1:]
}

// ListLogTypes returns the instance's ingestion log types, optionally filtered
// client-side by search (case-insensitive substring over both the log type id
// and its display name; "" returns all).
//
// pageSize caps results per API page (0 lets the server choose). The full set
// is paged through regardless of pageSize.
//
// DEVIATION: the wrapper's load_log_types caches and re-filters in three
// separate helpers (search_log_types / get_log_type_description). We fetch once,
// page transparently, and apply the same id-or-displayName substring match in a
// single call. The /logTypes collection has no server-side search param, so the
// filter is necessarily client-side — same as the wrapper.
func (c *Client) ListLogTypes(ctx context.Context, search string, pageSize int) ([]LogType, error) {
	needle := strings.ToLower(search)
	var all []LogType
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{}
		if pageSize > 0 {
			q.Set("pageSize", strconv.Itoa(pageSize))
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			LogTypes      []LogType `json:"logTypes"`
			NextPageToken string    `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("logTypes", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		for _, lt := range resp.LogTypes {
			if needle == "" || logTypeMatches(lt, needle) {
				all = append(all, lt)
			}
		}
		return resp.NextPageToken, nil
	})
	return all, err
}

// logTypeMatches reports whether needle (already lowercased) appears in the log
// type's id or display name.
func logTypeMatches(lt LogType, needle string) bool {
	if strings.Contains(strings.ToLower(lt.LogTypeID()), needle) {
		return true
	}
	return strings.Contains(strings.ToLower(lt.DisplayName), needle)
}

// GetLogTypeDescription returns the display name (description) for a log type
// id (e.g. "OKTA"). It returns the empty string with a nil error when no log
// type matches — mirroring the wrapper's get_log_type_description, which returns
// None on miss rather than raising.
//
// DEVIATION: the wrapper scans its full cached list. We page the /logTypes
// collection and short-circuit on the first id match.
func (c *Client) GetLogTypeDescription(ctx context.Context, logType string) (string, error) {
	suffix := "/logTypes/" + logType
	var desc string
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			LogTypes      []LogType `json:"logTypes"`
			NextPageToken string    `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("logTypes", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		for _, lt := range resp.LogTypes {
			if strings.HasSuffix(lt.Name, suffix) || lt.LogTypeID() == logType {
				desc = lt.DisplayName
				return "", nil // found: stop paging
			}
		}
		return resp.NextPageToken, nil
	})
	return desc, err
}

// CreateLogType creates a custom log type. logTypeID is the slug (e.g.
// "my_custom_log", 4-63 chars, /[a-z][0-9]-/). displayName is the label shown
// in the UI/feed creation. Returns the newly created LogType.
func (c *Client) CreateLogType(ctx context.Context, logTypeID, displayName string) (*LogType, error) {
	if !strings.HasSuffix(logTypeID, "_CUSTOM") {
		logTypeID += "_CUSTOM"
	}
	if !strings.HasSuffix(displayName, " Custom") {
		displayName += " Custom"
	}
	body := struct {
		DisplayName        string `json:"displayName"`
		ProductSource      string `json:"productSource"`
		IsCustom           bool   `json:"isCustom"`
		CustomLogTypeLabel string `json:"customLogTypeLabel"`
	}{
		DisplayName:        displayName,
		ProductSource:      displayName,
		IsCustom:           true,
		CustomLogTypeLabel: logTypeID,
	}
	q := url.Values{"logTypeId": {logTypeID}}
	var out LogType
	if err := c.post(ctx, c.resourcePath("logTypes", false), body, &out, withQuery(q)); err != nil {
		return nil, err
	}
	return &out, nil
}

// ValueMatch is one ingested UDM field value matching a findUdmFieldValues
// query: the value string, the field path it was seen on, and metadata.
type ValueMatch struct {
	FieldPath     string `json:"fieldPath,omitempty"`
	Value         string `json:"value,omitempty"`
	IngestionTime string `json:"ingestionTime,omitempty"`
	MatchEnd      int    `json:"matchEnd,omitempty"`
}

// fieldMatch is a UDM field path whose name matches the query.
type fieldMatch struct {
	FieldPath string `json:"fieldPath,omitempty"`
}

// FindUDMFieldValues returns ingested UDM field values whose value matches the
// partial query string, optionally restricted to a single fieldPath.
//
// query is the partial value to match (the wrapper's only filter). fieldPath,
// when non-empty, keeps only matches observed on exactly that UDM field. The
// returned slice is the de-duplicated set of matching value strings, in first-
// seen order. pageSize caps API results (0 lets the server choose).
//
// DEVIATION: the wrapper's find_udm_field_values takes only (query, page_size)
// and returns the raw {valueMatches, fieldMatches} blob. The :findUdmFieldValues
// endpoint has no fieldPath request param, so we honor the task's fieldPath
// argument as a client-side filter over the returned valueMatches and distill
// the result to the values the caller actually wants.
func (c *Client) FindUDMFieldValues(ctx context.Context, fieldPath, query string, pageSize int) ([]string, error) {
	q := url.Values{"query": {query}}
	if pageSize > 0 {
		q.Set("pageSize", strconv.Itoa(pageSize))
	}
	var resp struct {
		ValueMatches []ValueMatch `json:"valueMatches"`
		FieldMatches []fieldMatch `json:"fieldMatches"`
	}
	// RPC-style method directly on the instance: {instance}:findUdmFieldValues.
	if err := c.get(ctx, c.instancePath(false)+":findUdmFieldValues", &resp, withQuery(q)); err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(resp.ValueMatches))
	var values []string
	for _, m := range resp.ValueMatches {
		if fieldPath != "" && m.FieldPath != fieldPath {
			continue
		}
		if m.Value == "" {
			continue
		}
		if _, dup := seen[m.Value]; dup {
			continue
		}
		seen[m.Value] = struct{}{}
		values = append(values, m.Value)
	}
	return values, nil
}

// QueryValidation is the result of a UDM query syntax check (:validateQuery).
//
// The documented v1alpha response carries no isValid/validationMessage pair: it is
// {queryType, errorType, errorText, errorPosition}, where a valid query yields just
// {"queryType":...} and an invalid one adds errorType/errorText/errorPosition.
// Validity is therefore derived (no error reported = valid). The decode also still
// honors an explicit isValid/validationMessage if a server ever sends that shape.
type QueryValidation struct {
	IsValid           bool            `json:"isValid"`
	QueryType         string          `json:"queryType,omitempty"`         // e.g. QUERY_TYPE_UDM_QUERY, QUERY_TYPE_STATS_QUERY
	ErrorType         string          `json:"errorType,omitempty"`         // e.g. INVALID_QUERY_TYPE (empty when valid)
	ValidationMessage string          `json:"validationMessage,omitempty"` // the server's errorText, when invalid
	ErrorPosition     json.RawMessage `json:"errorPosition,omitempty"`     // {startLine,startColumn,endLine,endColumn}
}

// UnmarshalJSON maps the documented :validateQuery body (queryType / errorType /
// errorText / errorPosition) onto the typed fields and derives IsValid, while
// tolerating an isValid/validationMessage shape if one is ever sent.
func (v *QueryValidation) UnmarshalJSON(b []byte) error {
	var raw struct {
		QueryType         string          `json:"queryType"`
		ErrorType         string          `json:"errorType"`
		ErrorText         string          `json:"errorText"`
		ErrorPosition     json.RawMessage `json:"errorPosition"`
		IsValid           *bool           `json:"isValid"`           // older shape, if present
		ValidationMessage string          `json:"validationMessage"` // older shape, if present
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	v.QueryType = raw.QueryType
	v.ErrorType = raw.ErrorType
	v.ErrorPosition = raw.ErrorPosition
	v.ValidationMessage = raw.ErrorText
	if v.ValidationMessage == "" {
		v.ValidationMessage = raw.ValidationMessage
	}
	switch {
	case raw.IsValid != nil:
		v.IsValid = *raw.IsValid
	default:
		v.IsValid = raw.ErrorType == "" && raw.ErrorText == ""
	}
	return nil
}

// ValidateQuery checks UDM query syntax without running the search, returning
// the API's validity verdict, detected query type, and any error message.
//
// The wrapper replaces each "!" in rawQuery with a backslash followed by
// "u0021" (the unicode escape); the server does not treat a percent-encoded
// %21 the same way, so we mirror that escaping exactly. The backslash is built
// from its 0x5c byte so this source never embeds a literal backslash (which Go
// would otherwise read as the start of a unicode escape).
func (c *Client) ValidateQuery(ctx context.Context, query string) (*QueryValidation, error) {
	q := url.Values{
		"rawQuery":                    {strings.ReplaceAll(query, "!", string([]byte{0x5c})+"u0021")},
		"dialect":                     {"DIALECT_UDM_SEARCH"},
		"allowUnreplacedPlaceholders": {"false"},
	}
	var v QueryValidation
	// RPC-style method directly on the instance: {instance}:validateQuery.
	if err := c.get(ctx, c.instancePath(false)+":validateQuery", &v, withQuery(q)); err != nil {
		return nil, err
	}
	return &v, nil
}

// LogClassification is one predicted log type for a raw sample log, with the
// API's (advisory) confidence score.
type LogClassification struct {
	LogType string  `json:"logType,omitempty"`
	Score   float64 `json:"score,omitempty"`
}

// ClassifyLogType predicts the most likely log type for a raw sample log via
// the logs:classify endpoint, returning the highest-scoring log type id (e.g.
// "OKTA"). It returns "" with a nil error when the API yields no prediction.
//
// Scores are API-provided guidance only; for the full ranked list use
// ClassifyLogTypeAll.
//
// DEVIATION: the wrapper's classify_logs returns the raw prediction list. We
// add a convenience that returns the single best id (the common case) while
// keeping the full list available via ClassifyLogTypeAll.
func (c *Client) ClassifyLogType(ctx context.Context, sampleLog string) (string, error) {
	preds, err := c.ClassifyLogTypeAll(ctx, sampleLog)
	if err != nil {
		return "", err
	}
	if len(preds) == 0 {
		return "", nil
	}
	best := preds[0]
	for _, p := range preds[1:] {
		if p.Score > best.Score {
			best = p
		}
	}
	return best.LogType, nil
}

// ClassifyLogTypeAll returns every predicted log type for a raw sample log,
// in the order the API ranks them (typically descending confidence).
//
// The sample is base64-encoded before sending, matching the API's logData
// contract.
func (c *Client) ClassifyLogTypeAll(ctx context.Context, sampleLog string) ([]LogClassification, error) {
	encoded := base64.StdEncoding.EncodeToString([]byte(sampleLog))
	body := struct {
		LogData []string `json:"logData"`
	}{LogData: []string{encoded}}

	var resp struct {
		Predictions []LogClassification `json:"predictions"`
	}
	// RPC-style method directly on the instance: {instance}/logs:classify.
	if err := c.post(ctx, c.resourcePath("logs:classify", false), body, &resp); err != nil {
		return nil, err
	}
	return resp.Predictions, nil
}
