package soar

import (
	"context"
	"encoding/json"
	"strconv"
)

// CaseChatList returns chat messages for a case via the v1alpha cases resource.
func (c *Client) CaseChatList(ctx context.Context, caseID int) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", "cases/"+strconv.Itoa(caseID)+"/chatMessages", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CaseChatSend creates a new chat message on a case.
func (c *Client) CaseChatSend(ctx context.Context, caseID int, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", "cases/"+strconv.Itoa(caseID)+"/chatMessages", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CaseChatUnreadCount returns the unread message count for a case.
func (c *Client) CaseChatUnreadCount(ctx context.Context, caseID int) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", "cases/"+strconv.Itoa(caseID)+"/chatMessages:unreadMessagesCount", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
