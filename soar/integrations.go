// integrations.go — MODERN (v1alpha) integration/connector/job discovery.
//
// SOAR ships integrations (e.g. the VirusTotal or Slack packs); each bundles
// connector definitions (ingestion) and job definitions (scheduled work). These
// list endpoints are read-only catalogs used to drive configuration tooling.

package soar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"danny.vn/secops/soar/internal/transport"
)

// Integration is a SOAR integration (an IDE/marketplace pack).
//
// GOTCHA: each marketplace pack lists twice. The per-tenant INSTALLED copy has an
// Identifier of "<base>__<uuid>" (e.g. "VirusTotalV3__1a2b3c…") with ProdIdentifier
// set to the base ("VirusTotalV3"); the bare "<base>" entry is the catalog/base
// definition. Address either by Identifier for nested lists (ListConnectors etc.);
// Name is the full "projects/…/integrations/<identifier>" resource path.
type Integration struct {
	Name            string          `json:"name"`
	Identifier      string          `json:"identifier"`
	DisplayName     string          `json:"displayName"`
	LatestVersion   string          `json:"latestVersion"`
	UpdateAvailable bool            `json:"updateAvailable"`
	Custom          bool            `json:"custom"`               // custom (deletable) vs commercial pack
	Certified       bool            `json:"certified"`            // Google-certified
	Internal        bool            `json:"internal"`             // platform-internal pack
	ProdIdentifier  string          `json:"productionIdentifier"` // base pack this install derives from
	Raw             json.RawMessage `json:"-"`                    // full server object, untrimmed
}

// IsDeletableIntegration reports whether this tenant may delete the WHOLE
// integration pack: only a hand-built custom integration (Custom) is deletable.
// Commercial packs — including the per-tenant installed copies of marketplace
// integrations, whose Identifier carries a "__<uuid>" suffix but whose Custom is
// false — are NOT deletable here (the server rejects them), which protects the
// working installed integrations. To remove a duplicated connector *definition*
// inside such a pack, delete that definition with DeleteConnectorDef instead.
func IsDeletableIntegration(i Integration) bool { return i.Custom }

// UnmarshalJSON decodes the typed fields while preserving the complete server
// object in Raw (the v1alpha integration payload carries far more than we type).
func (i *Integration) UnmarshalJSON(data []byte) error {
	type alias Integration // avoid recursion
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*i = Integration(a)
	i.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ConnectorDef is a connector definition within an integration (an ingestion
// source template, not a configured instance). ID is the numeric definition id
// used in the connector's resource path (e.g. ".../connectors/48").
type ConnectorDef struct {
	Name        string          `json:"name"`
	ID          json.Number     `json:"id"`     // numeric in the v1alpha payload
	Custom      bool            `json:"custom"` // custom (deletable) vs commercial connector definition
	Identifier  string          `json:"identifier"`
	DisplayName string          `json:"displayName"`
	Raw         json.RawMessage `json:"-"`
}

// PathID returns the segment that addresses this connector in a resource path:
// the numeric ID when present, else the last segment of Name, else Identifier.
func (c *ConnectorDef) PathID() string { return pathID(c.ID.String(), c.Name, c.Identifier) }

// UnmarshalJSON decodes the typed fields and keeps the full object in Raw.
func (c *ConnectorDef) UnmarshalJSON(data []byte) error {
	type alias ConnectorDef
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*c = ConnectorDef(a)
	c.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// JobDef is a job definition within an integration (a scheduled-task template).
// ID is the numeric definition id used in the job's resource path.
type JobDef struct {
	Name        string          `json:"name"`
	ID          json.Number     `json:"id"` // numeric in the v1alpha payload
	Identifier  string          `json:"identifier"`
	DisplayName string          `json:"displayName"`
	Raw         json.RawMessage `json:"-"`
}

// PathID returns the segment that addresses this job in a resource path.
func (j *JobDef) PathID() string { return pathID(j.ID.String(), j.Name, j.Identifier) }

// pathID picks the resource-path segment from the available identifiers: a
// numeric id when present, else the last "/"-segment of the resource name, else
// the identifier. SOAR connector/job definitions are addressed by a numeric id.
func pathID(id, name, identifier string) string {
	if id != "" {
		return id
	}
	if name != "" {
		if i := strings.LastIndex(name, "/"); i >= 0 {
			return name[i+1:]
		}
		return name
	}
	return identifier
}

// UnmarshalJSON decodes the typed fields and keeps the full object in Raw.
func (j *JobDef) UnmarshalJSON(data []byte) error {
	type alias JobDef
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*j = JobDef(a)
	j.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// listMaxPages caps {items,nextPageToken} pagination as a runaway-loop backstop,
// not a data limit: it is set high enough that any legitimate finite collection
// drains its token first, so hitting it (PaginateV1Alpha then errors) signals a
// server returning a perpetual token — where failing beats looping or silently
// truncating. Sized to cover the highest-volume surfaces (e.g. cases) at the
// server's default page size.
const listMaxPages = 1000

// integrationsList is the v1alpha LIST envelope for integrations.
type integrationsList struct {
	Items         []Integration `json:"integrations"`
	NextPageToken string        `json:"nextPageToken"`
}

// connectorsList is the v1alpha LIST envelope for connector definitions.
type connectorsList struct {
	Items         []ConnectorDef `json:"connectors"`
	NextPageToken string         `json:"nextPageToken"`
}

// jobsList is the v1alpha LIST envelope for job definitions.
type jobsList struct {
	Items         []JobDef `json:"jobs"`
	NextPageToken string   `json:"nextPageToken"`
}

// ListIntegrations returns every installed integration in the tenant.
func (c *Client) ListIntegrations(ctx context.Context) ([]Integration, error) {
	var all []Integration
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var page integrationsList
		if err := c.t.V1Alpha(ctx, "GET", "integrations", nil, &page, pageTokenOpt(token)); err != nil {
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

// GetIntegration fetches a single integration by its Identifier (the installed
// copy's "<base>__<uuid>" or the bare base — see the Integration gotcha).
func (c *Client) GetIntegration(ctx context.Context, identifier string) (*Integration, error) {
	var out Integration
	if err := c.t.V1Alpha(ctx, "GET", "integrations/"+identifier, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteIntegration deletes a whole custom integration pack by its addressable
// key (Identifier or a full resource path). Only genuinely CUSTOM packs
// (custom=true) are deletable; commercial/marketplace packs — including their
// per-tenant installed copies — are rejected by the server. To remove a single
// duplicated connector definition inside a pack, use DeleteConnectorDef instead.
// LIVE MUTATION.
func (c *Client) DeleteIntegration(ctx context.Context, name string) error {
	return c.t.V1Alpha(ctx, "DELETE", "integrations/"+resourceID(name), nil, nil)
}

// resourceID extracts the addressable id from a value that may be either a bare
// id or a full "projects/…/integrations/<id>" resource name (mirrors the
// wrapper's format_resource_id: take the last path segment of a project-scoped
// name, else return the value unchanged).
func resourceID(name string) string {
	if strings.HasPrefix(name, "projects/") {
		if i := strings.LastIndex(name, "/"); i >= 0 {
			return name[i+1:]
		}
	}
	return name
}

// ListConnectors returns the connector definitions of one integration. integration
// is the addressable key (Name/Identifier — see the Integration gotcha).
func (c *Client) ListConnectors(ctx context.Context, integration string) ([]ConnectorDef, error) {
	var all []ConnectorDef
	res := fmt.Sprintf("integrations/%s/connectors", integration)
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var page connectorsList
		if err := c.t.V1Alpha(ctx, "GET", res, nil, &page, pageTokenOpt(token)); err != nil {
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

// GetConnectorDef fetches one connector definition by its parent integration key
// and numeric connector id (the id from ListConnectors).
func (c *Client) GetConnectorDef(ctx context.Context, integration, connectorID string) (*ConnectorDef, error) {
	var out ConnectorDef
	res := fmt.Sprintf("integrations/%s/connectors/%s", integration, connectorID)
	if err := c.t.V1Alpha(ctx, "GET", res, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteConnectorDef deletes one CUSTOM connector definition (e.g. a duplicated
// "Copy of …" template) from an integration. integration is the addressable key
// and connectorID is the numeric id from ListConnectors. Commercial (custom=false)
// connector definitions cannot be deleted — the server rejects them, which also
// protects the working stock connector. LIVE MUTATION.
func (c *Client) DeleteConnectorDef(ctx context.Context, integration, connectorID string) error {
	res := fmt.Sprintf("integrations/%s/connectors/%s", integration, connectorID)
	return c.t.V1Alpha(ctx, "DELETE", res, nil, nil)
}

// ListJobs returns the job definitions of one integration. integration is the
// addressable key (Name/Identifier — see the Integration gotcha).
func (c *Client) ListJobs(ctx context.Context, integration string) ([]JobDef, error) {
	var all []JobDef
	res := fmt.Sprintf("integrations/%s/jobs", integration)
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var page jobsList
		if err := c.t.V1Alpha(ctx, "GET", res, nil, &page, pageTokenOpt(token)); err != nil {
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

// pageTokenOpt builds the transport option carrying a pagination token. On the
// first page (empty token) it yields an empty Query, a harmless no-op the
// transport folds into its query map. DEVIATION: the transport exposes
// pagination via the generic Query option rather than a dedicated page-token
// knob, and transport.Option wraps an unexported type so a true no-op can't be
// constructed here — an empty url.Values is the clean equivalent. Shared across
// the package's v1alpha listers.
func pageTokenOpt(token string) transport.Option {
	if token == "" {
		return transport.Query(url.Values{})
	}
	return transport.Query(url.Values{"pageToken": {token}})
}

// ActionDef is an action definition listed by the wildcard actions catalog —
// one entry per action across ALL integrations (the playbook designer's action
// palette). ID is the numeric definition id, also embedded in the resource
// Name ("…/integrations/<key>/actions/<id>"); it is the id the playbook-usage
// reverse index keys on.
type ActionDef struct {
	Name        string          `json:"name"`
	ID          json.Number     `json:"id"`
	Integration string          `json:"integration"`
	DisplayName string          `json:"displayName"`
	Description string          `json:"description"`
	Enabled     bool            `json:"enabled"`
	Async       bool            `json:"async"`
	Custom      bool            `json:"custom"`
	Raw         json.RawMessage `json:"-"`
}

// PathID returns the segment that addresses this action in a resource path.
func (a *ActionDef) PathID() string { return pathID(a.ID.String(), a.Name, a.DisplayName) }

// UnmarshalJSON decodes the typed fields and keeps the full object in Raw.
func (a *ActionDef) UnmarshalJSON(data []byte) error {
	type alias ActionDef
	var x alias
	if err := json.Unmarshal(data, &x); err != nil {
		return err
	}
	*a = ActionDef(x)
	a.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// FlowFunction is a Flow utility definition from the wildcard catalogs: a
// transformer (a value-shaping function usable in playbook expressions) or a
// logical operator (a condition predicate). Both live under an integration
// (built-ins under "Core Functions") and carry the same numeric-id addressing
// as actions.
type FlowFunction struct {
	Name           string          `json:"name"`
	ID             json.Number     `json:"id"`
	Integration    string          `json:"integration"`
	DisplayName    string          `json:"displayName"`
	Description    string          `json:"description"`
	Enabled        bool            `json:"enabled"`
	Custom         bool            `json:"custom"`
	Type           string          `json:"type"` // e.g. "BuiltIn"
	ExpectedInput  string          `json:"expectedInput"`
	ExpectedOutput string          `json:"expectedOutput"`
	UsageExample   string          `json:"usageExample"`
	Raw            json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and keeps the full object in Raw.
func (f *FlowFunction) UnmarshalJSON(data []byte) error {
	type alias FlowFunction
	var x alias
	if err := json.Unmarshal(data, &x); err != nil {
		return err
	}
	*f = FlowFunction(x)
	f.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// actionsList is the v1alpha LIST envelope for action definitions.
type actionsList struct {
	Items         []ActionDef `json:"actions"`
	NextPageToken string      `json:"nextPageToken"`
}

// actionCatalogFields is the field mask the action catalogs request — the
// summary columns only, never the Python script bodies.
const actionCatalogFields = "actions.id,actions.name,actions.displayName,actions.integration,actions.description,actions.enabled,actions.async,actions.custom,nextPageToken"

// ListAllActions returns every action definition across ALL integrations via
// the `-` wildcard collection — the designer's action palette in one call.
func (c *Client) ListAllActions(ctx context.Context) ([]ActionDef, error) {
	return c.listActions(ctx, "-", actionCatalogFields)
}

// ListActions returns the action definitions of one integration. integration
// is the addressable key (Name/Identifier — see the Integration gotcha). It
// requests the summary columns only (no parameters or script bodies); use
// GetActionDef per action when the parameter schema is needed.
func (c *Client) ListActions(ctx context.Context, integration string) ([]ActionDef, error) {
	return c.listActions(ctx, integration, actionCatalogFields)
}

// GetActionDef returns ONE action definition's full object — including its
// `parameters` schema (type/mandatory/defaultValue/displayName/description/
// optionalValues), which the LIST collection never returns regardless of field
// mask (a server quirk: a parameters subtree mask yields empty objects, an
// explicit-leaf mask omits parameters, and the list omits them even unmasked).
// integration is the addressable key; actionID is the numeric definition id. The
// Python script body rides along but is not parsed. Read-only.
func (c *Client) GetActionDef(ctx context.Context, integration, actionID string) (json.RawMessage, error) {
	var out json.RawMessage
	res := fmt.Sprintf("integrations/%s/actions/%s", integration, actionID)
	if err := c.t.V1Alpha(ctx, "GET", res, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) listActions(ctx context.Context, integration, fields string) ([]ActionDef, error) {
	var all []ActionDef
	res := fmt.Sprintf("integrations/%s/actions", integration)
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		q := url.Values{"fields": {fields}, "pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var page actionsList
		if err := c.t.V1Alpha(ctx, "GET", res, nil, &page, transport.Query(q)); err != nil {
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

// transformersList is the v1alpha LIST envelope for transformers.
type transformersList struct {
	Items         []FlowFunction `json:"transformers"`
	NextPageToken string         `json:"nextPageToken"`
}

// logicalOperatorsList is the v1alpha LIST envelope for logical operators.
// The server keys this collection snake_case (`logical_operators`) even under
// `format=camel`, so both spellings are decoded.
type logicalOperatorsList struct {
	Items         []FlowFunction `json:"logical_operators"`
	ItemsCamel    []FlowFunction `json:"logicalOperators"`
	NextPageToken string         `json:"nextPageToken"`
	NextSnake     string         `json:"next_page_token"`
}

// ListTransformers returns every transformer (Flow value function) across all
// integrations via the `-` wildcard collection.
func (c *Client) ListTransformers(ctx context.Context) ([]FlowFunction, error) {
	var all []FlowFunction
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var page transformersList
		if err := c.t.V1Alpha(ctx, "GET", "integrations/-/transformers", nil, &page, pageTokenOpt(token)); err != nil {
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

// ListLogicalOperators returns every logical operator (Flow condition
// predicate) across all integrations via the `-` wildcard collection.
func (c *Client) ListLogicalOperators(ctx context.Context) ([]FlowFunction, error) {
	var all []FlowFunction
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var page logicalOperatorsList
		if err := c.t.V1Alpha(ctx, "GET", "integrations/-/logicalOperators", nil, &page, pageTokenOpt(token)); err != nil {
			return "", err
		}
		all = append(all, page.Items...)
		if len(page.Items) == 0 {
			all = append(all, page.ItemsCamel...)
		}
		next := page.NextPageToken
		if next == "" {
			next = page.NextSnake
		}
		return next, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}
