// Job revision history for the SOAR v1alpha API.
//
// A revision is a point-in-time snapshot of a job definition, stored under
// integrations/{integration}/jobs/{job}/revisions/{revision}.

package soar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"danny.vn/secops/soar/internal/transport"
)

// JobRevision is a point-in-time snapshot of a job definition.
type JobRevision struct {
	Name       string          `json:"name,omitempty"`
	Snapshot   json.RawMessage `json:"snapshot,omitempty"`
	CreateTime json.Number     `json:"createTime,omitempty"`
	Comment    string          `json:"comment,omitempty"`
	Author     string          `json:"author,omitempty"`
	Raw        json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and preserves the full payload in Raw.
func (r *JobRevision) UnmarshalJSON(data []byte) error {
	type alias JobRevision
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = JobRevision(a)
	r.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ListJobRevisions returns all revisions for a job definition.
func (c *Client) ListJobRevisions(ctx context.Context, integration, jobID string) ([]JobRevision, error) {
	base := fmt.Sprintf("integrations/%s/jobs/%s/revisions", integration, jobID)
	var all []JobRevision
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		var resp struct {
			Revisions     []json.RawMessage `json:"revisions"`
			Items         []json.RawMessage `json:"items"`
			NextPageToken string            `json:"nextPageToken"`
		}
		if err := c.t.V1Alpha(ctx, http.MethodGet, base, nil, &resp, pageTokenOpt(token)); err != nil {
			return "", err
		}
		batch := resp.Revisions
		if len(batch) == 0 {
			batch = resp.Items
		}
		for _, item := range batch {
			var rev JobRevision
			if err := json.Unmarshal(item, &rev); err != nil {
				return "", fmt.Errorf("soar: decode job revision: %w", err)
			}
			all = append(all, rev)
		}
		return resp.NextPageToken, nil
	})
	return all, err
}

// CreateJobRevision creates a new revision snapshot for a job definition.
// body is typically {"job": <currentJobDef>, "comment": "..."}.
// LIVE MUTATION.
func (c *Client) CreateJobRevision(ctx context.Context, integration, jobID string, body any) (*JobRevision, error) {
	res := fmt.Sprintf("integrations/%s/jobs/%s/revisions", integration, jobID)
	var out JobRevision
	if err := c.t.V1Alpha(ctx, http.MethodPost, res, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteJobRevision deletes a job revision. LIVE MUTATION.
func (c *Client) DeleteJobRevision(ctx context.Context, integration, jobID, revisionID string) error {
	res := fmt.Sprintf("integrations/%s/jobs/%s/revisions/%s",
		integration, jobID, revisionID)
	return c.t.V1Alpha(ctx, http.MethodDelete, res, nil, nil)
}

// RollbackJobRevision restores a job definition to a previous revision's
// snapshot. Returns the raw server response. LIVE MUTATION.
func (c *Client) RollbackJobRevision(ctx context.Context, integration, jobID, revisionID string) (json.RawMessage, error) {
	res := fmt.Sprintf("integrations/%s/jobs/%s/revisions/%s:rollback",
		integration, jobID, revisionID)
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, http.MethodPost, res, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
