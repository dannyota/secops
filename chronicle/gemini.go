package chronicle

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// Chronicle's Gemini (Duet AI) conversational interface. Every endpoint here is
// rooted at the instance path built from the string project ID (numeric=false),
// matching the wrapper, which appends each path after {base_url}/{instance_id}.

// Block is a discrete chunk of content in a Gemini response: text, code, HTML,
// or any future block kind. Kind mirrors the API's blockType ("TEXT", "CODE",
// "HTML", …); Content is the extracted text/code/HTML for that block.
//
// DEVIATION: the wrapper exposes typed accessors (get_code_blocks, etc.) but
// drops every block kind it doesn't special-case. We keep the kind on each
// block and preserve the raw block JSON, so callers can filter and recover
// anything the SDK doesn't model.
type Block struct {
	Kind    string          `json:"kind,omitempty"`    // blockType: TEXT, CODE, HTML, …
	Content string          `json:"content,omitempty"` // extracted text/code/HTML
	Title   string          `json:"title,omitempty"`   // present on some CODE blocks
	Raw     json.RawMessage `json:"-"`                 // original block object
}

// NavigationAction is the navigation target of a NAVIGATION suggested action.
type NavigationAction struct {
	TargetURI string `json:"targetUri,omitempty"`
}

// SuggestedAction is a follow-up the model proposes (e.g. a UI navigation).
type SuggestedAction struct {
	DisplayText string            `json:"displayText,omitempty"`
	ActionType  string            `json:"actionType,omitempty"` // e.g. "NAVIGATION"
	UseCaseID   string            `json:"useCaseId,omitempty"`
	Navigation  *NavigationAction `json:"navigation,omitempty"`
}

// GeminiResponse is a parsed Gemini conversation reply: ordered content blocks
// plus suggested actions, reference blocks, and grounding strings.
type GeminiResponse struct {
	Name             string            `json:"name,omitempty"`       // full message resource path
	InputQuery       string            `json:"inputQuery,omitempty"` // the question that was asked
	CreateTime       string            `json:"createTime,omitempty"`
	Blocks           []Block           `json:"blocks,omitempty"`
	SuggestedActions []SuggestedAction `json:"suggestedActions,omitempty"`
	References       []Block           `json:"references,omitempty"`
	Groundings       []string          `json:"groundings,omitempty"`
	Raw              json.RawMessage   `json:"-"` // full unprocessed API response
}

// TextContent concatenates the content of every TEXT block (the model's prose),
// joined by blank lines. Convenience for the common "just give me the answer"
// path; CodeBlocks/HTMLBlocks expose the rest.
func (r *GeminiResponse) TextContent() string {
	var parts []string
	for i := range r.Blocks {
		if r.Blocks[i].Kind == "TEXT" {
			parts = append(parts, r.Blocks[i].Content)
		}
	}
	return strings.Join(parts, "\n\n")
}

// CodeBlocks returns the CODE blocks in order.
func (r *GeminiResponse) CodeBlocks() []Block { return r.blocksOf("CODE") }

// HTMLBlocks returns the HTML blocks in order.
func (r *GeminiResponse) HTMLBlocks() []Block { return r.blocksOf("HTML") }

func (r *GeminiResponse) blocksOf(kind string) []Block {
	var out []Block
	for i := range r.Blocks {
		if r.Blocks[i].Kind == kind {
			out = append(out, r.Blocks[i])
		}
	}
	return out
}

// ErrGeminiOptInRequired is returned by QueryGemini when the instance reports
// that the user must opt in to Gemini before chatting. Recover by calling
// OptInToGemini, then retrying the query.
//
// DEVIATION: the wrapper auto-opts-in and silently retries inside query_gemini,
// mutating the client and swallowing the signal. We surface a sentinel error so
// the caller decides whether to flip a user-level preference, and keep QueryGemini
// side-effect-free. Pair with errors.Is(err, ErrGeminiOptInRequired).
var ErrGeminiOptInRequired = errors.New("chronicle: user must opt in to Gemini before using it (call OptInToGemini)")

// --- wire structs (the raw API shapes we decode) ---------------------------

type geminiHTMLContent struct {
	Value string `json:"privateDoNotAccessOrElseSafeHtmlWrappedValue,omitempty"`
}

type geminiRawBlock struct {
	BlockType   string             `json:"blockType,omitempty"`
	Content     string             `json:"content,omitempty"`
	Title       string             `json:"title,omitempty"`
	HTMLContent *geminiHTMLContent `json:"htmlContent,omitempty"`
}

type geminiRawAction struct {
	DisplayText string `json:"displayText,omitempty"`
	ActionType  string `json:"actionType,omitempty"`
	UseCaseID   string `json:"useCaseId,omitempty"`
	Navigation  *struct {
		TargetURI string `json:"targetUri,omitempty"`
	} `json:"navigation,omitempty"`
}

type geminiRawResponseItem struct {
	Blocks           []json.RawMessage `json:"blocks,omitempty"`
	References       []json.RawMessage `json:"references,omitempty"`
	Groundings       []string          `json:"groundings,omitempty"`
	SuggestedActions []geminiRawAction `json:"suggestedActions,omitempty"`
}

type geminiRawMessage struct {
	Name       string `json:"name,omitempty"`
	CreateTime string `json:"createTime,omitempty"`
	Input      *struct {
		Body string `json:"body,omitempty"`
	} `json:"input,omitempty"`
	Responses []geminiRawResponseItem `json:"responses,omitempty"`
}

// --- API methods -----------------------------------------------------------

// QueryGemini asks Gemini a question and returns the parsed reply. If
// conversationID is empty a fresh conversation is created first; otherwise the
// message is appended to the existing conversation (so callers can thread a
// multi-turn chat by reusing the ID from a prior response's Name).
//
// On an opt-in-required failure it returns ErrGeminiOptInRequired (wrapping the
// underlying *APIError); call OptInToGemini and retry.
func (c *Client) QueryGemini(ctx context.Context, question, conversationID string) (*GeminiResponse, error) {
	if conversationID == "" {
		id, err := c.createGeminiConversation(ctx, "New chat")
		if err != nil {
			return nil, err
		}
		conversationID = id
	}

	// DEVIATION: the wrapper hard-codes context.uri="/search" with an empty
	// body; we keep that default since the chat surface expects it.
	body := map[string]any{
		"input": map[string]any{
			"body": question,
			"context": map[string]any{
				"uri":  "/search",
				"body": map[string]any{},
			},
		},
	}

	path := c.resourcePath("users/me/conversations/"+url.PathEscape(conversationID)+"/messages", false)
	var raw json.RawMessage
	if err := c.post(ctx, path, body, &raw); err != nil {
		if isGeminiOptInRequired(err) {
			return nil, errors.Join(ErrGeminiOptInRequired, err)
		}
		return nil, err
	}
	return parseGeminiResponse(raw)
}

// OptInToGemini flips the calling user's preference to enable Duet AI chat,
// which is a prerequisite for QueryGemini. It is a no-op-safe idempotent PATCH.
//
// DEVIATION: the wrapper treats 401/403 as "expected" and returns False (warn +
// swallow). We let an authorization failure surface as *APIError — a caller that
// can't change its own preference should hear about it, not silently proceed.
func (c *Client) OptInToGemini(ctx context.Context) error {
	body := map[string]any{
		"ui_preferences": map[string]any{
			"enable_duet_ai_chat": true,
		},
	}
	q := url.Values{"updateMask": {"ui_preferences.enable_duet_ai_chat"}}
	path := c.resourcePath("users/me/preferenceSet", false)
	return c.patch(ctx, path, body, nil, withQuery(q))
}

// createGeminiConversation starts a new conversation and returns its trailing ID.
func (c *Client) createGeminiConversation(ctx context.Context, displayName string) (string, error) {
	if displayName == "" {
		displayName = "New chat"
	}
	body := map[string]any{"displayName": displayName}
	var resp struct {
		Name string `json:"name"`
	}
	if err := c.post(ctx, c.resourcePath("users/me/conversations", false), body, &resp); err != nil {
		return "", err
	}
	if i := strings.LastIndex(resp.Name, "/"); i >= 0 {
		return resp.Name[i+1:], nil
	}
	return resp.Name, nil
}

// --- parsing ---------------------------------------------------------------

// parseGeminiResponse flattens the nested responses[].blocks/references/actions
// of a message reply into a single typed GeminiResponse, preserving each block's
// raw JSON.
func parseGeminiResponse(raw json.RawMessage) (*GeminiResponse, error) {
	var msg geminiRawMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}

	out := &GeminiResponse{
		Name:       msg.Name,
		CreateTime: msg.CreateTime,
		Raw:        raw,
	}
	if msg.Input != nil {
		out.InputQuery = msg.Input.Body
	}

	for _, item := range msg.Responses {
		for _, rb := range item.Blocks {
			out.Blocks = append(out.Blocks, decodeBlock(rb))
		}
		for _, rr := range item.References {
			out.References = append(out.References, decodeBlock(rr))
		}
		out.Groundings = append(out.Groundings, item.Groundings...)
		for _, a := range item.SuggestedActions {
			sa := SuggestedAction{
				DisplayText: a.DisplayText,
				ActionType:  a.ActionType,
				UseCaseID:   a.UseCaseID,
			}
			if a.Navigation != nil {
				sa.Navigation = &NavigationAction{TargetURI: a.Navigation.TargetURI}
			}
			out.SuggestedActions = append(out.SuggestedActions, sa)
		}
	}
	return out, nil
}

// decodeBlock turns one raw block object into a typed Block, pulling HTML out of
// its safe-HTML wrapper and keeping the original JSON on Raw.
func decodeBlock(raw json.RawMessage) Block {
	var rb geminiRawBlock
	_ = json.Unmarshal(raw, &rb) // best-effort; Raw retains the original
	b := Block{Kind: rb.BlockType, Title: rb.Title, Raw: raw}
	switch rb.BlockType {
	case "HTML":
		if rb.HTMLContent != nil {
			b.Content = rb.HTMLContent.Value
		}
	default: // TEXT, CODE, and anything else carry a plain content string
		b.Content = rb.Content
	}
	return b
}

// isGeminiOptInRequired reports whether err is the instance telling us the user
// has not yet opted in to Gemini. The signal lives in the API error body.
func isGeminiOptInRequired(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	body := strings.ToLower(apiErr.Body)
	return strings.Contains(body, "users must opt-in before using gemini") ||
		strings.Contains(body, "must opt-in before using gemini") ||
		strings.Contains(body, "opt-in before using gemini")
}
