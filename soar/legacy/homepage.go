// LEGACY tier: the Siemplify external API (/api/external/v1) Homepage surface.
//
// The Homepage is the analyst landing dashboard. These endpoints back its
// widgets: the homepage case list/count plus the user-curated panels —
// attachments, contacts, links, notes, and RSS feeds. Each panel is a small CRUD
// surface (list-by-request, get/delete by integer id, create via POST, update via
// PUT). They predate the modern v1alpha model and are kept here until it covers
// them.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). The list endpoints page via RequestedPage/PageSize
// and an optional SearchTerm. All methods speak the AppKey-authenticated external
// API via c.t.External.
package legacy

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// homepagePageQuery builds the standard RequestedPage/PageSize/SearchTerm query
// shared by the Homepage "GetByRequest" list endpoints.
func homepagePageQuery(requestedPage, pageSize int, searchTerm string) url.Values {
	q := url.Values{}
	q.Set("RequestedPage", strconv.Itoa(requestedPage))
	q.Set("PageSize", strconv.Itoa(pageSize))
	q.Set("SearchTerm", searchTerm)
	return q
}

// HomepageListCases returns the homepage case list for a filtered/paged request.
// extra carries the optional Filters/SlaFilters/PriorityFilters/SortBy query
// values; pass nil for none.
//
// Deprecated: the /cases/homepagecases endpoints return a server-side HTTP 500
// (errorCode 2000) — the homepage-cases feature appears to be
// unprovisioned or broken on the backend, not in this SDK. The request is
// well-formed; the server errors. Kept for surface completeness; do not rely on
// it. Use the case-queue / search endpoints instead.
func (c *Client) HomepageListCases(ctx context.Context, requestedPage, pageSize int, searchTerm string, extra url.Values) (RawJSON, error) {
	q := homepagePageQuery(requestedPage, pageSize, searchTerm)
	for k, vs := range extra {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	return c.externalGetQuery(ctx, "/cases/homepagecases/GetByRequest", q)
}

// HomepageGetCasesCount returns the count of cases shown on the homepage.
//
// Deprecated: the /cases/homepagecases endpoints return a server-side HTTP 500
// (errorCode 2000) — the homepage-cases feature appears to be
// unprovisioned or broken on the backend, not in this SDK. The request is
// well-formed; the server errors. Kept for surface completeness; do not rely on
// it. Use GetCaseExists / the case-queue endpoints instead.
func (c *Client) HomepageGetCasesCount(ctx context.Context) (RawJSON, error) {
	return c.externalGet(ctx, "/cases/homepagecases/GetHomepageCasesCount")
}

// HomepageListAttachments returns a paged list of homepage attachment widgets.
func (c *Client) HomepageListAttachments(ctx context.Context, requestedPage, pageSize int, searchTerm string) (RawJSON, error) {
	return c.externalGetQuery(ctx, "/homepage/attachments/GetByRequest", homepagePageQuery(requestedPage, pageSize, searchTerm))
}

// HomepageGetAttachment returns one homepage attachment widget by id.
func (c *Client) HomepageGetAttachment(ctx context.Context, id int) (RawJSON, error) {
	return c.externalGet(ctx, "/homepage/attachments/"+strconv.Itoa(id))
}

// HomepageDeleteAttachment deletes one homepage attachment widget by id. LIVE
// MUTATION; this cannot be undone.
func (c *Client) HomepageDeleteAttachment(ctx context.Context, id int) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/homepage/attachments/"+strconv.Itoa(id), nil)
}

// HomepageCreateAttachment adds a homepage attachment widget. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) HomepageCreateAttachment(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/homepage/attachments", body)
}

// HomepageUpdateAttachment updates a homepage attachment widget. body is the
// freeform legacy payload. LIVE MUTATION.
func (c *Client) HomepageUpdateAttachment(ctx context.Context, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/homepage/attachments", body)
}

// HomepageListContacts returns a paged list of homepage contact widgets.
func (c *Client) HomepageListContacts(ctx context.Context, requestedPage, pageSize int, searchTerm string) (RawJSON, error) {
	return c.externalGetQuery(ctx, "/homepage/contacts/GetByRequest", homepagePageQuery(requestedPage, pageSize, searchTerm))
}

// HomepageGetContact returns one homepage contact widget by id.
func (c *Client) HomepageGetContact(ctx context.Context, id int) (RawJSON, error) {
	return c.externalGet(ctx, "/homepage/contacts/"+strconv.Itoa(id))
}

// HomepageDeleteContact deletes one homepage contact widget by id. LIVE
// MUTATION; this cannot be undone.
func (c *Client) HomepageDeleteContact(ctx context.Context, id int) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/homepage/contacts/"+strconv.Itoa(id), nil)
}

// HomepageCreateContact adds a homepage contact widget. body is the freeform
// legacy payload. LIVE MUTATION.
func (c *Client) HomepageCreateContact(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/homepage/contacts", body)
}

// HomepageUpdateContact updates a homepage contact widget. body is the freeform
// legacy payload. LIVE MUTATION.
func (c *Client) HomepageUpdateContact(ctx context.Context, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/homepage/contacts", body)
}

// HomepageListLinks returns a paged list of homepage link widgets.
func (c *Client) HomepageListLinks(ctx context.Context, requestedPage, pageSize int, searchTerm string) (RawJSON, error) {
	return c.externalGetQuery(ctx, "/homepage/links/GetByRequest", homepagePageQuery(requestedPage, pageSize, searchTerm))
}

// HomepageGetLink returns one homepage link widget by id.
func (c *Client) HomepageGetLink(ctx context.Context, id int) (RawJSON, error) {
	return c.externalGet(ctx, "/homepage/links/"+strconv.Itoa(id))
}

// HomepageDeleteLink deletes one homepage link widget by id. LIVE MUTATION; this
// cannot be undone.
func (c *Client) HomepageDeleteLink(ctx context.Context, id int) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodDelete, "/homepage/links/"+strconv.Itoa(id), nil)
}

// HomepageCreateLink adds a homepage link widget. body is the freeform legacy
// payload. LIVE MUTATION.
func (c *Client) HomepageCreateLink(ctx context.Context, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/homepage/links", body)
}

// HomepageUpdateLink updates a homepage link widget. body is the freeform legacy
// payload. LIVE MUTATION.
func (c *Client) HomepageUpdateLink(ctx context.Context, body any) (RawJSON, error) {
	return c.externalDo(ctx, http.MethodPut, "/homepage/links", body)
}
