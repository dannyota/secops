package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
)

// GenerateUdmKeyValueMappings takes a base64-encoded raw log and a log format
// (e.g. "JSON", "XML", "SYSLOG") and returns the UDM field mappings the
// ingestion pipeline would extract. Useful for building or previewing parser
// extractors without a full parser run.
//
// Endpoint: POST {instance}:generateUdmKeyValueMappings (instance-level
// custom method, project ID form — numeric=false).
func (c *Client) GenerateUdmKeyValueMappings(ctx context.Context, log, logFormat string) (map[string]string, error) {
	body := struct {
		LogFormat               string `json:"logFormat"`
		Log                     string `json:"log"`
		UseArrayBracketNotation bool   `json:"useArrayBracketNotation"`
		CompressArrayFields     bool   `json:"compressArrayFields"`
	}{
		LogFormat:               logFormat,
		Log:                     log,
		UseArrayBracketNotation: true,
		CompressArrayFields:     true,
	}
	var resp struct {
		FieldMappings map[string]string `json:"fieldMappings"`
	}
	path := c.instancePath(false) + ":generateUdmKeyValueMappings"
	if err := c.post(ctx, path, body, &resp); err != nil {
		return nil, err
	}
	return resp.FieldMappings, nil
}

// UpdateLogTypeSetting updates the per-log-type ingestion configuration (e.g.
// extractor settings). body is the raw JSON patch body; the server returns the
// updated LogTypeSetting.
//
// Endpoint: PATCH {instance}/logTypes/{logType}/logTypeSetting (project ID
// form — numeric=false, matching GetLogTypeSetting in schemas.go).
func (c *Client) UpdateLogTypeSetting(ctx context.Context, logType string, body json.RawMessage) (*LogTypeSetting, error) {
	sub := "logTypes/" + url.PathEscape(lastSegment(logType)) + "/logTypeSetting"
	var out LogTypeSetting
	if err := c.patch(ctx, c.resourcePath(sub, false), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
