package chronicle

import (
	"context"
	"maps"
	"net/url"
	"path"
	"strings"
)

// Feed write operations: create / get / update / enable / disable / delete and
// per-feed secret generation. The Feed type and ListFeeds live in feeds.go;
// this file extends that surface with the mutating endpoints.
//
// All feed endpoints use the project ID (string) form — numeric=false — because
// the upstream wrapper builds every feeds/* URL from the string project_id (see
// feeds.py / resource.go). The collection is {instance}/feeds and an individual
// feed is {instance}/feeds/{feedID}.

// feedDetails is the body's "details" sub-object: the feed source type, the
// (full or short) log type resource, an optional asset namespace, optional
// labels, and the source-specific settings (httpSettings, s3Settings, ...).
//
// Settings is folded in at the same level as feedSourceType/logType — feeds
// carry their transport config as sibling keys of feedSourceType (e.g.
// httpSettings), not nested under a "settings" key. We therefore marshal a flat
// merge: the known keys plus whatever the caller passes in settings.
//
// DEVIATION: the wrapper takes an opaque details dict and the caller must spell
// out feedSourceType/logType by hand. We accept those as typed parameters and
// assemble the dict, while still letting settings carry the freeform,
// source-specific remainder (and any overrides) verbatim.
type feedDetails map[string]any

// buildDetails assembles the details object from the typed parameters plus the
// freeform source-specific settings. Empty typed fields are omitted; settings
// keys are merged last so callers retain full control (including overriding the
// derived keys or adding labels).
func buildDetails(sourceType, logType, namespace string, settings map[string]any) feedDetails {
	d := feedDetails{}
	if sourceType != "" {
		d["feedSourceType"] = sourceType
	}
	if logType != "" {
		d["logType"] = logType
	}
	if namespace != "" {
		// Feed asset namespace ("environment"); tags ingested events. The live
		// API returns and accepts this under "assetNamespace" (verified against a
		// live tenant); the read side (mirror.feedRecord) uses the same key.
		d["assetNamespace"] = namespace
	}
	maps.Copy(d, settings)
	return d
}

// feedCreateBody is the POST/PATCH request body. Note the camelCase
// "displayName"/"details": the wrapper relies on asdict() producing snake_case
// and the server tolerating it, but the canonical Feed proto is camelCase, so
// we send the correct form.
//
// DEVIATION: wrapper emits display_name/details (snake_case from dataclass
// asdict); we send the canonical camelCase field names.
type feedCreateBody struct {
	DisplayName string      `json:"displayName,omitempty"`
	Details     feedDetails `json:"details,omitempty"`
}

// feedSecret is the response of GenerateSecret: {"secret": "..."}.
type feedSecret struct {
	Secret string `json:"secret"`
}

// feedID returns the trailing path segment of a feed name or ID, so callers may
// pass either "fe_xxx" or the full "projects/.../feeds/fe_xxx" resource name
// (mirrors feeds.py os.path.basename(feed_id)).
func feedID(id string) string {
	return path.Base(strings.TrimRight(id, "/"))
}

// CreateFeed creates a new ingestion feed. sourceType is the feedSourceType
// enum string (e.g. "HTTP", "AMAZON_S3"), logType is the log type — either a
// short name ("WINEVTLOG") or a full logTypes/* resource name — namespace is an
// optional asset namespace, and settings carries the source-specific config
// (httpSettings, s3Settings, labels, ...). Any of namespace/settings may be
// zero/nil.
func (c *Client) CreateFeed(ctx context.Context, displayName, sourceType, logType, namespace string, settings map[string]any) (*Feed, error) {
	body := feedCreateBody{
		DisplayName: displayName,
		Details:     buildDetails(sourceType, logType, namespace, settings),
	}
	var f Feed
	if err := c.post(ctx, c.resourcePath("feeds", false), body, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// GetFeed fetches a single feed by ID (accepts a bare ID or a full resource
// name).
func (c *Client) GetFeed(ctx context.Context, id string) (*Feed, error) {
	var f Feed
	if err := c.get(ctx, c.resourcePath("feeds/"+feedID(id), false), &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// UpdateFeed patches a feed. Only the supplied fields are sent and the
// updateMask is derived from exactly those: a non-empty displayName sets the
// "displayName" mask, and a non-nil details (built from sourceType/logType/
// namespace/settings) sets "details". Pass displayName="" to leave the name
// unchanged; pass all of sourceType/logType/namespace empty and settings nil to
// leave details unchanged.
//
// DEVIATION: the wrapper rebuilds the entire details object on every update and
// only auto-masks top-level keys; we mask precisely the fields the caller
// provided, so an unset details is never blanked server-side.
func (c *Client) UpdateFeed(ctx context.Context, id, displayName, sourceType, logType, namespace string, settings map[string]any) (*Feed, error) {
	body := feedCreateBody{}
	var mask []string
	if displayName != "" {
		body.DisplayName = displayName
		mask = append(mask, "displayName")
	}
	if d := buildDetails(sourceType, logType, namespace, settings); len(d) > 0 {
		body.Details = d
		mask = append(mask, "details")
	}
	var q []requestOption
	if len(mask) > 0 {
		q = append(q, withQuery(url.Values{"updateMask": {strings.Join(mask, ",")}}))
	}
	var f Feed
	if err := c.patch(ctx, c.resourcePath("feeds/"+feedID(id), false), body, &f, q...); err != nil {
		return nil, err
	}
	return &f, nil
}

// EnableFeed enables a feed (POST {feed}:enable) and returns its updated state.
func (c *Client) EnableFeed(ctx context.Context, id string) (*Feed, error) {
	return c.feedAction(ctx, id, "enable")
}

// DisableFeed disables a feed (POST {feed}:disable) and returns its updated
// state.
func (c *Client) DisableFeed(ctx context.Context, id string) (*Feed, error) {
	return c.feedAction(ctx, id, "disable")
}

// feedAction performs an RPC-style verb on a feed ({feed}:enable / :disable).
func (c *Client) feedAction(ctx context.Context, id, action string) (*Feed, error) {
	p := c.resourcePath("feeds/"+feedID(id), false) + ":" + action
	var f Feed
	if err := c.post(ctx, p, nil, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// DeleteFeed deletes a feed by ID.
func (c *Client) DeleteFeed(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", c.resourcePath("feeds/"+feedID(id), false), nil, nil)
}

// GenerateSecret generates (or rotates) the shared secret for a feed that
// supports one (e.g. HTTPS push feeds) and returns the secret string.
func (c *Client) GenerateSecret(ctx context.Context, id string) (string, error) {
	p := c.resourcePath("feeds/"+feedID(id), false) + ":generateSecret"
	var s feedSecret
	if err := c.post(ctx, p, nil, &s); err != nil {
		return "", err
	}
	return s.Secret, nil
}
