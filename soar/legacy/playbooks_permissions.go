// LEGACY tier: the Siemplify external API (/api/external/v1) Playbooks surface —
// playbook permission operations (read options, set, delete).
//
// Reads return json.RawMessage and writes take a freeform body; all methods
// speak the AppKey-authenticated external API via c.t.External.
package legacy

import (
	"context"
	"net/http"
	"net/url"
)

// PlaybookXSetPermissions sets the permissions for a playbook. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) PlaybookXSetPermissions(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/permissions", body)
}

// PlaybookXGetPermissionsOptions returns the available playbook permission
// options. body is the freeform legacy payload.
func (c *Client) PlaybookXGetPermissionsOptions(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/playbooks/permissions/options", body)
}

// PlaybookXDeletePermissions deletes the permissions for a playbook by its
// identifier, scoped to the given workflow original identifier. LIVE MUTATION.
func (c *Client) PlaybookXDeletePermissions(ctx context.Context, identifier, workflowOriginalIdentifier string) (RawJSON, error) {
	path := "/playbooks/permissions/" + url.PathEscape(identifier)
	if workflowOriginalIdentifier != "" {
		q := url.Values{"workflowOriginalIdentifier": {workflowOriginalIdentifier}}
		path += "?" + q.Encode()
	}
	return c.externalDo(ctx, http.MethodDelete, path, nil)
}
