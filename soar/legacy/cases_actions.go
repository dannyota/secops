// LEGACY tier: Siemplify external API (/api/external/v1) single-case ACTION
// surface — the bread-and-butter SOAR case automation.
//
// Every method here is a LIVE MUTATION against production cases. body is the
// freeform legacy payload and carries the SOAR INTEGER case id (see the dual
// case-id gotcha in cases.go). Bulk variants live in cases_bulk.go; comment/tag/
// priority basics are in cases.go.
package legacy

import "context"

// Typed request bodies for the per-alert verbs. The wire shapes differ subtly
// per verb (close/move key the case as sourceCaseId; priority/reopen as caseId),
// so the structs pin them once for every caller — the CLI and the live write
// smoke share these instead of hand-building maps that could drift.

// CloseAlertRequest closes one alert within a case (the case stays open).
// Reason takes the PascalCase wire token (Malicious | NotMalicious |
// Maintenance | Inconclusive — alerts take no Unknown); Usefulness is the
// optional None | NotUseful | Useful stat.
type CloseAlertRequest struct {
	SourceCaseID    int    `json:"sourceCaseId"`
	AlertIdentifier string `json:"alertIdentifier"`
	Reason          string `json:"reason"`
	RootCause       string `json:"rootCause"`
	Comment         string `json:"comment"`
	Usefulness      string `json:"usefulness,omitempty"`
}

// UpdateAlertPriorityRequest re-prioritizes one alert within its case. The
// request carries the alert's name and current priority alongside the target.
type UpdateAlertPriorityRequest struct {
	CaseID           int          `json:"caseId"`
	AlertIdentifier  string       `json:"alertIdentifier"`
	AlertName        string       `json:"alertName"`
	PreviousPriority CasePriority `json:"previousPriority"`
	Priority         CasePriority `json:"priority"`
}

// MoveAlertRequest moves one alert out of a case: into DestinationCaseID, or
// into a brand-new case when DestinationCaseID is zero (omitted on the wire).
type MoveAlertRequest struct {
	AlertIdentifier   string `json:"alertIdentifier"`
	SourceCaseID      int    `json:"sourceCaseId"`
	DestinationCaseID int    `json:"destinationCaseId,omitempty"`
}

// ReopenAlertRequest reopens one closed alert within a case.
type ReopenAlertRequest struct {
	CaseID          int    `json:"caseId"`
	AlertIdentifier string `json:"alertIdentifier"`
}

// CreateCase creates a case. body is the freeform case payload.
func (c *Client) CreateCase(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/cases/CreateCase", body)
}

// CreateManualCase is typed (it returns the new case id and enforces the
// non-null collection contract); see cases.go.

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
