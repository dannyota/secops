// LEGACY tier: Siemplify external API (/api/external/v1) single-case ACTION
// surface — the bread-and-butter SOAR case automation.
//
// Every method here is a LIVE MUTATION against production cases. body is the
// freeform legacy payload and carries the SOAR INTEGER case id (see the dual
// case-id gotcha in cases.go). Bulk variants live in cases_bulk.go; comment/tag/
// priority basics are in cases.go.
package legacy

import "context"

// CreateCase creates a case. body is the freeform case payload.
func (c *Client) CreateCase(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/CreateCase", body)
}

// CreateManualCase creates a manual (analyst-authored) case.
func (c *Client) CreateManualCase(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/CreateManualCase", body)
}

// CloseCase closes a case.
func (c *Client) CloseCase(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/CloseCase", body)
}

// CloseAlert closes a single alert within a case.
func (c *Client) CloseAlert(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/CloseAlert", body)
}

// AssignUserToCase assigns a case to a user.
func (c *Client) AssignUserToCase(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/AssignUserToCase", body)
}

// RenameCase renames a case.
func (c *Client) RenameCase(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/RenameCase", body)
}

// ChangeCaseDescription updates a case's description.
func (c *Client) ChangeCaseDescription(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/ChangeCaseDescription", body)
}

// ChangeCaseStage moves a case to a different stage.
func (c *Client) ChangeCaseStage(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/ChangeCaseStage", body)
}

// ChangeCaseImportanceStatus updates a case's importance/status.
func (c *Client) ChangeCaseImportanceStatus(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/ChangeCaseImportanceStatus", body)
}

// RemoveCaseTag removes a tag from a case.
func (c *Client) RemoveCaseTag(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/RemoveCaseTag", body)
}

// AddEvidence attaches evidence to a case.
func (c *Client) AddEvidence(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/AddEvidence", body)
}

// CreateCaseInsight adds an insight to a case.
func (c *Client) CreateCaseInsight(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/CreateCaseInsight", body)
}

// CreateCaseEntity adds an entity to a case.
func (c *Client) CreateCaseEntity(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/CreateCaseEntity", body)
}

// ExecuteManualAction runs an integration action against a case manually.
func (c *Client) ExecuteManualAction(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/ExecuteManualAction", body)
}

// UpdateAlertPriority changes a single alert's priority.
func (c *Client) UpdateAlertPriority(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/UpdateAlertPriority", body)
}

// RaiseIncident marks a case as an incident.
func (c *Client) RaiseIncident(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/RaiseIncident", body)
}

// UnraiseIncident clears the incident flag on a case.
func (c *Client) UnraiseIncident(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/UnraiseIncident", body)
}

// SkipAlert skips an alert.
func (c *Client) SkipAlert(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/Skip", body)
}
