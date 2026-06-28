// integrations_catalog.go — action, transformer, and logical-operator catalogs.
//
// These are the wildcard-collection read-only catalogs that list every action /
// transformer / logical-operator across ALL integrations — the playbook
// designer's palette surfaces.

package soar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"danny.vn/secops/soar/internal/transport"
)

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
