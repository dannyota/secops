package cli

import (
	"encoding/json"

	"danny.vn/secops/docs/tips"
)

func (s *mcpSession) handleResourceRead(req jrpcRequest) jrpcResponse {
	var params struct {
		URI string `json:"uri"`
	}
	_ = json.Unmarshal(req.Params, &params)

	body, ok := s.resCont[params.URI]
	if !ok {
		return jrpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &jrpcError{Code: -32602, Message: "unknown resource: " + params.URI},
		}
	}
	return jrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
		"contents": []map[string]string{
			{"uri": params.URI, "mimeType": "text/markdown", "text": body},
		},
	}}
}

func mcpResourcesFromTips() ([]mcpResource, map[string]string) {
	entries := tips.All()
	resources := make([]mcpResource, 0, len(entries))
	content := make(map[string]string, len(entries))

	for _, e := range entries {
		uri := "tips://" + e.Name
		resources = append(resources, mcpResource{
			URI:         uri,
			Name:        e.Title,
			Description: e.Title,
			MimeType:    "text/markdown",
		})
		content[uri] = e.Content
	}
	return resources, content
}
