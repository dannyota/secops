// LEGACY tier: the Siemplify external API (/api/external/v1) Settings surface.
//
// These methods cover the user/account-administration Settings operations:
// collaborator requests, license agreement, user profiles/images, audit CSV
// exports, and analyst lookup.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). All methods speak the AppKey-authenticated external
// API via c.t.External. Method names are prefixed "SettingX" to stay globally
// unique across the single *Client shared by every file in this package.
package legacy

import (
	"context"
	"net/http"
	"strconv"
)

// SettingXAddCollaboratorRequest creates a new collaborator request. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) SettingXAddCollaboratorRequest(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/AddCollaboratorRequest", body)
}

// SettingXUpdateCollaboratorRequest updates an existing collaborator request.
// body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) SettingXUpdateCollaboratorRequest(ctx context.Context, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/settings/UpdateCollaboratorRequest", body)
}

// SettingXDeleteCollaboratorRequest deletes a collaborator request by its
// numeric id. LIVE MUTATION; this cannot be undone.
func (c *Client) SettingXDeleteCollaboratorRequest(ctx context.Context, id int) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/settings/DeleteCollaboratorRequest/"+strconv.Itoa(id), nil)
}

// SettingXGetAllCollaboratorRequests returns every collaborator request.
func (c *Client) SettingXGetAllCollaboratorRequests(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetAllCollaboratorRequests")
}

// SettingXGetCollaboratorRequestsByUser returns collaborator requests scoped to
// the calling user.
func (c *Client) SettingXGetCollaboratorRequestsByUser(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetCollaboratorRequestsByUser")
}

// SettingXAddLicenseAgreementSignature records the calling user's signature of
// the license agreement. body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) SettingXAddLicenseAgreementSignature(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/AddLicenseAgreementSignature", body)
}

// SettingXGetLatestLicenseAgreement returns the most recent license agreement.
func (c *Client) SettingXGetLatestLicenseAgreement(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/settings/GetLatestLicenseAgreement")
}

// SettingXAddOrUpdateUserProfile creates or updates a user profile. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) SettingXAddOrUpdateUserProfile(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/AddOrUpdateUserProfile", body)
}

// SettingXGetUserProfiles returns user profiles matching the request. body is the
// freeform legacy filter payload.
func (c *Client) SettingXGetUserProfiles(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/GetUserProfiles", body)
}

// SettingXGetUserProfilesByEnvironments returns user profiles filtered by
// environment. body is the freeform legacy filter payload.
func (c *Client) SettingXGetUserProfilesByEnvironments(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/GetUserProfilesByEnvironments", body)
}

// SettingXGetUserProfileCards returns summary cards for user profiles. body is
// the freeform legacy filter payload.
func (c *Client) SettingXGetUserProfileCards(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/GetUserProfileCards", body)
}

// SettingXGetUserImage returns a user's avatar image. body selects the user
// (freeform legacy payload).
func (c *Client) SettingXGetUserImage(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/GetUserImage", body)
}

// SettingXUpdateUserImage sets a user's avatar image. body is the freeform legacy
// payload. LIVE MUTATION.
func (c *Client) SettingXUpdateUserImage(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/UpdateUserImage", body)
}

// SettingXGetAnalysts returns the analysts matching the request. body is the
// freeform legacy filter payload.
func (c *Client) SettingXGetAnalysts(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/GetAnalysts", body)
}

// SettingXExportAuditLastWeekAsCsv exports the last week of audit records as CSV.
// body is the freeform legacy request payload. LIVE MUTATION.
func (c *Client) SettingXExportAuditLastWeekAsCsv(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/ExportAuditLastWeekAsCsv", body)
}

// SettingXExportAuditLastWeekAsCsvV2 exports the last week of audit records as CSV
// (v2 endpoint). body is the freeform legacy request payload. LIVE MUTATION.
func (c *Client) SettingXExportAuditLastWeekAsCsvV2(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/ExportAuditLastWeekAsCsvV2", body)
}

// SettingXUploadCustomActionResultJson uploads a custom action-result JSON
// definition. body is the freeform legacy payload. LIVE MUTATION.
func (c *Client) SettingXUploadCustomActionResultJson(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/settings/UploadCustomActionResultJson", body)
}
