package chronicle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Watchlists use the project ID (string) form in their resource path
// (numeric=false): the wrapper builds the watchlists collection from the string
// project_id instance path. See resource.go for why the form is explicit per
// endpoint.

// watchlistsAPIVersion (entity watchlists, reads and writes) is pinned in versions.go.

// Watchlist is a named set of entities whose risk scores are weighted by
// MultiplyingFactor — used to elevate (or suppress) the risk of assets/users an
// analyst is tracking.
//
// Name is the watchlist resource, projects/.../watchlists/<id>; WatchlistID
// returns the trailing <id> segment. EntityPopulationMechanism and
// WatchlistUserPreferences are freeform nested blobs (the API shapes them as
// {"manual": {}} and {"pinned": true} respectively today) kept as raw JSON so
// callers don't branch on a schema the server may extend.
type Watchlist struct {
	Name                      string          `json:"name,omitempty"`
	DisplayName               string          `json:"displayName,omitempty"`
	Description               string          `json:"description,omitempty"`
	MultiplyingFactor         float64         `json:"multiplyingFactor,omitempty"`
	EntityCount               json.RawMessage `json:"entityCount,omitempty"`
	EntityPopulationMechanism json.RawMessage `json:"entityPopulationMechanism,omitempty"`
	WatchlistUserPreferences  json.RawMessage `json:"watchlistUserPreferences,omitempty"`
	CreateTime                string          `json:"createTime,omitempty"`
	UpdateTime                string          `json:"updateTime,omitempty"`
	Etag                      string          `json:"etag,omitempty"`
}

// WatchlistID returns the trailing <id> segment of the watchlist's resource
// Name, the identifier Get/Update/DeleteWatchlist expect.
func (w *Watchlist) WatchlistID() string {
	if w == nil || w.Name == "" {
		return ""
	}
	if i := strings.LastIndex(w.Name, "/watchlists/"); i >= 0 {
		return w.Name[i+len("/watchlists/"):]
	}
	return w.Name[strings.LastIndex(w.Name, "/")+1:]
}

// ListWatchlists returns every watchlist in the instance. pageSize <= 0 lets the
// server choose the page size; pagination is handled transparently across pages.
func (c *Client) ListWatchlists(ctx context.Context, pageSize int) ([]Watchlist, error) {
	var all []Watchlist
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{}
		if pageSize > 0 {
			q.Set("pageSize", strconv.Itoa(pageSize))
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			Watchlists    []Watchlist `json:"watchlists"`
			NextPageToken string      `json:"nextPageToken"`
		}
		opts := []requestOption{withVersion(watchlistsAPIVersion)}
		if len(q) > 0 {
			opts = append(opts, withQuery(q))
		}
		if err := c.get(ctx, c.resourcePath("watchlists", false), &resp, opts...); err != nil {
			return "", err
		}
		all = append(all, resp.Watchlists...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// GetWatchlist fetches a single watchlist by ID (the trailing segment of its
// resource name).
func (c *Client) GetWatchlist(ctx context.Context, id string) (*Watchlist, error) {
	var w Watchlist
	if err := c.get(ctx, c.resourcePath("watchlists/"+id, false), &w, withVersion(watchlistsAPIVersion)); err != nil {
		return nil, err
	}
	return &w, nil
}

// CreateWatchlist creates a watchlist with manual entity population.
//
// multiplyingFactor weights the risk score of entities in the watchlist
// (1.0 = unchanged). description is optional ("" omits it). The wrapper always
// seeds entityPopulationMechanism with {"manual": {}}; we do the same so the
// created watchlist is immediately usable for manual entity assignment.
func (c *Client) CreateWatchlist(ctx context.Context, name, displayName, description string, multiplyingFactor float64) (*Watchlist, error) {
	body := struct {
		Name                      string          `json:"name,omitempty"`
		DisplayName               string          `json:"displayName"`
		Description               string          `json:"description,omitempty"`
		MultiplyingFactor         float64         `json:"multiplyingFactor"`
		EntityPopulationMechanism json.RawMessage `json:"entityPopulationMechanism"`
	}{
		Name:                      name,
		DisplayName:               displayName,
		Description:               description,
		MultiplyingFactor:         multiplyingFactor,
		EntityPopulationMechanism: json.RawMessage(`{"manual":{}}`),
	}
	var w Watchlist
	if err := c.post(ctx, c.resourcePath("watchlists", false), body, &w, withVersion(watchlistsAPIVersion)); err != nil {
		return nil, err
	}
	return &w, nil
}

// WatchlistUpdate is a partial update to a watchlist. Only the set fields are
// sent, and the updateMask is derived from exactly those fields so body and mask
// never drift.
//
// Pinned controls the watchlistUserPreferences.pinned flag (whether the
// watchlist is pinned in the UI). All pointer fields are nil-to-skip; an empty
// non-nil string description clears the description.
type WatchlistUpdate struct {
	DisplayName       *string  // nil leaves display name unchanged
	Description       *string  // nil leaves description unchanged
	MultiplyingFactor *float64 // nil leaves multiplying factor unchanged
	Pinned            *bool    // nil leaves user preferences unchanged
}

// UpdateWatchlist patches a watchlist, sending only the fields set on upd and an
// updateMask covering exactly those fields.
//
// DEVIATION: the wrapper takes raw entity_population_mechanism /
// watchlist_user_preferences dicts and an optional caller-supplied update_mask.
// We expose the one user-preference that matters in practice (Pinned) as a typed
// bool and always derive the mask from the set fields, so there is no way to send
// a body and mask that disagree. The mask uses the snake_case field paths the API
// expects (display_name, multiplying_factor, watchlist_user_preferences).
// Updating entity_population_mechanism is intentionally unsupported here (a
// watchlist's population mechanism is fixed at create time in practice); set it
// via CreateWatchlist.
func (c *Client) UpdateWatchlist(ctx context.Context, id string, upd WatchlistUpdate) (*Watchlist, error) {
	body := struct {
		DisplayName              *string         `json:"displayName,omitempty"`
		Description              *string         `json:"description,omitempty"`
		MultiplyingFactor        *float64        `json:"multiplyingFactor,omitempty"`
		WatchlistUserPreferences json.RawMessage `json:"watchlistUserPreferences,omitempty"`
	}{}
	var mask []string

	if upd.DisplayName != nil {
		// Pointer so a deliberate empty-string rename serializes as
		// "displayName":"" and matches the mask (no body/mask drift).
		body.DisplayName = upd.DisplayName
		mask = append(mask, "display_name")
	}
	if upd.Description != nil {
		body.Description = upd.Description
		mask = append(mask, "description")
	}
	if upd.MultiplyingFactor != nil {
		body.MultiplyingFactor = upd.MultiplyingFactor
		mask = append(mask, "multiplying_factor")
	}
	if upd.Pinned != nil {
		prefs, err := json.Marshal(struct {
			Pinned bool `json:"pinned"`
		}{Pinned: *upd.Pinned})
		if err != nil {
			return nil, err
		}
		body.WatchlistUserPreferences = prefs
		mask = append(mask, "watchlist_user_preferences")
	}

	if len(mask) == 0 {
		return nil, &APIError{
			Method: "PATCH",
			URL:    c.resourcePath("watchlists/"+id, false),
			Status: 0,
			Body:   "no watchlist fields provided to update",
		}
	}

	q := url.Values{"updateMask": {strings.Join(mask, ",")}}
	var w Watchlist
	if err := c.patch(ctx, c.resourcePath("watchlists/"+id, false), body, &w, withQuery(q), withVersion(watchlistsAPIVersion)); err != nil {
		return nil, err
	}
	return &w, nil
}

// DeleteWatchlist deletes a watchlist by ID. When force is true, any entities
// still assigned to the watchlist are deleted along with it; when false the API
// rejects deletion of a non-empty watchlist.
func (c *Client) DeleteWatchlist(ctx context.Context, id string, force bool) error {
	opts := []requestOption{withVersion(watchlistsAPIVersion)}
	if force {
		opts = append(opts, withQuery(url.Values{"force": {"true"}}))
	}
	return c.do(ctx, "DELETE", c.resourcePath("watchlists/"+id, false), nil, nil, opts...)
}

// WatchlistEntity builds the Entity body for the watchlist membership verbs.
// Exactly ONE selector field is set per the API contract (asset ip/mac/
// hostname, or user userid/email/employee id/SID); Namespace is optional.
type WatchlistEntity struct {
	Asset map[string]any `json:"asset,omitempty"`
	User  map[string]any `json:"user,omitempty"`

	Namespace string `json:"namespace,omitempty"`
}

// AddWatchlistEntity puts one entity on a watchlist (entities:add) — a
// standard containment/tracking response action during investigation.
// LIVE MUTATION; watchlist membership also feeds risk-score multipliers.
//
// The request's entity field is the UDM Entity ENVELOPE ({metadata, entity:
// <Noun>, …}) — the asset/user selector sits on its inner Noun, one level
// below the envelope. The response echoes the stored Entity; its name field
// is what RemoveWatchlistEntity takes. The entities sub-resource is
// documented on v1alpha; the parent watchlists pin (v1) does not apply to it.
func (c *Client) AddWatchlistEntity(ctx context.Context, watchlistID string, entity WatchlistEntity) (json.RawMessage, error) {
	if strings.TrimSpace(watchlistID) == "" {
		return nil, fmt.Errorf("chronicle: watchlistID is required")
	}
	var body struct {
		Entity struct {
			Entity WatchlistEntity `json:"entity"`
		} `json:"entity"`
	}
	body.Entity.Entity = entity
	var out json.RawMessage
	path := c.resourcePath("watchlists/"+watchlistID+"/entities:add", false)
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RemoveWatchlistEntity removes one entity from a watchlist by the entity's
// full resource name (…/watchlists/{w}/entities/{e} — the add response's
// name field). RPC entities.remove. LIVE MUTATION.
func (c *Client) RemoveWatchlistEntity(ctx context.Context, entityName string) error {
	entityName = strings.TrimPrefix(strings.TrimSpace(entityName), "/")
	if entityName == "" {
		return fmt.Errorf("chronicle: the entity resource name is required")
	}
	return c.post(ctx, entityName+":remove", struct{}{}, nil)
}

// BatchRemoveWatchlistEntities removes entities from a watchlist
// (entities:batchRemove). body is the documented request payload, kept
// freeform until a live run confirms its exact shape. LIVE MUTATION.
func (c *Client) BatchRemoveWatchlistEntities(ctx context.Context, watchlistID string, body any) (json.RawMessage, error) {
	if strings.TrimSpace(watchlistID) == "" {
		return nil, fmt.Errorf("chronicle: watchlistID is required")
	}
	var out json.RawMessage
	path := c.resourcePath("watchlists/"+watchlistID+"/entities:batchRemove", false)
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
