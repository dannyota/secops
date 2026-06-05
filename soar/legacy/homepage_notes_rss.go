// LEGACY tier: the Siemplify external API (/api/external/v1) Homepage surface —
// the notes and RSS-feed panels of the analyst homepage. See homepage.go for the
// package overview and conventions.
package legacy

import (
	"context"
	"net/http"
	"strconv"
)

// HomepageListNotes returns a paged list of homepage note widgets.
func (c *Client) HomepageListNotes(ctx context.Context, requestedPage, pageSize int, searchTerm string) (RawJSON, error) {
	return c.externalGetQuery(ctx, "/homepage/notes/GetByRequest", homepagePageQuery(requestedPage, pageSize, searchTerm))
}

// HomepageGetNote returns one homepage note widget by id.
func (c *Client) HomepageGetNote(ctx context.Context, id int) (RawJSON, error) {
	return c.externalGet(ctx, "/homepage/notes/"+strconv.Itoa(id))
}

// HomepageDeleteNote deletes one homepage note widget by id. LIVE MUTATION; this
// cannot be undone.
func (c *Client) HomepageDeleteNote(ctx context.Context, id int) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/homepage/notes/"+strconv.Itoa(id), nil)
}

// HomepageCreateNote adds a homepage note widget. body is the freeform legacy
// payload. LIVE MUTATION.
func (c *Client) HomepageCreateNote(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/homepage/notes", body)
}

// HomepageUpdateNote updates a homepage note widget. body is the freeform legacy
// payload. LIVE MUTATION.
func (c *Client) HomepageUpdateNote(ctx context.Context, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/homepage/notes", body)
}

// HomepageListRss returns a paged list of homepage RSS-feed widgets.
func (c *Client) HomepageListRss(ctx context.Context, requestedPage, pageSize int, searchTerm string) (RawJSON, error) {
	return c.externalGetQuery(ctx, "/homepage/rss/GetByRequest", homepagePageQuery(requestedPage, pageSize, searchTerm))
}

// HomepageGetRss returns one homepage RSS-feed widget by id.
func (c *Client) HomepageGetRss(ctx context.Context, id int) (RawJSON, error) {
	return c.externalGet(ctx, "/homepage/rss/"+strconv.Itoa(id))
}

// HomepageDeleteRss deletes one homepage RSS-feed widget by id. LIVE MUTATION;
// this cannot be undone.
func (c *Client) HomepageDeleteRss(ctx context.Context, id int) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/homepage/rss/"+strconv.Itoa(id), nil)
}

// HomepageCreateRss adds a homepage RSS-feed widget. body is the freeform legacy
// payload. LIVE MUTATION.
func (c *Client) HomepageCreateRss(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/homepage/rss", body)
}

// HomepageUpdateRss updates a homepage RSS-feed widget. body is the freeform
// legacy payload. LIVE MUTATION.
func (c *Client) HomepageUpdateRss(ctx context.Context, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/homepage/rss", body)
}

// HomepageGetRssCount returns the count of homepage RSS-feed widgets.
func (c *Client) HomepageGetRssCount(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/homepage/rss/GetRssCount")
}
