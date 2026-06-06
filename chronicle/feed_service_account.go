package chronicle

import (
	"context"
	"encoding/json"
)

// FeedServiceAccount identifies the GCP service account Chronicle uses to read a
// customer's feed sources. For GOOGLE_CLOUD_STORAGE_V2 (and other V2 pull) feeds
// this is the Storage Transfer Service account that must be granted read access
// to the source bucket BEFORE the feed can be created — otherwise the create
// fails with FAILED_PRECONDITION.
//
// The API field carrying the email has varied across versions; ServiceAccount /
// ServiceAccountEmail cover the known shapes and Email() returns whichever is
// populated, with Raw kept for anything unmodeled.
type FeedServiceAccount struct {
	Name                string          `json:"name,omitempty"`
	ServiceAccount      string          `json:"serviceAccount,omitempty"`
	ServiceAccountEmail string          `json:"serviceAccountEmail,omitempty"`
	SubjectID           string          `json:"subjectId,omitempty"` // the SA's unique numeric id (what the live API returns)
	Raw                 json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and retains the full object in Raw.
func (f *FeedServiceAccount) UnmarshalJSON(data []byte) error {
	type alias FeedServiceAccount
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*f = FeedServiceAccount(a)
	f.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// Email returns the service account email from whichever field the API populated.
func (f *FeedServiceAccount) Email() string {
	if f.ServiceAccount != "" {
		return f.ServiceAccount
	}
	return f.ServiceAccountEmail
}

// FetchFeedServiceAccount returns the service account Chronicle uses to read this
// customer's feed sources (feedServiceAccounts:fetchServiceAccountForCustomer).
// Grant the returned account read access to a GCS bucket before creating a
// GOOGLE_CLOUD_STORAGE_V2 feed against it.
//
// DEVIATION: this endpoint uses the project NUMBER form (numeric=true), matching
// the live API; most other feed endpoints use the project ID.
func (c *Client) FetchFeedServiceAccount(ctx context.Context) (*FeedServiceAccount, error) {
	var f FeedServiceAccount
	if err := c.get(ctx, c.resourcePath("feedServiceAccounts:fetchServiceAccountForCustomer", true), &f); err != nil {
		return nil, err
	}
	return &f, nil
}
