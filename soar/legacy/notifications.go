// LEGACY tier: the Siemplify external API (/api/external/v1) Notifications surface.
//
// These endpoints expose the in-product notification center: the user's
// notification list and unread count, per-user notification settings, and the
// dismiss/close operations for individual or all notifications. Several of the
// close operations are modeled upstream as GETs even though they mutate state
// (they dismiss notifications), so their doc comments are flagged accordingly.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). All methods speak the AppKey-authenticated external
// API via c.t.External.
package legacy

import (
	"context"
	"strconv"
)

// NotificationListUser returns the current user's notifications.
func (c *Client) NotificationListUser(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/notifications/GetUserNotifications")
}

// NotificationGetUnreadCount returns the count of unread notifications.
func (c *Client) NotificationGetUnreadCount(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/notifications/GetUnreadNotificationCount")
}

// NotificationGetUserSettings returns the current user's notification settings.
func (c *Client) NotificationGetUserSettings(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/notifications/GetUserNotificationSettings")
}

// NotificationSaveUserSettings updates the current user's notification settings.
// body is the freeform settings payload. LIVE MUTATION.
func (c *Client) NotificationSaveUserSettings(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/notifications/SaveUserNotificationSettings", body)
}

// NotificationCloseAll dismisses all of the current user's notifications.
// Modeled upstream as a GET. LIVE MUTATION.
func (c *Client) NotificationCloseAll(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/notifications/CloseAllNotifications")
}

// NotificationCloseUser dismisses one user notification by its record id.
// Modeled upstream as a GET. LIVE MUTATION.
func (c *Client) NotificationCloseUser(ctx context.Context, recordID int) (RawJSON, error) {
	return c.externalGet(ctx, "/notifications/CloseUserNotification/"+strconv.Itoa(recordID))
}

// NotificationCloseSystem dismisses one system notification by its record id.
// Modeled upstream as a GET. LIVE MUTATION.
func (c *Client) NotificationCloseSystem(ctx context.Context, recordID int) (RawJSON, error) {
	return c.externalGet(ctx, "/notifications/CloseSystemNotification/"+strconv.Itoa(recordID))
}
