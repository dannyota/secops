package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// MSSP / multi-tenant federation surfaces (chronicle host, instance path,
// numeric=false). FederationGroups group a deployment's subtenant instances;
// Tenants are a partner's subtenants; the multitenant directory enumerates the
// super/subtenants of the current deployment. These are meaningful only on
// MSSP / multi-tenant deployments — on a single-tenant instance the lists are
// empty (or the directory returns just this instance).

// FederationGroupType is the type of a federation group.
type FederationGroupType string

const (
	FederationGroupDefault FederationGroupType = "FEDERATION_GROUP_TYPE_DEFAULT"
)

// FederationGroup is a named subset of a deployment's subtenant instances.
type FederationGroup struct {
	Name               string              `json:"name,omitempty"`
	DisplayName        string              `json:"displayName,omitempty"`
	Type               FederationGroupType `json:"type,omitempty"`
	FederatedInstances []string            `json:"federatedInstances,omitempty"`
	Raw                json.RawMessage     `json:"-"`
}

// UnmarshalJSON decodes the typed fields and retains the full payload in Raw.
func (f *FederationGroup) UnmarshalJSON(data []byte) error {
	type alias FederationGroup
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*f = FederationGroup(a)
	f.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ID returns the trailing resource-id segment of the group's Name.
func (f *FederationGroup) ID() string {
	if f == nil {
		return ""
	}
	return lastSegment(f.Name)
}

// ListFederationGroups returns every federation group in the instance.
func (c *Client) ListFederationGroups(ctx context.Context) ([]FederationGroup, error) {
	var all []FederationGroup
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"100"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			FederationGroups []FederationGroup `json:"federationGroups"`
			NextPageToken    string            `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("federationGroups", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.FederationGroups...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetFederationGroup fetches a single federation group by its <id> segment.
func (c *Client) GetFederationGroup(ctx context.Context, id string) (*FederationGroup, error) {
	var f FederationGroup
	if err := c.get(ctx, c.resourcePath("federationGroups/"+url.PathEscape(lastSegment(id)), false), &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// CreateFederationGroup creates a federation group. The id is server-assigned.
func (c *Client) CreateFederationGroup(ctx context.Context, group FederationGroup) (*FederationGroup, error) {
	group.Name = ""
	var f FederationGroup
	if err := c.post(ctx, c.resourcePath("federationGroups", false), group, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// UpdateFederationGroup patches a federation group, sending body under updateMask.
// An empty mask defaults to the operator-editable field set.
func (c *Client) UpdateFederationGroup(ctx context.Context, id string, body json.RawMessage, mask []string) (*FederationGroup, error) {
	if len(mask) == 0 {
		mask = []string{"display_name", "type", "federated_instances"}
	}
	q := url.Values{"updateMask": {strings.Join(mask, ",")}}
	var f FederationGroup
	if err := c.patch(ctx, c.resourcePath("federationGroups/"+url.PathEscape(lastSegment(id)), false), body, &f, withQuery(q)); err != nil {
		return nil, err
	}
	return &f, nil
}

// DeleteFederationGroup deletes a federation group by its <id> segment.
func (c *Client) DeleteFederationGroup(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", c.resourcePath("federationGroups/"+url.PathEscape(lastSegment(id)), false), nil, nil)
}

// --- tenants (partner) + multitenant directory (read) -----------------------

// Tenant is a partner's subtenant. Most fields are output-only; partner-only
// create/update are not wrapped here (they return LROs and require partner
// privileges) — this SDK exposes the read paths.
type Tenant struct {
	Name              string          `json:"name,omitempty"`
	TenantGcpProject  string          `json:"tenantGcpProject,omitempty"`
	State             string          `json:"state,omitempty"`
	DisplayName       string          `json:"displayName,omitempty"`
	CustomerCode      string          `json:"customerCode,omitempty"`
	RetentionDuration string          `json:"retentionDuration,omitempty"`
	AuthMethod        string          `json:"authMethod,omitempty"`
	Raw               json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and retains the full payload in Raw.
func (t *Tenant) UnmarshalJSON(data []byte) error {
	type alias Tenant
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*t = Tenant(a)
	t.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ListTenants lists a partner's tenants (empty on a non-partner instance).
func (c *Client) ListTenants(ctx context.Context) ([]Tenant, error) {
	var all []Tenant
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"100"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Tenants       []Tenant `json:"tenants"`
			NextPageToken string   `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("tenants", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.Tenants...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetMultitenantDirectory returns the super/subtenants of the current deployment
// (and the current tenant), as raw JSON. On a single-tenant instance it lists just
// this instance. GET {instance}/multitenantDirectory.
func (c *Client) GetMultitenantDirectory(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.get(ctx, c.resourcePath("multitenantDirectory", false), &out); err != nil {
		return nil, err
	}
	return out, nil
}
