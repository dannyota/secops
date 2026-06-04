package chronicle

import (
	"context"
	"net/url"
)

// Parser is a log-type parser configuration on the Chronicle instance.
//
// CBN holds the parser source (the "Config-Based Normalizer") as the API
// returns it: base64-encoded. The mirror layer decodes it to a .conf file; the
// SDK keeps it raw so callers decide when to decode.
//
// Creator and VersionInfo are intentionally freeform: they are small,
// loosely-specified metadata objects (creator.source, versionInfo.version,
// versionInfo.rollbackAvailable, ...) whose shapes the API does not firmly
// commit to. Everything load-bearing for pull/push is a typed scalar field.
type Parser struct {
	Name         string         `json:"name"`
	State        string         `json:"state"`
	Type         string         `json:"type"`
	ReleaseStage string         `json:"releaseStage,omitempty"`
	CreateTime   string         `json:"createTime,omitempty"`
	CBN          string         `json:"cbn,omitempty"` // base64-encoded parser source
	Creator      map[string]any `json:"creator,omitempty"`
	VersionInfo  map[string]any `json:"versionInfo,omitempty"`
}

// ListParsers returns every parser configured for logType (e.g. "OKTA",
// "WINDOWS_AD"). Both ACTIVE and inactive parsers are returned; callers filter
// for state == "ACTIVE" when they want the live one.
//
// DEVIATION: parsers use the project NUMBER form (numeric=true), matching the
// legacy tool's raw_get(..., numeric_project=True). This diverges from the
// resource.go doc table, which lists parsers under the project-ID form; the
// live endpoint accepts (and the legacy tool relies on) the numeric form, so
// that is what is encoded here.
//
// DEVIATION: page size is capped at 100 to mirror the legacy puller and the
// endpoint's documented per-page limit, rather than the 1000 used for rules.
func (c *Client) ListParsers(ctx context.Context, logType string) ([]Parser, error) {
	sub := "logTypes/" + url.PathEscape(logType) + "/parsers"
	var all []Parser
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"100"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Parsers       []Parser `json:"parsers"`
			NextPageToken string   `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath(sub, true), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.Parsers...)
		return resp.NextPageToken, nil
	})
	return all, err
}
