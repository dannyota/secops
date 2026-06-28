package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
)

// NativeDashboard is one entry from the nativeDashboards listing. The BASIC
// view (used by List) returns all typed fields below; the FULL view (used by
// Get) additionally populates Definition.Charts with layout and chart refs.
// Raw retains the complete server JSON for lossless round-trips.
type NativeDashboard struct {
	Name        string `json:"name"`                  // projects/.../nativeDashboards/<id>
	DisplayName string `json:"displayName"`           // human-readable title
	Type        string `json:"type"`                  // "CUSTOM" or "CURATED"
	Description string `json:"description,omitempty"` // optional description
	Access      string `json:"access,omitempty"`      // DASHBOARD_PUBLIC or DASHBOARD_PRIVATE
	CreateTime  string `json:"createTime,omitempty"`  // RFC 3339
	UpdateTime  string `json:"updateTime,omitempty"`  // RFC 3339
	CreateUser  string `json:"createUserId,omitempty"`
	UpdateUser  string `json:"updateUserId,omitempty"`
	Etag        string `json:"etag,omitempty"`

	// Raw is the complete, unmodified JSON object for this dashboard.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields via an alias (to avoid recursion) and
// stashes the full object bytes in Raw.
func (d *NativeDashboard) UnmarshalJSON(data []byte) error {
	type alias NativeDashboard
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*d = NativeDashboard(a)
	d.Raw = append(d.Raw[:0], data...)
	return nil
}

// ListNativeDashboards returns all native dashboards. The API returns BASIC
// view (no view= param) which includes name, displayName, type, description,
// access, createTime, updateTime, createUserId, updateUserId, etag — everything
// except definition.charts (those are in FULL view, which the list endpoint
// rejects). The full fields are available in each entry's Raw.
//
// DEVIATION: native dashboards use the project ID (string) form, so this calls
// resourcePath with numeric=false. The roadmap table that grouped
// nativeDashboards under the numeric form is wrong; the live API and the legacy
// tool both pass the string project ID for this endpoint.
func (c *Client) ListNativeDashboards(ctx context.Context) ([]NativeDashboard, error) {
	var all []NativeDashboard
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			NativeDashboards []NativeDashboard `json:"nativeDashboards"`
			NextPageToken    string            `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("nativeDashboards", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.NativeDashboards...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// ExportDashboard returns the full export (charts + queries) of a single CUSTOM
// dashboard as raw JSON, suitable for re-import.
//
// dashboardName may be a bare dashboard id or a fully-qualified resource name
// (projects/.../nativeDashboards/<id>); a bare id is qualified against the
// instance path. The endpoint is POST {instance}/nativeDashboards:export with
// a {"names": [...]} body, mirroring the official wrapper.
//
// DEVIATION: only CUSTOM dashboards are exportable; CURATED ones return an
// error from the server, so callers should gate on NativeDashboard.Type and
// fall back to the listing entry's Raw for CURATED dashboards.
func (c *Client) ExportDashboard(ctx context.Context, dashboardName string) (json.RawMessage, error) {
	name := dashboardName
	if !isQualifiedName(name) {
		name = c.instancePath(false) + "/nativeDashboards/" + name
	}
	body := struct {
		Names []string `json:"names"`
	}{Names: []string{name}}

	var resp json.RawMessage
	if err := c.post(ctx, c.resourcePath("nativeDashboards:export", false), body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// isQualifiedName reports whether name is already a full resource name.
func isQualifiedName(name string) bool {
	return len(name) >= len("projects/") && name[:len("projects/")] == "projects/"
}
