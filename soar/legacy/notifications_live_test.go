package legacy

import "testing"

// TestLiveNotificationsReads exercises the notification-center read endpoints
// (safe, read-only): the current user's notifications, unread count, and
// per-user notification settings. All are zero-argument and succeed on a tenant
// with no prior setup. Runs under SECOPS_SOAR_SMOKE=1.
//
// The mutating endpoints in this tag are intentionally excluded: settings
// save (a per-user/auth-adjacent mutation) and the dismiss/close operations
// (NotificationCloseAll is a destructive bulk dismiss; CloseUser/CloseSystem
// need a live record id and mutate). No safe cosmetic config resource exists
// here, so there is no CRUD test.
func TestLiveNotificationsReads(t *testing.T) {
	lc, ctx := liveClient(t)
	readProbe(t, "notifications/GetUserNotifications", func() (RawJSON, error) { return lc.NotificationListUser(ctx) })
	readProbe(t, "notifications/GetUnreadNotificationCount", func() (RawJSON, error) { return lc.NotificationGetUnreadCount(ctx) })
	readProbe(t, "notifications/GetUserNotificationSettings", func() (RawJSON, error) { return lc.NotificationGetUserSettings(ctx) })
}
