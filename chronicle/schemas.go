// schemas.go — ingestion schema discovery (SIEM plane, read-only).
//
// The schema surfaces describe the shape of a valid ingestion Feed:
// feedSourceTypeSchemas enumerates the available feed source types (HTTP, S3,
// GCS, third-party APIs, ...) and, per source type, logTypeSchemas lists the log
// types it accepts together with the detail fields each requires. logTypes is the
// catalog of ingest log types; logTypeSetting is the per-log-type ingestion
// configuration. Together they are the reference for validating feed YAML before a
// deploy.
//
// These are upstream-defined, so there is no write path — this is a read surface.
// They ride the feeds family on the chronicle host (v1alpha default, project ID
// form), matching feeds.go. See docs/SURFACES.md.

package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
)

// FeedSourceTypeSchema describes one feed source type. The stable framing is
// typed; the full server object is preserved in Raw. LogTypeSchemas is populated
// only when the server inlines the per-source-type log schemas in the list
// response — otherwise fetch them with ListLogTypeSchemas. Read-only.
type FeedSourceTypeSchema struct {
	Name           string          `json:"name"` // .../feedSourceTypeSchemas/{feedSourceType}
	ID             string          `json:"-"`    // short id (last name segment) == feedSourceType
	DisplayName    string          `json:"displayName"`
	Description    string          `json:"description"`
	FeedSourceType string          `json:"feedSourceType"` // value for details.feed_source_type on a Feed
	ReadOnly       bool            `json:"readOnly"`
	LogTypeSchemas []LogTypeSchema `json:"logTypeSchemas,omitempty"`
	Raw            json.RawMessage `json:"-"` // full server object, untrimmed
}

// UnmarshalJSON decodes the typed fields, derives the short ID from the resource
// name, and preserves the complete server object in Raw.
func (s *FeedSourceTypeSchema) UnmarshalJSON(data []byte) error {
	type alias FeedSourceTypeSchema // avoid recursion
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*s = FeedSourceTypeSchema(a)
	s.ID = lastSegment(s.Name)
	s.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// LogTypeSchema describes the detail fields a feed needs for one log type under a
// given feed source type. The stable framing is typed; the field schemas (and any
// alternatives) live in Raw. Read-only.
type LogTypeSchema struct {
	Name                    string          `json:"name"` // .../feedSourceTypeSchemas/{sourceType}/logTypeSchemas/{logType}
	ID                      string          `json:"-"`    // short id (last name segment)
	DisplayName             string          `json:"displayName"`
	LogType                 string          `json:"logType"` // value for details.log_type on a Feed
	ReadOnly                bool            `json:"readOnly"`
	SupportingDocumentation string          `json:"supportingDocumentation,omitempty"`
	Raw                     json.RawMessage `json:"-"` // full server object incl. detailsFieldSchemas
}

// UnmarshalJSON decodes the typed fields, derives the short ID, keeps Raw.
func (s *LogTypeSchema) UnmarshalJSON(data []byte) error {
	type alias LogTypeSchema
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*s = LogTypeSchema(a)
	s.ID = lastSegment(s.Name)
	s.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// LogType (name + displayName) is defined in log_meta.go alongside ListLogTypes;
// GetLogType below fetches one by id.

// LogTypeSetting is the per-log-type ingestion configuration (a singleton
// sub-resource at {instance}/logTypes/{logType}/logTypeSetting). The stable
// framing is typed; the full server object is preserved in Raw. Read-only.
type LogTypeSetting struct {
	Name string          `json:"name"` // .../logTypes/{logType}/logTypeSetting
	Raw  json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and keeps Raw.
func (s *LogTypeSetting) UnmarshalJSON(data []byte) error {
	type alias LogTypeSetting
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*s = LogTypeSetting(a)
	s.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ListFeedSourceTypeSchemas returns every feed source type schema on the instance.
// Read-only.
//
// Endpoint: GET {instance}/feedSourceTypeSchemas (key "feedSourceTypeSchemas").
// Rides the feeds family — project ID form, v1alpha default.
func (c *Client) ListFeedSourceTypeSchemas(ctx context.Context) ([]FeedSourceTypeSchema, error) {
	var all []FeedSourceTypeSchema
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"100"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			FeedSourceTypeSchemas []FeedSourceTypeSchema `json:"feedSourceTypeSchemas"`
			NextPageToken         string                 `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("feedSourceTypeSchemas", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.FeedSourceTypeSchemas...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// ListLogTypeSchemas returns the log type schemas compatible with a feed source
// type (its accepted log types and the detail fields each needs). feedSourceType
// is the short id or full resource name of a feedSourceTypeSchema. Read-only.
//
// Endpoint: GET {instance}/feedSourceTypeSchemas/{sourceType}/logTypeSchemas
// (key "logTypeSchemas").
func (c *Client) ListLogTypeSchemas(ctx context.Context, feedSourceType string) ([]LogTypeSchema, error) {
	sub := "feedSourceTypeSchemas/" + url.PathEscape(lastSegment(feedSourceType)) + "/logTypeSchemas"
	var all []LogTypeSchema
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			LogTypeSchemas []LogTypeSchema `json:"logTypeSchemas"`
			NextPageToken  string          `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath(sub, false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.LogTypeSchemas...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetLogType fetches one ingest log type by short id or full resource name.
// Read-only.
//
// Endpoint: GET {instance}/logTypes/{logType}.
func (c *Client) GetLogType(ctx context.Context, logType string) (*LogType, error) {
	var out LogType
	if err := c.get(ctx, c.resourcePath("logTypes/"+url.PathEscape(lastSegment(logType)), false), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetLogTypeSetting fetches the per-log-type ingestion configuration. logType is
// the short id or full resource name of a log type. Read-only.
//
// Endpoint: GET {instance}/logTypes/{logType}/logTypeSetting.
func (c *Client) GetLogTypeSetting(ctx context.Context, logType string) (*LogTypeSetting, error) {
	sub := "logTypes/" + url.PathEscape(lastSegment(logType)) + "/logTypeSetting"
	var out LogTypeSetting
	if err := c.get(ctx, c.resourcePath(sub, false), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
