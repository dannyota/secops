// MODERN. Job instance configuration (v1alpha) for the SOAR API.
//
// A job instance is a configured, schedulable run of an integration job. The
// resource path is
// integrations/{integration}/jobs/{job}/jobInstances/{instance}.

package soar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"danny.vn/secops/soar/internal/transport"
)

// JobInstance is a configured instance of an integration job.
//
// SOAR returns job parameters as a string-keyed bag of stringified values, so
// Parameters is map[string]string. Raw carries the full server payload for
// fields not modeled above (e.g. nested scheduling or run-state structures).
type JobInstance struct {
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	// No omitempty on the mutable scalars: a sparse PATCH that sets
	// enabled=false, cronSchedule="" (switch to interval), or intervalSeconds=0
	// must serialize the zero value or the masked update silently no-ops.
	Enabled         bool              `json:"enabled"`
	CronSchedule    string            `json:"cronSchedule"`
	IntervalSeconds int               `json:"intervalSeconds"`
	Parameters      map[string]string `json:"parameters,omitempty"`
	Raw             json.RawMessage   `json:"-"`
}

// jobInstancePath builds the v1alpha resource path for a job instance.
func jobInstancePath(integration, jobID, instanceID string) string {
	return fmt.Sprintf("integrations/%s/jobs/%s/jobInstances/%s",
		integration, jobID, instanceID)
}

// ListJobInstances returns every configured instance of an integration job.
func (c *Client) ListJobInstances(ctx context.Context, integration, jobID string) ([]JobInstance, error) {
	base := fmt.Sprintf("integrations/%s/jobs/%s/jobInstances", integration, jobID)
	var all []JobInstance
	err := transport.PaginateV1Alpha(50, func(token string) (string, error) {
		q := url.Values{}
		if token != "" {
			q.Set("pageToken", token)
		}
		// Accept either the resource-named collection key or the generic "items"
		// (the v1alpha LIST shape varies); decode each item to preserve Raw.
		var resp struct {
			JobInstances  []json.RawMessage `json:"jobInstances"`
			Items         []json.RawMessage `json:"items"`
			NextPageToken string            `json:"nextPageToken"`
		}
		if err := c.t.V1Alpha(ctx, http.MethodGet, base, nil, &resp, transport.Query(q)); err != nil {
			return "", err
		}
		batch := resp.JobInstances
		if len(batch) == 0 {
			batch = resp.Items
		}
		for _, item := range batch {
			ji, derr := decodeJobInstance(item)
			if derr != nil {
				return "", derr
			}
			all = append(all, *ji)
		}
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetJobInstance fetches a single job instance's configuration.
func (c *Client) GetJobInstance(ctx context.Context, integration, jobID, instanceID string) (*JobInstance, error) {
	res := jobInstancePath(integration, jobID, instanceID)

	// DEVIATION: decode once into the typed struct and again into Raw so callers
	// keep both the modeled fields and any unmodeled server payload.
	var raw json.RawMessage
	if err := c.t.V1Alpha(ctx, http.MethodGet, res, nil, &raw); err != nil {
		return nil, err
	}
	return decodeJobInstance(raw)
}

// UpdateJobInstance applies a sparse PATCH to a job instance. updateMask names
// the fields to change (e.g. "enabled", "cronSchedule"); body is any
// JSON-marshalable payload (typically a *JobInstance or a partial map).
//
// DEVIATION: like connectors, secret parameters read back masked ("***…") from
// GetJobInstance. The server treats the masked sentinel as "unchanged", so a
// round-trip get→patch is safe: pass the masked value back verbatim to leave the
// secret intact. Only send a real cleartext value to genuinely rotate one — and
// never log or commit it.
func (c *Client) UpdateJobInstance(ctx context.Context, integration, jobID, instanceID string, body any, updateMask ...string) (*JobInstance, error) {
	res := jobInstancePath(integration, jobID, instanceID)

	var opts []transport.Option
	if len(updateMask) > 0 {
		opts = append(opts, transport.UpdateMask(updateMask...))
	}

	var raw json.RawMessage
	if err := c.t.V1Alpha(ctx, http.MethodPatch, res, body, &raw, opts...); err != nil {
		return nil, err
	}
	return decodeJobInstance(raw)
}

// decodeJobInstance unmarshals a server payload into a JobInstance, retaining
// the original JSON in Raw.
func decodeJobInstance(raw json.RawMessage) (*JobInstance, error) {
	if len(raw) == 0 {
		return &JobInstance{}, nil
	}
	var ji JobInstance
	if err := json.Unmarshal(raw, &ji); err != nil {
		return nil, fmt.Errorf("soar: decode job instance: %w", err)
	}
	ji.Raw = raw
	return &ji, nil
}
