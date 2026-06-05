// LEGACY tier: the Siemplify external API (/api/external/v1) jobs surface.
//
// Jobs are the scheduled background tasks SOAR runs (sync, hygiene, etc.). These
// endpoints list job definitions/templates and manage job *instances* (CRUD +
// run). Reads return RawJSON; writes take a freeform body. AppKey auth.
package legacy

import (
	"context"
	"net/http"
	"net/url"
)

// ListInstalledJobs returns every installed job in the platform.
func (c *Client) ListInstalledJobs(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/jobs/GetInstalledJobs")
}

// ListJobTemplates returns the configurable job templates in the system.
func (c *Client) ListJobTemplates(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/jobs/GetJobTemplates")
}

// ListJobInstances returns every configured job instance.
func (c *Client) ListJobInstances(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/jobs/instances")
}

// RunJob runs a job by identifier. body is the freeform run payload (carries the
// job identifier). LIVE: this executes the job now.
func (c *Client) RunJob(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/jobs/RunJob", body)
}

// RunJobInstance runs a specific job instance now. body carries the instance id.
// LIVE: this executes the job now.
func (c *Client) RunJobInstance(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/jobs/instances/run", body)
}

// SaveOrUpdateJob adds or updates a job (definition-level). body is the freeform
// job payload. LIVE MUTATION.
func (c *Client) SaveOrUpdateJob(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/jobs/SaveOrUpdateJobData", body)
}

// CreateJobInstance adds a new job instance. body is the freeform instance
// payload. LIVE MUTATION.
func (c *Client) CreateJobInstance(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/jobs/instances", body)
}

// UpdateJobInstance updates an existing job instance. body is the freeform
// instance payload. LIVE MUTATION.
func (c *Client) UpdateJobInstance(ctx context.Context, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/jobs/instances", body)
}

// DeleteJobData removes a job (definition-level). body carries the job
// identifier. LIVE MUTATION; this cannot be undone.
func (c *Client) DeleteJobData(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/jobs/DeleteJobData", body)
}

// DeleteJobInstance removes a job instance by id. LIVE MUTATION; cannot be undone.
func (c *Client) DeleteJobInstance(ctx context.Context, id string) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/jobs/instances/"+url.PathEscape(id), nil)
}
