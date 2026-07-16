package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// Error notification configs let a customer receive ingestion-health alerts
// (zero-ingest, size-threshold, normalization-delay) on Cloud Monitoring
// notification channels. Full CRUD on the chronicle host instance path
// (projects/.../instances/.../errorNotificationConfigs). The id is server-assigned
// on create. Project ID (string) form (numeric=false).
//
// A config carries exactly one notification_type block (a oneof):
// ingestionCountZeroNotifications / ingestionSizeThresholdNotifications /
// normalizationDelayThresholdNotifications — kept as raw JSON here since the typed
// top-level fields (displayName/enabled/notificationChannels) are what callers
// branch on, while the condition blocks are passed through verbatim.

// ErrorNotificationConfig is an ingestion-health notification configuration.
type ErrorNotificationConfig struct {
	Name                 string   `json:"name,omitempty"`
	DisplayName          string   `json:"displayName,omitempty"`
	Enabled              bool     `json:"enabled"`
	NotificationChannels []string `json:"notificationChannels,omitempty"`
	// The oneof notification_type — exactly one is set.
	IngestionCountZero     json.RawMessage `json:"ingestionCountZeroNotifications,omitempty"`
	IngestionSizeThreshold json.RawMessage `json:"ingestionSizeThresholdNotifications,omitempty"`
	NormalizationDelay     json.RawMessage `json:"normalizationDelayThresholdNotifications,omitempty"`
	Raw                    json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and retains the full payload in Raw.
func (e *ErrorNotificationConfig) UnmarshalJSON(data []byte) error {
	type alias ErrorNotificationConfig
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*e = ErrorNotificationConfig(a)
	e.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ID returns the trailing resource-id segment of the config's Name.
func (e *ErrorNotificationConfig) ID() string {
	if e == nil {
		return ""
	}
	return lastSegment(e.Name)
}

// ListErrorNotificationConfigs returns every config in the instance, paginating
// over nextPageToken.
func (c *Client) ListErrorNotificationConfigs(ctx context.Context) ([]ErrorNotificationConfig, error) {
	var all []ErrorNotificationConfig
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"100"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			ErrorNotificationConfigs []ErrorNotificationConfig `json:"errorNotificationConfigs"`
			NextPageToken            string                    `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("errorNotificationConfigs", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.ErrorNotificationConfigs...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetErrorNotificationConfig fetches a single config by its <id> segment.
func (c *Client) GetErrorNotificationConfig(ctx context.Context, id string) (*ErrorNotificationConfig, error) {
	var e ErrorNotificationConfig
	if err := c.get(ctx, c.resourcePath("errorNotificationConfigs/"+url.PathEscape(lastSegment(id)), false), &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// CreateErrorNotificationConfig creates a config from the given body. The id is
// server-assigned; the created resource (with its name) is returned.
func (c *Client) CreateErrorNotificationConfig(ctx context.Context, body json.RawMessage) (*ErrorNotificationConfig, error) {
	var e ErrorNotificationConfig
	if err := c.post(ctx, c.resourcePath("errorNotificationConfigs", false), body, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// UpdateErrorNotificationConfig patches a config, sending body under updateMask.
// An empty mask defaults to the operator-editable field set.
func (c *Client) UpdateErrorNotificationConfig(ctx context.Context, id string, body json.RawMessage, mask []string) (*ErrorNotificationConfig, error) {
	if len(mask) == 0 {
		mask = []string{"display_name", "enabled", "notification_channels"}
	}
	q := url.Values{"updateMask": {strings.Join(mask, ",")}}
	var e ErrorNotificationConfig
	if err := c.patch(ctx, c.resourcePath("errorNotificationConfigs/"+url.PathEscape(lastSegment(id)), false), body, &e, withQuery(q)); err != nil {
		return nil, err
	}
	return &e, nil
}

// DeleteErrorNotificationConfig deletes a config by its <id> segment.
func (c *Client) DeleteErrorNotificationConfig(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", c.resourcePath("errorNotificationConfigs/"+url.PathEscape(lastSegment(id)), false), nil, nil)
}
