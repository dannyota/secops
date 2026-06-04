package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
)

// Feed is an ingestion feed configured on the Chronicle instance.
//
// Details is intentionally a freeform map: feed shapes vary widely by source
// type (HTTP, S3, GCS, Azure Blob, third-party APIs, ...) and carry
// credential-bearing scalars (apiKey, password, secret, ...). Keeping it
// untyped lets the mirror layer recurse and redact secrets (see
// internal/mirror.redact) before anything is written to disk.
//
// FailureDetails is kept as json.RawMessage because the API returns a small
// structured object (errorCode, httpErrorCode, errorCause, errorAction) only on
// failed feeds; callers that care can unmarshal it on demand.
type Feed struct {
	Name                   string          `json:"name"`
	DisplayName            string          `json:"displayName"`
	UID                    string          `json:"uid"`
	State                  string          `json:"state"`
	Details                map[string]any  `json:"details,omitempty"`
	LastFeedInitiationTime string          `json:"lastFeedInitiationTime,omitempty"`
	FailureMsg             string          `json:"failureMsg,omitempty"`
	FailureDetails         json.RawMessage `json:"failureDetails,omitempty"`
}

// ListFeeds returns every ingestion feed on the instance.
//
// DEVIATION: feeds use the project ID form (numeric=false), unlike rules. The
// official wrapper resolves the project form indirectly via its request helper;
// here it is explicit per the resource.go contract.
func (c *Client) ListFeeds(ctx context.Context) ([]Feed, error) {
	var all []Feed
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Feeds         []Feed `json:"feeds"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("feeds", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.Feeds...)
		return resp.NextPageToken, nil
	})
	return all, err
}
