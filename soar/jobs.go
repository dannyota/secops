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
type JobInstance struct {
	Name              string      `json:"name,omitempty"`
	DisplayName       string      `json:"displayName,omitempty"`
	ID                json.Number `json:"id,omitempty"`
	Job               string      `json:"job,omitempty"`
	Integration       string      `json:"integration,omitempty"`
	Description       string      `json:"description,omitempty"`
	UniqueIdentifier  string      `json:"uniqueIdentifier,omitempty"`
	Script            string      `json:"script,omitempty"`
	Author            string      `json:"author,omitempty"`
	Agent             string      `json:"agent,omitempty"`
	DocumentationLink string      `json:"documentationLink,omitempty"`
	LastRunStatus     string      `json:"lastRunStatus,omitempty"`

	// No omitempty on the mutable scalars: a sparse PATCH that sets
	// enabled=false or intervalSeconds=0 must serialize the zero value or the
	// masked update silently no-ops.
	Enabled         bool `json:"enabled"`
	Advanced        bool `json:"advanced"`
	Custom          bool `json:"custom"`
	IntervalSeconds int  `json:"intervalSeconds"`

	AdvancedConfig *AdvancedConfig        `json:"advancedConfig,omitempty"`
	Parameters     []JobInstanceParameter `json:"parameters,omitempty"`

	// Timestamps (epoch millis as json.Number).
	CreateTime           json.Number `json:"createTime,omitempty"`
	UpdateTime           json.Number `json:"updateTime,omitempty"`
	LastRunTime          json.Number `json:"lastRunTime,omitempty"`
	NextScheduledRunTime json.Number `json:"nextScheduledRunTime,omitempty"`

	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and preserves the full payload in Raw.
func (ji *JobInstance) UnmarshalJSON(data []byte) error {
	type alias JobInstance
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*ji = JobInstance(a)
	ji.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// JobInstanceParameter is a single parameter on a job instance.
type JobInstanceParameter struct {
	ID          json.Number `json:"id,omitempty"`
	Mandatory   bool        `json:"mandatory"`
	Type        string      `json:"type,omitempty"`
	DisplayName string      `json:"displayName,omitempty"`
	Value       string      `json:"value"`
}

// JobInstanceLog is a single execution log entry for a job instance.
type JobInstanceLog struct {
	Name          string          `json:"name,omitempty"`
	ID            json.Number     `json:"id,omitempty"`
	StartTime     json.Number     `json:"startTime,omitempty"`
	EndTime       json.Number     `json:"endTime,omitempty"`
	Message       string          `json:"message,omitempty"`
	Status        string          `json:"status,omitempty"` // SUCCESS or ERROR
	JobIdentifier json.Number     `json:"jobIdentifier,omitempty"`
	JobInstanceId json.Number     `json:"jobInstanceId,omitempty"`
	Integration   string          `json:"integration,omitempty"`
	Raw           json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and preserves the full payload in Raw.
func (l *JobInstanceLog) UnmarshalJSON(data []byte) error {
	type alias JobInstanceLog
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*l = JobInstanceLog(a)
	l.Raw = append(json.RawMessage(nil), data...)
	return nil
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
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
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

// ListAllJobInstances returns every job instance across all integrations using
// the wildcard resource path.
func (c *Client) ListAllJobInstances(ctx context.Context) ([]JobInstance, error) {
	base := "integrations/-/jobs/-/jobInstances"
	var all []JobInstance
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		q := url.Values{}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			JobInstances  []json.RawMessage `json:"jobInstances"`
			Items         []json.RawMessage `json:"items"`
			TotalSize     int               `json:"totalSize"`
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
// the fields to change (e.g. "enabled", "intervalSeconds"); body is any
// JSON-marshalable payload (typically a *JobInstance or a partial map).
//
// DEVIATION: like connectors, secret parameters read back masked ("***...") from
// GetJobInstance. The server treats the masked sentinel as "unchanged", so a
// round-trip get-patch is safe: pass the masked value back verbatim to leave the
// secret intact. Only send a real cleartext value to genuinely rotate one.
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

// CreateJobInstance creates a new job instance under the given integration/job.
// LIVE MUTATION.
func (c *Client) CreateJobInstance(ctx context.Context, integration, jobID string, body any) (*JobInstance, error) {
	res := fmt.Sprintf("integrations/%s/jobs/%s/jobInstances", integration, jobID)
	var raw json.RawMessage
	if err := c.t.V1Alpha(ctx, http.MethodPost, res, body, &raw); err != nil {
		return nil, err
	}
	return decodeJobInstance(raw)
}

// DeleteJobInstance deletes a job instance. LIVE MUTATION.
func (c *Client) DeleteJobInstance(ctx context.Context, integration, jobID, instanceID string) error {
	res := jobInstancePath(integration, jobID, instanceID)
	return c.t.V1Alpha(ctx, http.MethodDelete, res, nil, nil)
}

// RunJobInstanceOnDemand runs a scheduled job instance immediately rather than
// waiting for its schedule. LIVE MUTATION.
func (c *Client) RunJobInstanceOnDemand(ctx context.Context, integration, jobID, instanceID string) (json.RawMessage, error) {
	var out json.RawMessage
	res := jobInstancePath(integration, jobID, instanceID) + ":runOnDemand"
	if err := c.t.V1Alpha(ctx, http.MethodPost, res, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListJobInstanceLogs returns execution logs for a job instance. Returns
// (logs, nextPageToken, totalSize, error).
func (c *Client) ListJobInstanceLogs(ctx context.Context, integration, jobID, instanceID string, pageSize int, pageToken string) ([]JobInstanceLog, string, int, error) {
	res := fmt.Sprintf("integrations/%s/jobs/%s/jobInstances/%s/logs",
		integration, jobID, instanceID)
	q := url.Values{}
	if pageSize > 0 {
		q.Set("pageSize", fmt.Sprintf("%d", pageSize))
	}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}

	var resp struct {
		Logs          []json.RawMessage `json:"logs"`
		Items         []json.RawMessage `json:"items"`
		NextPageToken string            `json:"nextPageToken"`
		TotalSize     int               `json:"totalSize"`
	}
	if err := c.t.V1Alpha(ctx, http.MethodGet, res, nil, &resp, transport.Query(q)); err != nil {
		return nil, "", 0, err
	}

	batch := resp.Logs
	if len(batch) == 0 {
		batch = resp.Items
	}
	logs := make([]JobInstanceLog, 0, len(batch))
	for _, item := range batch {
		var l JobInstanceLog
		if err := json.Unmarshal(item, &l); err != nil {
			return nil, "", 0, fmt.Errorf("soar: decode job instance log: %w", err)
		}
		logs = append(logs, l)
	}
	return logs, resp.NextPageToken, resp.TotalSize, nil
}

// GetJobInstanceLog fetches a single execution log entry.
func (c *Client) GetJobInstanceLog(ctx context.Context, integration, jobID, instanceID, logID string) (*JobInstanceLog, error) {
	res := fmt.Sprintf("integrations/%s/jobs/%s/jobInstances/%s/logs/%s",
		integration, jobID, instanceID, logID)
	var out JobInstanceLog
	if err := c.t.V1Alpha(ctx, http.MethodGet, res, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
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
	return &ji, nil
}
