// LEGACY tier: the Siemplify external API (/api/external/v1) Case Management
// surface, casechat sub-tree.
//
// Case chat is the per-case messaging thread: analysts post messages, pin and
// unpin them, and exchange attachments. These endpoints read the thread (with
// pagination and search), post new messages, toggle pins, and fetch attachment
// bytes/previews. This is the reliable external-API path for case chat.
//
// Shapes are the deeply-nested, schema-unstable legacy payloads, so reads return
// json.RawMessage and writes take a freeform body (the caller supplies/decodes
// only the fields it needs). All methods speak the AppKey-authenticated external
// API via c.t.External.
package legacy

import (
	"context"
	"net/url"
	"strconv"
)

// CaseChatList returns the chat thread for one case. The optional query
// parameters (pagesize, page, startMessageId, includeStartMessage, searchTerm)
// page and filter the messages; pass an empty url.Values for defaults.
func (c *Client) CaseChatList(ctx context.Context, caseID int, q url.Values) (RawJSON, error) {
	path := "/casechat/" + strconv.Itoa(caseID)
	if len(q) == 0 {
		return c.externalGet(ctx, path)
	}
	return c.externalGetQuery(ctx, path, q)
}

// CaseChatNewMessagesCount returns the count of new (unread) chat messages for
// one case.
func (c *Client) CaseChatNewMessagesCount(ctx context.Context, caseID int) (RawJSON, error) {
	return c.externalGet(ctx, "/casechat/"+strconv.Itoa(caseID)+"/new-messages-count")
}

// CaseChatGetAttachment returns the bytes/metadata of one chat attachment.
func (c *Client) CaseChatGetAttachment(ctx context.Context, attachmentID int) (RawJSON, error) {
	return c.externalGet(ctx, "/casechat/attachments/"+strconv.Itoa(attachmentID))
}

// CaseChatGetAttachmentPreview returns a preview of one chat attachment.
func (c *Client) CaseChatGetAttachmentPreview(ctx context.Context, attachmentID int) (RawJSON, error) {
	return c.externalGet(ctx, "/casechat/attachments/preview/"+strconv.Itoa(attachmentID))
}

// CaseChatPost posts a new chat message to one case. body is the freeform
// message payload. LIVE MUTATION.
func (c *Client) CaseChatPost(ctx context.Context, caseID int, body any) (RawJSON, error) {
	return c.externalPost(ctx, "/casechat/"+strconv.Itoa(caseID), body)
}

// CaseChatPinMessage pins one chat message. LIVE MUTATION.
func (c *Client) CaseChatPinMessage(ctx context.Context, messageID int) (RawJSON, error) {
	return c.externalPost(ctx, "/casechat/pin/"+strconv.Itoa(messageID), nil)
}

// CaseChatUnpinMessage unpins one chat message. LIVE MUTATION.
func (c *Client) CaseChatUnpinMessage(ctx context.Context, messageID int) (RawJSON, error) {
	return c.externalPost(ctx, "/casechat/unpin/"+strconv.Itoa(messageID), nil)
}
