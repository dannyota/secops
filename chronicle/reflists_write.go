package chronicle

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
)

// Reference-list syntax-type enums (REFERENCE_LIST_SYNTAX_TYPE_*). These pick
// how the server validates each entry (content line): plain-text patterns,
// regular expressions, or CIDR ranges.
const (
	RefListSyntaxString = "REFERENCE_LIST_SYNTAX_TYPE_PLAIN_TEXT_STRING"
	RefListSyntaxRegex  = "REFERENCE_LIST_SYNTAX_TYPE_REGEX"
	RefListSyntaxCIDR   = "REFERENCE_LIST_SYNTAX_TYPE_CIDR"
)

// refListIDRegex mirrors the wrapper's REF_LIST_DATA_TABLE_ID_REGEX: a
// reference-list ID must start with a letter and contain only letters, digits,
// and underscores, with total length < 256.
var refListIDRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,254}$`)

// validateRefListID rejects an ID the server would reject, with a clear local
// error rather than a 400 round-trip.
func validateRefListID(id string) error {
	if !refListIDRegex.MatchString(id) {
		return fmt.Errorf("chronicle: invalid reference list name %q: must start "+
			"with a letter, contain only letters/digits/underscores, and be < 256 chars", id)
	}
	return nil
}

// validateCIDREntries rejects any entry that is not valid CIDR notation,
// mirroring the wrapper's validate_cidr_entries (which uses ipaddress with
// strict=False — host bits allowed). Empty input is a no-op.
//
// DEVIATION: netip.ParsePrefix is strict about host bits, so we fall back to
// ParseAddr to accept bare addresses and to ParsePrefix's masked form, matching
// Python's strict=False leniency.
func validateCIDREntries(entries []string) error {
	for _, e := range entries {
		if _, err := netip.ParsePrefix(e); err == nil {
			continue
		}
		if _, err := netip.ParseAddr(e); err == nil {
			continue
		}
		return fmt.Errorf("chronicle: invalid CIDR entry: %q", e)
	}
	return nil
}

// toRefListEntries wraps plain strings as ReferenceListEntry content lines.
func toRefListEntries(values []string) []ReferenceListEntry {
	entries := make([]ReferenceListEntry, len(values))
	for i, v := range values {
		entries[i] = ReferenceListEntry{Value: v}
	}
	return entries
}

// refListCreateRequest is the POST body for CreateReferenceList. The server
// reads displayName from the referenceListId query param, not the body.
type refListCreateRequest struct {
	Description string               `json:"description"`
	Entries     []ReferenceListEntry `json:"entries"`
	SyntaxType  string               `json:"syntaxType"`
}

// CreateReferenceList creates a reference list whose ID and display name are
// displayName. entries become the list's content lines; syntaxType is one of
// the RefListSyntax* enums (defaults to plain-text string when empty). When the
// syntax type is CIDR, entries are validated locally before the request.
//
// Reference lists use the project ID form (numeric=false). The created list is
// returned as the server stored it.
func (c *Client) CreateReferenceList(ctx context.Context, displayName, description string, entries []string, syntaxType string) (*ReferenceList, error) {
	if err := validateRefListID(displayName); err != nil {
		return nil, err
	}
	if syntaxType == "" {
		syntaxType = RefListSyntaxString
	}
	if syntaxType == RefListSyntaxCIDR {
		if err := validateCIDREntries(entries); err != nil {
			return nil, err
		}
	}

	body := refListCreateRequest{
		Description: description,
		Entries:     toRefListEntries(entries),
		SyntaxType:  syntaxType,
	}
	q := url.Values{"referenceListId": {displayName}}

	var rl ReferenceList
	if err := c.post(ctx, c.resourcePath("referenceLists", false), body, &rl, withQuery(q)); err != nil {
		return nil, err
	}
	rl.Name = c.canonicalRefListName(rl.Name)
	return &rl, nil
}

// GetReferenceList fetches a single reference list by name (its short ID or a
// full resource name; only the trailing segment is used). When full is true the
// FULL view is requested (metadata + content lines + associated rule counts);
// otherwise the BASIC view (metadata only) is requested.
//
// DEVIATION: the wrapper always sends a view (defaulting to FULL); we expose a
// bool so callers can take the cheaper BASIC view when entries aren't needed.
func (c *Client) GetReferenceList(ctx context.Context, name string, full bool) (*ReferenceList, error) {
	view := refListViewBasic
	if full {
		view = refListViewFull
	}
	q := url.Values{"view": {view}}

	id := refListShortID(name)
	var rl ReferenceList
	if err := c.get(ctx, c.resourcePath("referenceLists/"+id, false), &rl, withQuery(q)); err != nil {
		return nil, err
	}
	rl.Name = c.canonicalRefListName(rl.Name)
	return &rl, nil
}

// refListUpdateRequest is the PATCH body for UpdateReferenceList. Both fields
// are optional; only those named in the updateMask are applied.
type refListUpdateRequest struct {
	Description *string               `json:"description,omitempty"`
	Entries     *[]ReferenceListEntry `json:"entries,omitempty"`
}

// UpdateReferenceList replaces a reference list's description and/or entries.
// Entries replace the existing content lines wholesale (not a merge). Pass
// entries=nil to leave entries untouched; pass an empty non-nil slice to clear
// them. The updateMask is derived from which fields are supplied, so an
// unintended field is never overwritten.
//
// At least one of description/entries must change. When the target list has
// CIDR syntax, new entries are validated locally first; this costs one GET to
// read the stored syntaxType, exactly as the wrapper does.
func (c *Client) UpdateReferenceList(ctx context.Context, name, description string, entries []string) (*ReferenceList, error) {
	setDescription := description != ""
	setEntries := entries != nil
	if !setDescription && !setEntries {
		return nil, fmt.Errorf("chronicle: UpdateReferenceList: provide a description, entries, or both")
	}

	id := refListShortID(name)

	if setEntries {
		// CIDR validation needs the stored syntax type.
		existing, err := c.GetReferenceList(ctx, id, false)
		if err != nil {
			return nil, err
		}
		if existing.SyntaxType == RefListSyntaxCIDR {
			if err := validateCIDREntries(entries); err != nil {
				return nil, err
			}
		}
	}

	var body refListUpdateRequest
	var mask []string
	if setDescription {
		body.Description = &description
		mask = append(mask, "description")
	}
	if setEntries {
		// A pointer to the (possibly empty) slice: an empty non-nil slice
		// serializes as "entries": [] (the clear-entries contract), while a
		// description-only update leaves Entries nil and omits the key.
		e := toRefListEntries(entries)
		body.Entries = &e
		mask = append(mask, "entries")
	}
	q := url.Values{"updateMask": {strings.Join(mask, ",")}}

	var rl ReferenceList
	if err := c.patch(ctx, c.resourcePath("referenceLists/"+id, false), body, &rl, withQuery(q)); err != nil {
		return nil, err
	}
	rl.Name = c.canonicalRefListName(rl.Name)
	return &rl, nil
}

// refListShortID returns the trailing ID segment of name, tolerating either a
// bare ID ("my_list") or a full resource name
// (".../referenceLists/my_list").
func refListShortID(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// canonicalRefListName rebuilds a reference-list resource name in the project-ID
// form this SDK uses (numeric=false), from whatever name the server echoed.
//
// DEVIATION: the server is inconsistent about the project segment — Create echoes
// the project NUMBER in the returned name while List echoes the project ID. Left
// as-is, a freshly created list and the same list seen via List carry different
// resource names, so any consumer that keys identity on the name (the reconcile
// engine) treats them as two objects. Normalizing every returned name to the
// id form makes the identity stable. An empty name (never created) stays empty.
func (c *Client) canonicalRefListName(name string) string {
	if name == "" {
		return ""
	}
	return c.resourcePath("referenceLists/"+refListShortID(name), false)
}
