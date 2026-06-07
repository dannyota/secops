// forwarders.go — ingestion config (SIEM plane): forwarders and their collectors.
//
// A forwarder is an on-prem ingestion agent; collectors are the per-source inputs
// it runs. Managing them as code is core to ingestion-as-code. The unexported
// get-or-create in ingest.go covers raw-log ingest; these are the public CRUD.
// See docs/SURFACES.md.

package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// forwardersAPIVersion (forwarders + collectors) is pinned in versions.go.

// ForwarderID returns the trailing id segment of the forwarder's resource name.
func (f *Forwarder) ForwarderID() string { return lastSegment(f.Name) }

// ListForwarders returns every forwarder in the instance. Read-only.
func (c *Client) ListForwarders(ctx context.Context) ([]Forwarder, error) {
	var all []Forwarder
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Forwarders    []Forwarder `json:"forwarders"`
			NextPageToken string      `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("forwarders", false), &resp, withQuery(q), withVersion(forwardersAPIVersion)); err != nil {
			return "", err
		}
		all = append(all, resp.Forwarders...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetForwarder fetches one forwarder by short id or full resource name.
func (c *Client) GetForwarder(ctx context.Context, id string) (*Forwarder, error) {
	var out Forwarder
	if err := c.get(ctx, c.resourcePath("forwarders/"+url.PathEscape(lastSegment(id)), false), &out, withVersion(forwardersAPIVersion)); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateForwarder creates a forwarder from a freeform body (displayName + config).
// LIVE MUTATION.
func (c *Client) CreateForwarder(ctx context.Context, body any) (*Forwarder, error) {
	var out Forwarder
	if err := c.post(ctx, c.resourcePath("forwarders", false), body, &out, withVersion(forwardersAPIVersion)); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateForwarder patches a forwarder; mask lists the body fields to update. LIVE MUTATION.
func (c *Client) UpdateForwarder(ctx context.Context, id string, body any, mask ...string) (*Forwarder, error) {
	var out Forwarder
	var opts []requestOption
	if len(mask) > 0 {
		opts = append(opts, withQuery(url.Values{"updateMask": {strings.Join(mask, ",")}}))
	}
	if err := c.patch(ctx, c.resourcePath("forwarders/"+url.PathEscape(lastSegment(id)), false), body, &out, append(opts, withVersion(forwardersAPIVersion))...); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteForwarder deletes a forwarder by id. LIVE MUTATION.
func (c *Client) DeleteForwarder(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", c.resourcePath("forwarders/"+url.PathEscape(lastSegment(id)), false), nil, nil, withVersion(forwardersAPIVersion))
}

// Collector is one input source configured on a forwarder. Only the framing is
// typed; the per-source settings live in Raw.
type Collector struct {
	Name        string          `json:"name"` // .../forwarders/{fwd}/collectors/{id}
	ID          string          `json:"-"`
	DisplayName string          `json:"displayName"`
	Raw         json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes typed fields, derives ID from the name, keeps Raw.
func (c *Collector) UnmarshalJSON(data []byte) error {
	type alias Collector
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*c = Collector(a)
	c.ID = lastSegment(c.Name)
	c.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ListCollectors returns the collectors configured on a forwarder. Read-only.
func (c *Client) ListCollectors(ctx context.Context, forwarderID string) ([]Collector, error) {
	var all []Collector
	base := "forwarders/" + url.PathEscape(lastSegment(forwarderID)) + "/collectors"
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Collectors    []Collector `json:"collectors"`
			NextPageToken string      `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath(base, false), &resp, withQuery(q), withVersion(forwardersAPIVersion)); err != nil {
			return "", err
		}
		all = append(all, resp.Collectors...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetCollector fetches one collector on a forwarder by id. Read-only.
func (c *Client) GetCollector(ctx context.Context, forwarderID, collectorID string) (*Collector, error) {
	var out Collector
	sub := "forwarders/" + url.PathEscape(lastSegment(forwarderID)) + "/collectors/" + url.PathEscape(lastSegment(collectorID))
	if err := c.get(ctx, c.resourcePath(sub, false), &out, withVersion(forwardersAPIVersion)); err != nil {
		return nil, err
	}
	return &out, nil
}
