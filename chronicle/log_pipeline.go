package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// Log-processing pipelines transform inbound logs before parsing/ingestion.
// All endpoints live under the instance collection logProcessingPipelines and
// use the project ID (string) form — numeric=false — matching the wrapper,
// which builds every instance URL from the string project_id. See resource.go.

// Stream identifies one log source bound to a pipeline. Exactly one of LogType
// or FeedID is set, mirroring the wrapper's {"logType": ...} / {"feedId": ...}
// stream dicts.
type Stream struct {
	LogType string `json:"logType,omitempty"`
	FeedID  string `json:"feedId,omitempty"`
}

// Pipeline is a log-processing pipeline resource.
//
// Name is projects/.../logProcessingPipelines/<id>; the bare <id> used by the
// per-pipeline methods is the final path segment (see PipelineID).
//
// Processors and CustomMetadata are kept as json.RawMessage: their schemas are
// rich and evolving (processor types, conditions, transforms) and the SDK does
// not need to interpret them — callers round-trip the freeform config verbatim.
type Pipeline struct {
	Name           string          `json:"name,omitempty"`
	DisplayName    string          `json:"displayName,omitempty"`
	Description    string          `json:"description,omitempty"`
	Processors     json.RawMessage `json:"processors,omitempty"`
	CustomMetadata json.RawMessage `json:"customMetadata,omitempty"`
	Etag           string          `json:"etag,omitempty"`
	CreateTime     string          `json:"createTime,omitempty"`
	UpdateTime     string          `json:"updateTime,omitempty"`
}

// PipelineID returns the trailing <id> segment of the pipeline's resource Name.
func (p *Pipeline) PipelineID() string {
	if p == nil || p.Name == "" {
		return ""
	}
	return p.Name[strings.LastIndex(p.Name, "/")+1:]
}

// pipelineResourceID extracts the bare pipeline ID from either a full resource
// name (projects/.../logProcessingPipelines/<id>) or a bare ID.
//
// Mirrors the wrapper's format_resource_id helper.
func pipelineResourceID(id string) string {
	if strings.HasPrefix(id, "projects/") {
		return id[strings.LastIndex(id, "/")+1:]
	}
	return id
}

// ListPipelines returns every log-processing pipeline on the instance,
// optionally restricted by an AIP-160 filter expression (filterExpr=="" for
// none). All pages are followed.
func (c *Client) ListPipelines(ctx context.Context, filterExpr string) ([]Pipeline, error) {
	var all []Pipeline
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if filterExpr != "" {
			q.Set("filter", filterExpr)
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			LogProcessingPipelines []Pipeline `json:"logProcessingPipelines"`
			NextPageToken          string     `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("logProcessingPipelines", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.LogProcessingPipelines...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetPipeline fetches a single pipeline. pipelineID may be a bare ID or a full
// resource name.
func (c *Client) GetPipeline(ctx context.Context, pipelineID string) (*Pipeline, error) {
	id := pipelineResourceID(pipelineID)
	var p Pipeline
	if err := c.get(ctx, c.resourcePath("logProcessingPipelines/"+id, false), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// CreatePipeline creates a new pipeline. If pipelineID is non-empty it is sent
// as logProcessingPipelineId; otherwise the server assigns one.
func (c *Client) CreatePipeline(ctx context.Context, p *Pipeline, pipelineID string) (*Pipeline, error) {
	var opts []requestOption
	if pipelineID != "" {
		opts = append(opts, withQuery(url.Values{"logProcessingPipelineId": {pipelineID}}))
	}
	var out Pipeline
	if err := c.post(ctx, c.resourcePath("logProcessingPipelines", false), p, &out, opts...); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdatePipeline patches a pipeline. updateMask is a comma-separated field list
// (e.g. "displayName,description"); pass "" to let the server overwrite all
// provided fields.
//
// DEVIATION: updateMask is an explicit caller argument here. The wrapper takes
// the same shape; we keep the body typed (*Pipeline) rather than a freeform map.
func (c *Client) UpdatePipeline(ctx context.Context, pipelineID string, p *Pipeline, updateMask string) (*Pipeline, error) {
	id := pipelineResourceID(pipelineID)
	var opts []requestOption
	if updateMask != "" {
		opts = append(opts, withQuery(url.Values{"updateMask": {updateMask}}))
	}
	var out Pipeline
	if err := c.patch(ctx, c.resourcePath("logProcessingPipelines/"+id, false), p, &out, opts...); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeletePipeline deletes a pipeline. If etag is non-empty it is sent for
// optimistic concurrency — deletion fails on mismatch.
func (c *Client) DeletePipeline(ctx context.Context, pipelineID, etag string) error {
	id := pipelineResourceID(pipelineID)
	var opts []requestOption
	if etag != "" {
		opts = append(opts, withQuery(url.Values{"etag": {etag}}))
	}
	return c.do(ctx, "DELETE", c.resourcePath("logProcessingPipelines/"+id, false), nil, nil, opts...)
}

// AssociateStreams binds the given streams (log types / feeds) to a pipeline.
func (c *Client) AssociateStreams(ctx context.Context, pipelineID string, streams []Stream) error {
	id := pipelineResourceID(pipelineID)
	body := struct {
		Streams []Stream `json:"streams"`
	}{Streams: streams}
	path := c.resourcePath("logProcessingPipelines/"+id, false) + ":associateStreams"
	return c.post(ctx, path, body, nil)
}

// DissociateStreams unbinds the given streams from a pipeline.
func (c *Client) DissociateStreams(ctx context.Context, pipelineID string, streams []Stream) error {
	id := pipelineResourceID(pipelineID)
	body := struct {
		Streams []Stream `json:"streams"`
	}{Streams: streams}
	path := c.resourcePath("logProcessingPipelines/"+id, false) + ":dissociateStreams"
	return c.post(ctx, path, body, nil)
}

// FetchAssociatedPipeline returns the pipeline currently associated with a
// single stream. The stream's set field is sent as stream.logType /
// stream.feedId query params.
func (c *Client) FetchAssociatedPipeline(ctx context.Context, stream Stream) (*Pipeline, error) {
	q := url.Values{}
	if stream.LogType != "" {
		q.Set("stream.logType", stream.LogType)
	}
	if stream.FeedID != "" {
		q.Set("stream.feedId", stream.FeedID)
	}
	path := c.resourcePath("logProcessingPipelines", false) + ":fetchAssociatedPipeline"
	var p Pipeline
	if err := c.get(ctx, path, &p, withQuery(q)); err != nil {
		return nil, err
	}
	return &p, nil
}

// SampleLogs is the result of FetchSampleLogsByStreams.
//
// Logs is the structured log list; SampleLogs holds the deprecated
// base64-encoded log strings. Both are kept freeform (the per-log shape is
// source-specific).
type SampleLogs struct {
	Logs       []json.RawMessage `json:"logs,omitempty"`
	SampleLogs []string          `json:"sampleLogs,omitempty"`
}

// FetchSampleLogsByStreams fetches sample logs for the given streams.
// sampleLogsCount is the per-stream count (server default 100 when 0; max 1000
// or 4MB per stream).
func (c *Client) FetchSampleLogsByStreams(ctx context.Context, streams []Stream, sampleLogsCount int) (*SampleLogs, error) {
	body := struct {
		Streams         []Stream `json:"streams"`
		SampleLogsCount int      `json:"sampleLogsCount,omitempty"`
	}{Streams: streams, SampleLogsCount: sampleLogsCount}
	path := c.resourcePath("logProcessingPipelines", false) + ":fetchSampleLogsByStreams"
	var out SampleLogs
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TestPipelineResult is the result of TestPipeline: the input logs after being
// run through the candidate pipeline. Kept freeform per the wrapper.
type TestPipelineResult struct {
	Logs []json.RawMessage `json:"logs,omitempty"`
}

// TestPipeline runs inputLogs through a candidate pipeline config without
// persisting it, returning the processed logs.
//
// inputLogs are freeform log objects (json.RawMessage); each is sent verbatim.
func (c *Client) TestPipeline(ctx context.Context, config *Pipeline, inputLogs []json.RawMessage) (*TestPipelineResult, error) {
	body := struct {
		LogProcessingPipeline *Pipeline         `json:"logProcessingPipeline"`
		InputLogs             []json.RawMessage `json:"inputLogs"`
	}{LogProcessingPipeline: config, InputLogs: inputLogs}
	path := c.resourcePath("logProcessingPipelines", false) + ":testPipeline"
	var out TestPipelineResult
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
