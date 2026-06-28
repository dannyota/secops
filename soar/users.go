package soar

import (
	"context"
	"encoding/json"
	"net/url"

	"danny.vn/secops/soar/internal/transport"
)

// User is a SOAR user from the legacySoarUsers v1alpha collection.
type User struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"displayName"`
	Email       string          `json:"email"`
	UserName    string          `json:"userName"`
	Raw         json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and keeps the full object in Raw.
func (u *User) UnmarshalJSON(data []byte) error {
	type alias User
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*u = User(a)
	u.Raw = append(json.RawMessage(nil), data...)
	return nil
}

type usersList struct {
	Items         []User `json:"legacySoarUsers"`
	NextPageToken string `json:"nextPageToken"`
}

// ListUsers returns all SOAR users via the v1alpha legacySoarUsers collection.
func (c *Client) ListUsers(ctx context.Context) ([]User, error) {
	var all []User
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var page usersList
		if err := c.t.V1Alpha(ctx, "GET", "legacySoarUsers", nil, &page, transport.Query(q)); err != nil {
			return "", err
		}
		all = append(all, page.Items...)
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// ListUsersFiltered returns SOAR users matching a server-side filter (e.g.
// "accountState = 'Active'").
func (c *Client) ListUsersFiltered(ctx context.Context, filter string) ([]User, error) {
	var all []User
	err := transport.PaginateV1Alpha(listMaxPages, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if filter != "" {
			q.Set("filter", filter)
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		var page usersList
		if err := c.t.V1Alpha(ctx, "GET", "legacySoarUsers", nil, &page, transport.Query(q)); err != nil {
			return "", err
		}
		all = append(all, page.Items...)
		return page.NextPageToken, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// GetUser returns a single SOAR user by their UUID.
func (c *Client) GetUser(ctx context.Context, userID string) (*User, error) {
	var out User
	if err := c.t.V1Alpha(ctx, "GET", "legacySoarUsers/"+userID, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetUserLocalization returns the localization settings for a SOAR user.
func (c *Client) GetUserLocalization(ctx context.Context, userID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", "legacySoarUsers/"+userID+"/localization", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetUserNotificationCount returns the unread notification count for a user.
func (c *Client) GetUserNotificationCount(ctx context.Context, userID string) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", "legacySoarUsers/"+userID+"/userNotifications:count", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
