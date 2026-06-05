// LEGACY tier: the Siemplify external API (/api/external/v1) Approval Links
// surface.
//
// Approval links are the one-click action URLs SOAR embeds in notifications
// (email, chat) so a recipient can approve or reject a pending playbook step
// without signing in to the platform. Applying a link records that decision and
// resumes the waiting playbook. The single external endpoint here takes the
// freeform legacy payload that the link encodes; the caller supplies it verbatim.
//
// As elsewhere in this package the method is a thin wrapper over c.t.External:
// the write takes a freeform body and returns json.RawMessage (the caller
// decodes only what it needs).
package legacy

import "context"

// ApprovalLinkApply applies an approval-link decision (approve/reject) for a
// pending playbook step, resuming the waiting workflow. body is the freeform
// legacy approval-link payload. LIVE MUTATION.
func (c *Client) ApprovalLinkApply(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/approval-link/Apply", body)
}
