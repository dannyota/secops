// config_surfaces.go — MODERN v1alpha read coverage for the SOAR config surfaces
// that currently run on the legacy reconcile engine (environments, socRoles,
// customLists, case stage/close/tag definitions). These all answer on the SOAR
// host (see soar/v1alpha_probe_test.go); exposing modern reads lets the CLI /
// reconcile engine prefer v1alpha with a legacy fallback (Wave 13). Shapes vary
// per surface, so records are returned raw for the caller to parse.

package soar

import (
	"context"
	"encoding/json"

	"danny.vn/secops/soar/internal/transport"
)

// listCollection GETs a v1alpha collection by resource name, returning each record
// raw. The list envelope keys its array under the resource name (the v1alpha
// convention, e.g. {"socRoles":[…],"nextPageToken":…}); pagination is followed.
func (c *Client) listCollection(ctx context.Context, resource string) ([]json.RawMessage, error) {
	var all []json.RawMessage
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var env map[string]json.RawMessage
		if err := c.t.V1Alpha(ctx, "GET", resource, nil, &env, pageTokenOpt(token)); err != nil {
			return "", err
		}
		if items, ok := env[resource]; ok && len(items) > 0 {
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
