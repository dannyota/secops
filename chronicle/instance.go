package chronicle

import "context"

// Instance identifies a Chronicle SIEM instance.
//
// The v1 instances.get response currently exposes only the resource name.
type Instance struct {
	Name string `json:"name,omitempty"`
}

// GetInstance returns the Chronicle SIEM instance configured on the client.
// It is a single, non-paginated request to the stable v1 endpoint.
func (c *Client) GetInstance(ctx context.Context) (*Instance, error) {
	var out Instance
	if err := c.get(ctx, c.instancePath(false), &out, withVersion("v1")); err != nil {
		return nil, err
	}
	return &out, nil
}
