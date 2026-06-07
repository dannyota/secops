// config_surfaces.go — MODERN v1alpha coverage for the SOAR config surfaces that
// also run on the legacy reconcile engine (environments, socRoles, customLists,
// case stage/close/tag definitions). These all answer on the SOAR host (see
// soar/v1alpha_probe_test.go). Reads let the CLI / reconcile engine prefer
// v1alpha with a legacy fallback (Wave 13); the create/get/update/delete writes
// below are live-validated (create→get→delete on customLists/socRoles/
// caseTagDefinitions; environments create is reachable but license-capped) — the
// v1alpha write endpoints do NOT 500 here. Shapes vary per surface, so records
// are passed and returned raw for the caller to parse.

package soar

import (
	"context"
	"encoding/json"

	"danny.vn/secops/soar/internal/transport"
)

// listCollection GETs a v1alpha collection by resource name, returning each record
// raw. The list envelope keys its array under the resource name (the v1alpha
// convention, e.g. {"socRoles":[…],"nextPageToken":…}), but some surfaces report
// the generic "items" key instead, so both are accepted; pagination is followed.
func (c *Client) listCollection(ctx context.Context, resource string) ([]json.RawMessage, error) {
	var all []json.RawMessage
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var env map[string]json.RawMessage
		if err := c.t.V1Alpha(ctx, "GET", resource, nil, &env, pageTokenOpt(token)); err != nil {
			return "", err
		}
		items := env[resource]
		if len(items) == 0 {
			items = env["items"] // generic v1alpha collection-key fallback
		}
		if len(items) > 0 {
			var page []json.RawMessage
			if err := json.Unmarshal(items, &page); err != nil {
				return "", err
			}
			all = append(all, page...)
		}
		next := ""
		if tok, ok := env["nextPageToken"]; ok {
			_ = json.Unmarshal(tok, &next)
		}
		return next, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// ListEnvironments returns the SOAR environments (modern v1alpha). Read-only.
func (c *Client) ListEnvironments(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, "environments")
}

// ListSocRoles returns the SOC roles (modern v1alpha). Read-only.
func (c *Client) ListSocRoles(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, "socRoles")
}

// ListCustomLists returns the custom (tracking/standard/block) lists (modern
// v1alpha). Read-only.
func (c *Client) ListCustomLists(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, "customLists")
}

// ListCaseStageDefinitions returns the case-stage definitions (modern v1alpha).
// Read-only.
func (c *Client) ListCaseStageDefinitions(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, "caseStageDefinitions")
}

// ListCaseCloseDefinitions returns the case-close (root-cause) definitions (modern
// v1alpha). Read-only.
func (c *Client) ListCaseCloseDefinitions(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, "caseCloseDefinitions")
}

// ListCaseTagDefinitions returns the case-tag definitions (modern v1alpha).
// Read-only.
func (c *Client) ListCaseTagDefinitions(ctx context.Context) ([]json.RawMessage, error) {
	return c.listCollection(ctx, "caseTagDefinitions")
}

// Writes (modern v1alpha) -----------------------------------------------------
//
// These config collections are uniform Google-style v1alpha collections served on
// the SOAR host, so the create/get/update/delete primitives are generic and the
// per-surface wrappers below just name the resource. Bodies are the caller's
// struct/map (shapes vary per surface — see the v1alpha REST reference); any
// repeated field must be sent as [] not null (the server NPEs to HTTP 500 on a
// null collection). The created/updated record is returned raw. LIVE MUTATIONS.
//
// create→get→delete is live-validated for customLists, socRoles, and
// caseTagDefinitions; environments create is reachable but may be license-capped.

// createInCollection POSTs a new item into a v1alpha collection.
func (c *Client) createInCollection(ctx context.Context, resource string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", resource, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// getInCollection GETs one item by id from a v1alpha collection.
func (c *Client) getInCollection(ctx context.Context, resource, id string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", resource+"/"+id, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// updateInCollection PATCHes an item; pass updateMask to scope the write.
func (c *Client) updateInCollection(ctx context.Context, resource, id string, body any, updateMask ...string) (json.RawMessage, error) {
	var opts []transport.Option
	if len(updateMask) > 0 {
		opts = append(opts, transport.UpdateMask(updateMask...))
	}
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "PATCH", resource+"/"+id, body, &out, opts...); err != nil {
		return nil, err
	}
	return out, nil
}

// deleteInCollection DELETEs an item by id.
func (c *Client) deleteInCollection(ctx context.Context, resource, id string) error {
	return c.t.V1Alpha(ctx, "DELETE", resource+"/"+id, nil, nil)
}

// Environment writes (modern v1alpha). LIVE MUTATIONS.
func (c *Client) CreateEnvironment(ctx context.Context, body any) (json.RawMessage, error) {
	return c.createInCollection(ctx, "environments", body)
}

func (c *Client) GetEnvironment(ctx context.Context, id string) (json.RawMessage, error) {
	return c.getInCollection(ctx, "environments", id)
}

func (c *Client) UpdateEnvironment(ctx context.Context, id string, body any, updateMask ...string) (json.RawMessage, error) {
	return c.updateInCollection(ctx, "environments", id, body, updateMask...)
}

func (c *Client) DeleteEnvironment(ctx context.Context, id string) error {
	return c.deleteInCollection(ctx, "environments", id)
}

// SocRole writes (modern v1alpha). LIVE MUTATIONS.
func (c *Client) CreateSocRole(ctx context.Context, body any) (json.RawMessage, error) {
	return c.createInCollection(ctx, "socRoles", body)
}

func (c *Client) GetSocRole(ctx context.Context, id string) (json.RawMessage, error) {
	return c.getInCollection(ctx, "socRoles", id)
}

func (c *Client) UpdateSocRole(ctx context.Context, id string, body any, updateMask ...string) (json.RawMessage, error) {
	return c.updateInCollection(ctx, "socRoles", id, body, updateMask...)
}

func (c *Client) DeleteSocRole(ctx context.Context, id string) error {
	return c.deleteInCollection(ctx, "socRoles", id)
}

// CustomList writes (modern v1alpha). LIVE MUTATIONS.
func (c *Client) CreateCustomList(ctx context.Context, body any) (json.RawMessage, error) {
	return c.createInCollection(ctx, "customLists", body)
}

func (c *Client) GetCustomList(ctx context.Context, id string) (json.RawMessage, error) {
	return c.getInCollection(ctx, "customLists", id)
}

func (c *Client) UpdateCustomList(ctx context.Context, id string, body any, updateMask ...string) (json.RawMessage, error) {
	return c.updateInCollection(ctx, "customLists", id, body, updateMask...)
}

func (c *Client) DeleteCustomList(ctx context.Context, id string) error {
	return c.deleteInCollection(ctx, "customLists", id)
}

// CaseStageDefinition writes (modern v1alpha). LIVE MUTATIONS.
func (c *Client) CreateCaseStageDefinition(ctx context.Context, body any) (json.RawMessage, error) {
	return c.createInCollection(ctx, "caseStageDefinitions", body)
}

func (c *Client) DeleteCaseStageDefinition(ctx context.Context, id string) error {
	return c.deleteInCollection(ctx, "caseStageDefinitions", id)
}

// CaseCloseDefinition writes (modern v1alpha). LIVE MUTATIONS.
func (c *Client) CreateCaseCloseDefinition(ctx context.Context, body any) (json.RawMessage, error) {
	return c.createInCollection(ctx, "caseCloseDefinitions", body)
}

func (c *Client) DeleteCaseCloseDefinition(ctx context.Context, id string) error {
	return c.deleteInCollection(ctx, "caseCloseDefinitions", id)
}

// CaseTagDefinition writes (modern v1alpha). LIVE MUTATIONS.
func (c *Client) CreateCaseTagDefinition(ctx context.Context, body any) (json.RawMessage, error) {
	return c.createInCollection(ctx, "caseTagDefinitions", body)
}

func (c *Client) GetCaseTagDefinition(ctx context.Context, id string) (json.RawMessage, error) {
	return c.getInCollection(ctx, "caseTagDefinitions", id)
}

func (c *Client) DeleteCaseTagDefinition(ctx context.Context, id string) error {
	return c.deleteInCollection(ctx, "caseTagDefinitions", id)
}
