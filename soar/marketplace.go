// marketplace.go — MODERN Content Hub on the SOAR plane.
//
// The Content Hub install surface answers on the tenant SOAR host
// (siemplify-soar.com) using the v1alpha resource format + AppKey — the same plane
// as integrations/connectors here, NOT chronicle.googleapis.com (which 500s for
// this tenant type). marketplaceIntegrations is the durable twin of the legacy
// /store install path, and the only place an UNINSTALL exists. See docs/design/surfaces.md.

package soar

import (
	"context"
	"encoding/json"

	"danny.vn/secops/soar/internal/transport"
)

// MarketplaceIntegration is an installable Content-Hub integration pack. The
// stable framing is typed; the full record is in Raw.
type MarketplaceIntegration struct {
	Name        string          `json:"name"`
	Identifier  string          `json:"identifier"`
	DisplayName string          `json:"displayName"`
	IsInstalled bool            `json:"isInstalled"`
	Raw         json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and keeps the full object in Raw.
func (m *MarketplaceIntegration) UnmarshalJSON(data []byte) error {
	type alias MarketplaceIntegration
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*m = MarketplaceIntegration(a)
	m.Raw = append(json.RawMessage(nil), data...)
	return nil
}

type marketplaceList struct {
	Items         []MarketplaceIntegration `json:"marketplaceIntegrations"`
	NextPageToken string                   `json:"nextPageToken"`
}

// ListMarketplaceIntegrations returns the Content-Hub integration catalog
// (GET marketplaceIntegrations on the SOAR host, v1alpha). Read-only.
func (c *Client) ListMarketplaceIntegrations(ctx context.Context) ([]MarketplaceIntegration, error) {
	var all []MarketplaceIntegration
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var page marketplaceList
		if err := c.t.V1Alpha(ctx, "GET", "marketplaceIntegrations", nil, &page, pageTokenOpt(token)); err != nil {
			return "", err
		}
		all = append(all, page.Items...)
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// GetMarketplaceIntegration fetches one catalog entry by its identifier.
func (c *Client) GetMarketplaceIntegration(ctx context.Context, identifier string) (*MarketplaceIntegration, error) {
	var out MarketplaceIntegration
	if err := c.t.V1Alpha(ctx, "GET", "marketplaceIntegrations/"+identifier, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// InstallMarketplaceIntegration installs an integration pack (POST
// marketplaceIntegrations/{id}:install). body carries any install options. The
// modern twin of the legacy /store install. LIVE MUTATION.
func (c *Client) InstallMarketplaceIntegration(ctx context.Context, identifier string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "marketplaceIntegrations/"+identifier+":install", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UninstallMarketplaceIntegration uninstalls an integration pack (POST
// marketplaceIntegrations/{id}:uninstall) — the uninstall the legacy /store
// surface lacks. body carries any options. LIVE MUTATION.
func (c *Client) UninstallMarketplaceIntegration(ctx context.Context, identifier string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "marketplaceIntegrations/"+identifier+":uninstall", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ContentPack is a Content-Hub content pack (a bundle of playbooks/connectors/
// dashboards/use-cases). The stable framing is typed; the full record is in Raw.
type ContentPack struct {
	Name        string          `json:"name"`
	Identifier  string          `json:"identifier"`
	DisplayName string          `json:"displayName"`
	IsInstalled bool            `json:"isInstalled"`
	Raw         json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and keeps the full object in Raw.
func (p *ContentPack) UnmarshalJSON(data []byte) error {
	type alias ContentPack
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*p = ContentPack(a)
	p.Raw = append(json.RawMessage(nil), data...)
	return nil
}

type contentPacksList struct {
	Items         []ContentPack `json:"contentPacks"`
	NextPageToken string        `json:"nextPageToken"`
}

// GetContentPack fetches one Content-Hub content pack by its identifier (GET
// contentHub/contentPacks/{id} on the SOAR host, v1alpha) — the inspect-before-
// install read the list-only surface lacked. Read-only.
func (c *Client) GetContentPack(ctx context.Context, identifier string) (*ContentPack, error) {
	var out ContentPack
	if err := c.t.V1Alpha(ctx, "GET", "contentHub/contentPacks/"+identifier, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListContentPacks returns the Content-Hub content packs (GET
// contentHub/contentPacks on the SOAR host, v1alpha). Read-only.
func (c *Client) ListContentPacks(ctx context.Context) ([]ContentPack, error) {
	var all []ContentPack
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var page contentPacksList
		if err := c.t.V1Alpha(ctx, "GET", "contentHub/contentPacks", nil, &page, pageTokenOpt(token)); err != nil {
			return "", err
		}
		all = append(all, page.Items...)
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// ContentPackDeployPlaybooks deploys playbooks from a content pack.
func (c *Client) ContentPackDeployPlaybooks(ctx context.Context, id string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "contentHub/contentPacks/"+id+":deployPlaybooks", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ContentPackDeployConnectors deploys connector instances from a content pack.
func (c *Client) ContentPackDeployConnectors(ctx context.Context, id string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "contentHub/contentPacks/"+id+":deployConnectorInstances", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ContentPackDeployTestCases deploys test cases from a content pack.
func (c *Client) ContentPackDeployTestCases(ctx context.Context, id string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "contentHub/contentPacks/"+id+":deployTestCases", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ContentPackDelete deletes a custom content pack.
func (c *Client) ContentPackDelete(ctx context.Context, id string) error {
	return c.t.V1Alpha(ctx, "DELETE", "contentHub/contentPacks/"+id, nil, nil)
}

// FetchCommercialDiff returns the diff between installed and marketplace version.
func (c *Client) FetchCommercialDiff(ctx context.Context, integrationID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", "marketplaceIntegrations/"+integrationID+":fetchCommercialDiff", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListFeaturedPlaybooks lists Google-curated featured playbooks.
func (c *Client) ListFeaturedPlaybooks(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", "contentHub/featuredContentPlaybooks", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// InstallFeaturedPlaybook installs a featured playbook by its uid. body
// carries the install options — the API requires {"environments": [...]}.
func (c *Client) InstallFeaturedPlaybook(ctx context.Context, uid string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "contentHub/featuredContentPlaybooks/"+uid+":install", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
