package soar

import (
	"context"
	"encoding/json"
	"strconv"
)

func casePath(caseID int, sub string) string {
	return "cases/" + strconv.Itoa(caseID) + "/" + sub
}

// ListCustomFieldValues returns the custom field values for a case.
func (c *Client) ListCustomFieldValues(ctx context.Context, caseID int) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", casePath(caseID, "customFieldValues"), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListCaseWallRecords returns the case wall timeline entries.
func (c *Client) ListCaseWallRecords(ctx context.Context, caseID int) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", casePath(caseID, "caseWallRecords"), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListContextProperties returns the case-level key-value context properties.
func (c *Client) ListContextProperties(ctx context.Context, caseID int) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "GET", casePath(caseID, "contextProperties"), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetContextProperty creates or updates a case context property.
func (c *Client) SetContextProperty(ctx context.Context, caseID int, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.t.V1Alpha(ctx, "POST", casePath(caseID, "contextProperties"), body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
