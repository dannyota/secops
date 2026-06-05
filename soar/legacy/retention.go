// LEGACY tier: the Siemplify external API (/api/external/v1) Retention surface.
//
// Data-retention operations purge aged data from the platform. They map to the
// manual ("run now") retention jobs the platform otherwise runs on a schedule:
// system-data retention, user-data retention, and bulk case deletion. Every
// operation here permanently destroys data, so each is a guarded live mutation.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so writes take
// a freeform body (the caller supplies/decodes only the fields it needs). All
// methods speak the AppKey-authenticated external API via c.t.External.
package legacy

import "context"

// RetentionDeleteCases bulk-deletes cases matching the supplied request. body is
// the freeform case-deletion request payload. LIVE MUTATION; permanently
// destroys the matched cases and cannot be undone.
func (c *Client) RetentionDeleteCases(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/retention/delete-cases", body)
}

// RetentionRunSystem performs a manual system-data retention pass. body is the
// freeform retention request payload. LIVE MUTATION; permanently purges aged
// system data and cannot be undone.
func (c *Client) RetentionRunSystem(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/retention/system", body)
}

// RetentionRunUser performs a manual user-data retention pass. body is the
// freeform retention request payload. LIVE MUTATION; permanently purges aged
// user data and cannot be undone.
func (c *Client) RetentionRunUser(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/retention/user", body)
}
