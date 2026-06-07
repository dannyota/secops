package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// DataTaps stream normalized/filtered UDM events to a Cloud Pub/Sub topic. The
// modern surface lives on the chronicle host under the instance path
// (projects/.../instances/.../dataTaps), full CRUD — the older Backstory
// (`backstory.googleapis.com/v1/dataTaps`) endpoint is superseded by this. The id
// is server-assigned on create. Project ID (string) form (numeric=false).
//
// Prerequisite: grant the Pub/Sub Publisher role to
// publisher@chronicle-data-tap.iam.gserviceaccount.com on the target topic.

// DataTapFilter selects which events a tap publishes.
type DataTapFilter string

const (
	DataTapAllEvents     DataTapFilter = "ALL_UDM_EVENTS"     // every normalized event
	DataTapAlertEvents   DataTapFilter = "ALERT_UDM_EVENTS"   // significant alerts only
	DataTapLabeledEvents DataTapFilter = "LABELED_UDM_EVENTS" // events tagged by labeling rules
)

// DataTapSerialization is the output wire format.
type DataTapSerialization string

const (
	DataTapProto DataTapSerialization = "MARSHALLED_PROTO" // marshalled proto (default)
	DataTapJSON  DataTapSerialization = "JSON_OBJECT"      // JSON object
)

// CloudPubSubSink is the Pub/Sub destination of a tap.
type CloudPubSubSink struct {
	Topic string `json:"topic,omitempty"` // projects/{project}/topics/{topic}
}

// DataTap streams events to a sink. Name is the server-assigned resource name.
type DataTap struct {
	Name                string               `json:"name,omitempty"`
	DisplayName         string               `json:"displayName,omitempty"`
	Filter              DataTapFilter        `json:"filter,omitempty"`
	SerializationFormat DataTapSerialization `json:"serializationFormat,omitempty"`
	CloudPubsubSink     *CloudPubSubSink     `json:"cloudPubsubSink,omitempty"`
	Raw                 json.RawMessage      `json:"-"`
}

// UnmarshalJSON decodes the typed fields and retains the full payload in Raw.
func (d *DataTap) UnmarshalJSON(data []byte) error {
	type alias DataTap
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*d = DataTap(a)
	d.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// ID returns the trailing resource-id segment of the tap's Name.
func (d *DataTap) ID() string {
	if d == nil {
		return ""
	}
	return lastSegment(d.Name)
}

// ListDataTaps returns every data tap in the instance, paginating over nextPageToken.
func (c *Client) ListDataTaps(ctx context.Context) ([]DataTap, error) {
	var all []DataTap
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"100"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			DataTaps      []DataTap `json:"dataTaps"`
			NextPageToken string    `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath("dataTaps", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.DataTaps...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetDataTap fetches a single data tap by its <id> segment.
func (c *Client) GetDataTap(ctx context.Context, id string) (*DataTap, error) {
	var d DataTap
	if err := c.get(ctx, c.resourcePath("dataTaps/"+url.PathEscape(lastSegment(id)), false), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// CreateDataTap creates a data tap. The id is server-assigned; the created
// resource (with its name) is returned.
func (c *Client) CreateDataTap(ctx context.Context, tap DataTap) (*DataTap, error) {
	tap.Name = "" // server-assigned
	var d DataTap
	if err := c.post(ctx, c.resourcePath("dataTaps", false), tap, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// UpdateDataTap patches a data tap, sending body under updateMask. An empty mask
// defaults to the operator-editable field set.
//
// NOTE: the v1alpha PATCH is documented but returns 501 UNIMPLEMENTED on the
// current backend; the reconcile surface therefore does an update as
// delete-old + create-new. This method is kept for when the backend implements it.
func (c *Client) UpdateDataTap(ctx context.Context, id string, body json.RawMessage, mask []string) (*DataTap, error) {
	if len(mask) == 0 {
		mask = []string{"display_name", "filter", "serialization_format", "cloud_pubsub_sink"}
	}
	q := url.Values{"updateMask": {strings.Join(mask, ",")}}
	var d DataTap
	if err := c.patch(ctx, c.resourcePath("dataTaps/"+url.PathEscape(lastSegment(id)), false), body, &d, withQuery(q)); err != nil {
		return nil, err
	}
	return &d, nil
}

// DeleteDataTap deletes a data tap by its <id> segment.
func (c *Client) DeleteDataTap(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", c.resourcePath("dataTaps/"+url.PathEscape(lastSegment(id)), false), nil, nil)
}
