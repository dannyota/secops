package cli

import (
	"encoding/json"

	"danny.vn/secops/docs/guides"
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

func mcpResourcesFromEmbedded() ([]mcpResource, map[string]string) {
	tipEntries := tips.All()
	guideEntries := guides.All()

	total := len(tipEntries) + len(guideEntries)
	resources := make([]mcpResource, 0, total)
	content := make(map[string]string, total)

	for _, e := range tipEntries {
		uri := "tips://" + e.Name
		resources = append(resources, mcpResource{
			URI:         uri,
			Name:        e.Title,
			Description: e.Title,
			MimeType:    "text/markdown",
		})
		content[uri] = e.Content
	}

	for _, e := range guideEntries {
		uri := "guide://" + e.Name
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
