package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
)

// Enrichment controls turn OFF a UDM enrichment for a log type / enrichment type
// (the enrichment process populates events with asset/user/process/GTI/GeoIP
// context). The surface is OPERATED IMPERATIVELY, not via the reconcile engine:
// there is no patch, a create for an existing control just appends a record (the
// resource accumulates time-ranged records rather than being replaced), and the
// :disable verb closes the most recent record's time range. So config-as-code
// round-tripping doesn't fit; callers create/list/get/disable/delete directly.
//
// Modern chronicle host, instance path (numeric=false).

// EnrichmentType is the kind of enrichment a control targets.
type EnrichmentType string

const (
	EnrichmentAllTypes EnrichmentType = "ALL_TYPES"
	EnrichmentAsset    EnrichmentType = "ASSET"
	EnrichmentUser     EnrichmentType = "USER"
	EnrichmentProcess  EnrichmentType = "PROCESS"
	EnrichmentGoogleTI EnrichmentType = "GOOGLE_THREAT_INTEL"
	EnrichmentGeoIP    EnrichmentType = "GEOIP"
)

// EnrichmentControl is a control over an enrichment. The option and records are
// kept as raw JSON (the option carries a oneof and the records are output-only,
// accumulating activity entries).
type EnrichmentControl struct {
	Name        string          `json:"name,omitempty"`
	DisplayName string          `json:"displayName,omitempty"`
	Description string          `json:"description,omitempty"`
	Option      json.RawMessage `json:"enrichmentControlOption,omitempty"`
	Records     json.RawMessage `json:"records,omitempty"` // output only
	Raw         json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and retains the full payload in Raw.
func (e *EnrichmentControl) UnmarshalJSON(data []byte) error {
	type alias EnrichmentControl
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*e = EnrichmentControl(a)
	e.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ID returns the trailing resource-id segment of the control's Name.
func (e *EnrichmentControl) ID() string {
	if e == nil {
		return ""
	}
	return lastSegment(e.Name)
}

// ListEnrichmentControls returns every enrichment control in the instance,
// paginating over nextPageToken.
func (c *Client) ListEnrichmentControls(ctx context.Context) ([]EnrichmentControl, error) {
	var all []EnrichmentControl
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"100"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			EnrichmentControls []EnrichmentControl `json:"enrichmentControls"`
			NextPageToken      string              `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("enrichmentControls", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.EnrichmentControls...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetEnrichmentControl fetches a single enrichment control by its <id> segment.
func (c *Client) GetEnrichmentControl(ctx context.Context, id string) (*EnrichmentControl, error) {
	var e EnrichmentControl
	if err := c.get(ctx, c.resourcePath("enrichmentControls/"+url.PathEscape(lastSegment(id)), false), &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// CreateEnrichmentControl creates (or appends a record to) an enrichment control
// from the given body. Returns the resulting control.
func (c *Client) CreateEnrichmentControl(ctx context.Context, body json.RawMessage) (*EnrichmentControl, error) {
	var e EnrichmentControl
	if err := c.post(ctx, c.resourcePath("enrichmentControls", false), body, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// DisableEnrichmentControl closes the most recent record of a control (POST
// {name}:disable), re-enabling the enrichment from now on.
func (c *Client) DisableEnrichmentControl(ctx context.Context, id string) (*EnrichmentControl, error) {
	path := c.resourcePath("enrichmentControls/"+url.PathEscape(lastSegment(id)), false) + ":disable"
	var e EnrichmentControl
	if err := c.post(ctx, path, struct{}{}, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// DeleteEnrichmentControl deletes an enrichment control by its <id> segment.
func (c *Client) DeleteEnrichmentControl(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", c.resourcePath("enrichmentControls/"+url.PathEscape(lastSegment(id)), false), nil, nil)
}
