package chronicle

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Data Export endpoints export Chronicle data to a Google Cloud Storage bucket.
// Every URL is built from the instance path with the string project ID
// (numeric=false), matching the wrapper, which builds dataExports paths off the
// string project_id. See resource.go for why the form is explicit per endpoint.

// dataExportTimeFormat is the millisecond-precision RFC 3339 form the Data
// Export API expects (e.g. "2006-01-02T15:04:05.000000Z"). The wrapper formats
// every time with strftime("%Y-%m-%dT%H:%M:%S.%fZ"); we mirror that precision.
const dataExportTimeFormat = "2006-01-02T15:04:05.000000Z"

func formatDataExportTime(t time.Time) string {
	return t.UTC().Format(dataExportTimeFormat)
}

// DataExportStatus is the lifecycle status of a data export job.
//
// Stage is the export stage/state enum reported by the API — one of
// IN_QUEUE, PROCESSING, FINISHED_SUCCESS, FINISHED_FAILURE, CANCELLED
// (the API spells the state under "stage"). ProgressPercentage and
// FailureReason are populated as a job advances.
type DataExportStatus struct {
	Stage              string `json:"stage,omitempty"`
	ProgressPercentage int    `json:"progressPercentage,omitempty"`
	FailureReason      string `json:"failureReason,omitempty"`
}

// DataExport is a single Chronicle-to-GCS export job.
//
// Name is the export resource (projects/.../dataExports/<id>); the trailing
// segment is the data_export_id the other methods take (see ExportID).
// IncludeLogTypes is empty when the job exports all log types.
type DataExport struct {
	Name             string            `json:"name,omitempty"`
	StartTime        string            `json:"startTime,omitempty"`
	EndTime          string            `json:"endTime,omitempty"`
	GCSBucket        string            `json:"gcsBucket,omitempty"`
	IncludeLogTypes  []string          `json:"includeLogTypes,omitempty"`
	DataExportStatus *DataExportStatus `json:"dataExportStatus,omitempty"`
}

// ExportID returns the trailing <id> segment of the export's resource Name,
// the identifier GetDataExport/CancelDataExport/UpdateDataExport expect.
func (e *DataExport) ExportID() string {
	if e == nil || e.Name == "" {
		return ""
	}
	return e.Name[strings.LastIndex(e.Name, "/")+1:]
}

// formatLogType expands a bare log-type name into a fully-qualified log-type
// resource (mirrors the wrapper's _get_formatted_log_type). An input that
// already contains a "/" is assumed fully-qualified and returned unchanged.
func (c *Client) formatLogType(logType string) string {
	if strings.Contains(logType, "/") {
		return logType
	}
	return c.instancePath(false) + "/logTypes/" + logType
}

// CreateDataExport creates a new export job writing the given log types to
// gcsBucket over [start, end). gcsBucket must be in the form
// "projects/{project}/buckets/{bucket}".
//
// Passing an empty logTypes slice exports ALL log types (the API treats an empty
// includeLogTypes as "everything"), matching the wrapper's export_all_logs path.
// Each bare log-type name is expanded to a full log-type resource.
//
// DEVIATION: the wrapper exposes a deprecated singular log_type plus a separate
// export_all_logs flag and validates their combinations in Python. We collapse
// to one []string parameter: a populated slice exports those types, an empty
// slice exports all — no redundant flag, no client-side combination errors.
func (c *Client) CreateDataExport(ctx context.Context, gcsBucket string, logTypes []string, start, end time.Time) (*DataExport, error) {
	if gcsBucket == "" {
		return nil, &APIError{Method: "POST", URL: c.resourcePath("dataExports", false), Body: "gcsBucket must be provided"}
	}
	if !strings.HasPrefix(gcsBucket, "projects/") {
		return nil, &APIError{Method: "POST", URL: c.resourcePath("dataExports", false), Body: "gcsBucket must be in format: projects/{project}/buckets/{bucket}"}
	}
	if !end.After(start) {
		return nil, &APIError{Method: "POST", URL: c.resourcePath("dataExports", false), Body: "end time must be after start time"}
	}

	include := make([]string, 0, len(logTypes))
	for _, lt := range logTypes {
		include = append(include, c.formatLogType(lt))
	}

	body := struct {
		StartTime       string   `json:"startTime"`
		EndTime         string   `json:"endTime"`
		GCSBucket       string   `json:"gcsBucket"`
		IncludeLogTypes []string `json:"includeLogTypes"`
	}{
		StartTime:       formatDataExportTime(start),
		EndTime:         formatDataExportTime(end),
		GCSBucket:       gcsBucket,
		IncludeLogTypes: include,
	}

	var out DataExport
	if err := c.post(ctx, c.resourcePath("dataExports", false), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetDataExport fetches a single export job by ID.
func (c *Client) GetDataExport(ctx context.Context, id string) (*DataExport, error) {
	var out DataExport
	if err := c.get(ctx, c.resourcePath("dataExports/"+id, false), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDataExports returns every export job on the instance. pageSize bounds the
// per-request page (the API caps it); <= 0 lets the server choose.
func (c *Client) ListDataExports(ctx context.Context, pageSize int) ([]DataExport, error) {
	var all []DataExport
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{}
		if pageSize > 0 {
			q.Set("pageSize", fmt.Sprintf("%d", pageSize))
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			DataExports   []DataExport `json:"dataExports"`
			NextPageToken string       `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("dataExports", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.DataExports...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// CancelDataExport cancels an in-progress export job and returns its updated
// state. The cancel verb is an RPC-style method on the resource:
// dataExports/<id>:cancel (no path separator).
func (c *Client) CancelDataExport(ctx context.Context, id string) (*DataExport, error) {
	var out DataExport
	if err := c.post(ctx, c.resourcePath("dataExports/"+id, false)+":cancel", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DataExportUpdate is a sparse update to an export job. Only the non-nil fields
// are sent, and the updateMask is built from exactly those fields. The job must
// be in the IN_QUEUE state for an update to be accepted.
type DataExportUpdate struct {
	Start     *time.Time // nil leaves the start time unchanged
	End       *time.Time // nil leaves the end time unchanged
	GCSBucket *string    // nil leaves the bucket unchanged
	LogTypes  *[]string  // nil leaves log types unchanged; non-nil replaces (each name is expanded)
}

// UpdateDataExport patches an export job, sending only the fields set on upd and
// an updateMask covering exactly those fields.
//
// DEVIATION: the body is assembled as a map so an explicit empty includeLogTypes
// (export-all) is sent verbatim rather than dropped by struct omitempty — the
// updateMask and body never drift. (The wrapper builds the same sparse payload.)
func (c *Client) UpdateDataExport(ctx context.Context, id string, upd DataExportUpdate) (*DataExport, error) {
	body := map[string]any{}
	var mask []string
	if upd.Start != nil {
		body["startTime"] = formatDataExportTime(*upd.Start)
		mask = append(mask, "startTime")
	}
	if upd.End != nil {
		body["endTime"] = formatDataExportTime(*upd.End)
		mask = append(mask, "endTime")
	}
	if upd.GCSBucket != nil {
		body["gcsBucket"] = *upd.GCSBucket
		mask = append(mask, "gcsBucket")
	}
	if upd.LogTypes != nil {
		inc := make([]string, 0, len(*upd.LogTypes))
		for _, lt := range *upd.LogTypes {
			inc = append(inc, c.formatLogType(lt))
		}
		body["includeLogTypes"] = inc
		mask = append(mask, "includeLogTypes")
	}
	if len(mask) == 0 {
		return nil, &APIError{Method: "PATCH", URL: c.resourcePath("dataExports/"+id, false), Body: "no fields provided to update"}
	}

	q := url.Values{"updateMask": {strings.Join(mask, ",")}}
	var out DataExport
	if err := c.patch(ctx, c.resourcePath("dataExports/"+id, false), body, &out, withQuery(q)); err != nil {
		return nil, err
	}
	return &out, nil
}

// AvailableLogType is a log type that can be exported, with the time window over
// which data exists. StartTime/EndTime are RFC 3339 strings as returned by the
// API (kept as strings to avoid lossy round-tripping).
//
// NOTE: unlike the create/get/list/cancel bodies (camelCase), the
// dataExports:fetchavailablelogtypes *response* is snake_case, so these tags
// (and the response wrapper below) use snake_case to match the live payload.
type AvailableLogType struct {
	LogType     string `json:"log_type,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	StartTime   string `json:"start_time,omitempty"`
	EndTime     string `json:"end_time,omitempty"`
}

// FetchAvailableLogTypes returns the log types exportable over [start, end),
// paginating through all pages. The verb is an RPC-style method on the
// collection: dataExports:fetchavailablelogtypes, with the time range and page
// token in the POST body (the wrapper passes pageSize/pageToken in-body, not as
// query params).
//
// DEVIATION: the wrapper returns a single page plus a next_page_token for the
// caller to loop on; we drive the foundation paginator and return the complete
// set, consistent with the other List* methods here.
func (c *Client) FetchAvailableLogTypes(ctx context.Context, start, end time.Time) ([]AvailableLogType, error) {
	if !end.After(start) {
		return nil, &APIError{Method: "POST", URL: c.resourcePath("dataExports", false), Body: "end time must be after start time"}
	}

	path := c.resourcePath("dataExports", false) + ":fetchavailablelogtypes"
	startStr := formatDataExportTime(start)
	endStr := formatDataExportTime(end)

	var all []AvailableLogType
	err := paginate(50, func(token string) (string, error) {
		body := struct {
			StartTime string `json:"startTime"`
			EndTime   string `json:"endTime"`
			PageToken string `json:"pageToken,omitempty"`
		}{StartTime: startStr, EndTime: endStr, PageToken: token}

		var resp struct {
			AvailableLogTypes []AvailableLogType `json:"available_log_types"`
			NextPageToken     string             `json:"next_page_token"`
		}
		if err := c.post(ctx, path, body, &resp); err != nil {
			return "", err
		}
		all = append(all, resp.AvailableLogTypes...)
		return resp.NextPageToken, nil
	})
	return all, err
}
