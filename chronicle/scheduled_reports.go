package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// Scheduled dashboard reports (dashboardScheduledReports): a recurring delivery of
// a native dashboard rendered to PDF/CSV/PNG and emailed (or written to GCS) on a
// cron schedule. Full CRUD plus imperative trigger/duplicate/fetchHistory. The
// resource id is server-assigned on create; an etag guards concurrent edits. All
// endpoints use the project ID (string) form (numeric=false), matching the sibling
// dashboards surface.

// ReportFileFormat is the rendered output format of a scheduled report.
type ReportFileFormat string

const (
	ReportFormatPDF ReportFileFormat = "FILE_FORMAT_PDF"
	ReportFormatCSV ReportFileFormat = "FILE_FORMAT_CSV"
	ReportFormatPNG ReportFileFormat = "FILE_FORMAT_PNG"
)

// scheduledReportWritableMask is the FieldMask (snake_case) for the operator-
// editable fields of a scheduled report — exactly the keys the reconcile surface
// round-trips, so a whole-body update never touches output-only/server fields.
var scheduledReportWritableMask = []string{
	"display_name", "description", "user_data", "dashboard",
	"scope_info", "cron_details", "delivery_details", "format",
}

// DashboardScheduledReport is a scheduled-report config. The operator-editable
// blocks (dashboard reference, schedule, delivery, format, scope) are kept as raw
// JSON so this SDK round-trips the server shape verbatim without chasing every
// nested field; identity/state/etag are typed.
type DashboardScheduledReport struct {
	Name            string          `json:"name,omitempty"`
	DisplayName     string          `json:"displayName,omitempty"`
	Description     string          `json:"description,omitempty"`
	Status          string          `json:"status,omitempty"` // output only (ReportState)
	UserData        json.RawMessage `json:"userData,omitempty"`
	Dashboard       json.RawMessage `json:"dashboard,omitempty"`
	ScopeInfo       json.RawMessage `json:"scopeInfo,omitempty"`
	CronDetails     json.RawMessage `json:"cronDetails,omitempty"`
	DeliveryDetails json.RawMessage `json:"deliveryDetails,omitempty"`
	Format          json.RawMessage `json:"format,omitempty"`
	Etag            string          `json:"etag,omitempty"`
	Raw             json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and retains the full payload in Raw.
func (r *DashboardScheduledReport) UnmarshalJSON(data []byte) error {
	type alias DashboardScheduledReport
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = DashboardScheduledReport(a)
	r.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ID returns the trailing resource-id segment of the report's Name.
func (r *DashboardScheduledReport) ID() string {
	if r == nil {
		return ""
	}
	return lastSegment(r.Name)
}

// ListScheduledReports returns every scheduled report in the instance, paginating
// over nextPageToken.
func (c *Client) ListScheduledReports(ctx context.Context) ([]DashboardScheduledReport, error) {
	var all []DashboardScheduledReport
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"100"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			DashboardScheduledReports []DashboardScheduledReport `json:"dashboardScheduledReports"`
			NextPageToken             string                     `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("dashboardScheduledReports", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.DashboardScheduledReports...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetScheduledReport fetches a single scheduled report by its <id> segment.
func (c *Client) GetScheduledReport(ctx context.Context, id string) (*DashboardScheduledReport, error) {
	var r DashboardScheduledReport
	if err := c.get(ctx, c.resourcePath("dashboardScheduledReports/"+url.PathEscape(lastSegment(id)), false), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateScheduledReport creates a scheduled report from the given body (a
// DashboardScheduledReport JSON object). The id is server-assigned; the created
// resource (with its name + etag) is returned.
func (c *Client) CreateScheduledReport(ctx context.Context, body json.RawMessage) (*DashboardScheduledReport, error) {
	var r DashboardScheduledReport
	if err := c.post(ctx, c.resourcePath("dashboardScheduledReports", false), body, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// UpdateScheduledReport patches a scheduled report, sending body under updateMask.
// An empty mask defaults to the full operator-editable field set (a whole-body
// update); pass etag for optimistic concurrency (it is also carried in body).
func (c *Client) UpdateScheduledReport(ctx context.Context, id string, body json.RawMessage, mask []string) (*DashboardScheduledReport, error) {
	if len(mask) == 0 {
		mask = scheduledReportWritableMask
	}
	q := url.Values{"updateMask": {strings.Join(mask, ",")}}
	var r DashboardScheduledReport
	if err := c.patch(ctx, c.resourcePath("dashboardScheduledReports/"+url.PathEscape(lastSegment(id)), false), body, &r, withQuery(q)); err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteScheduledReport deletes a scheduled report by its <id> segment. A non-empty
// etag is sent for optimistic concurrency.
func (c *Client) DeleteScheduledReport(ctx context.Context, id, etag string) error {
	path := c.resourcePath("dashboardScheduledReports/"+url.PathEscape(lastSegment(id)), false)
	var opts []requestOption
	if etag != "" {
		opts = append(opts, withQuery(url.Values{"etag": {etag}}))
	}
	return c.do(ctx, "DELETE", path, nil, nil, opts...)
}

// TriggerScheduledReport sends the report immediately (POST {name}:trigger),
// returning the freeform server response. This is an imperative action, not config.
func (c *Client) TriggerScheduledReport(ctx context.Context, id string) (json.RawMessage, error) {
	path := c.resourcePath("dashboardScheduledReports/"+url.PathEscape(lastSegment(id)), false) + ":trigger"
	var out json.RawMessage
	if err := c.post(ctx, path, struct{}{}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DuplicateScheduledReport copies an existing scheduled report (POST
// {name}:duplicate), returning the freeform server response. body carries the
// duplicate request payload (e.g. the new display name); pass nil for the
// server's defaults. Imperative action, not config.
func (c *Client) DuplicateScheduledReport(ctx context.Context, id string, body any) (json.RawMessage, error) {
	path := c.resourcePath("dashboardScheduledReports/"+url.PathEscape(lastSegment(id)), false) + ":duplicate"
	var out json.RawMessage
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FetchScheduledReportHistory retrieves the past-year run history (run count,
// status, success/failure detail) for a scheduled report (GET {name}:fetchHistory).
func (c *Client) FetchScheduledReportHistory(ctx context.Context, id string) (json.RawMessage, error) {
	path := c.resourcePath("dashboardScheduledReports/"+url.PathEscape(lastSegment(id)), false) + ":fetchHistory"
	var out json.RawMessage
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}
