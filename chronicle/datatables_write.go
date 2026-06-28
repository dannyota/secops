package chronicle

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Write-side data table operations: create/get/update/delete tables. Bulk row
// operations (create/update/replace/delete) live in datatables_import.go.
// Reuses DataTable, DataTableColumn, and DataTableRow from datatables.go.
//
// Like the read side, every endpoint here lives under the project-ID (string)
// form of the instance path (numeric=false) — the wrapper builds all of these
// from the string project_id.

// DataTableColumnType is the type of a data table column, referenced from
// YARA-L rules. One of STRING, REGEX, CIDR, NUMBER (per the wrapper's
// DataTableColumnType StrEnum).
type DataTableColumnType string

const (
	ColumnTypeString DataTableColumnType = "STRING"
	ColumnTypeRegex  DataTableColumnType = "REGEX"
	ColumnTypeCIDR   DataTableColumnType = "CIDR"
	ColumnTypeNumber DataTableColumnType = "NUMBER"
)

// dataTableNameRE mirrors the wrapper's REF_LIST_DATA_TABLE_ID_REGEX: a name
// must start with a letter, contain only letters/digits/underscores, and be at
// most 255 chars.
var dataTableNameRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,254}$`)

// validateDataTableName enforces the server-side ID constraints up front so a
// bad name fails locally with a clear message instead of a generic 400.
func validateDataTableName(name string) error {
	if !dataTableNameRE.MatchString(name) {
		return fmt.Errorf("chronicle: invalid data table name %q: must start with a letter, "+
			"contain only letters, numbers, and underscores, and be < 256 characters", name)
	}
	return nil
}

// CreateColumn describes one column to create. Exactly one of Type or
// MappedColumnPath should be set: Type for a literal column (STRING/REGEX/CIDR/
// NUMBER), MappedColumnPath for an entity-proto field mapping. ColumnType and
// mappedColumnPath are mutually exclusive on the wire.
type CreateColumn struct {
	Name             string              // the originalColumn name
	Type             DataTableColumnType // set for a typed column
	MappedColumnPath string              // set for an entity field mapping
}

// columnInfoBody is the per-column request shape (subset of the wrapper's
// columnInfo dict). columnType and mappedColumnPath are mutually exclusive.
type columnInfoBody struct {
	ColumnIndex      int    `json:"columnIndex"`
	OriginalColumn   string `json:"originalColumn"`
	ColumnType       string `json:"columnType,omitempty"`
	MappedColumnPath string `json:"mappedColumnPath,omitempty"`
}

type scopeInfoBody struct {
	DataAccessScopes []string `json:"dataAccessScopes,omitempty"`
}

type createDataTableBody struct {
	Description string           `json:"description,omitempty"`
	ColumnInfo  []columnInfoBody `json:"columnInfo"`
	ScopeInfo   *scopeInfoBody   `json:"scopeInfo,omitempty"`
}

// CreateDataTableResult is the created DataTable plus, if rows were supplied,
// the row-creation outcome.
//
// DEVIATION: the wrapper stuffs "rowCreationResponses"/"rowCreationError" into
// the returned dict and swallows row failures. We return the typed table and
// the row batches separately, and surface any row-creation error instead of
// hiding it in a string field — but we still return the created table so the
// caller can decide whether a partial create is acceptable.
type CreateDataTableResult struct {
	Table        *DataTable
	RowResponses []DataTableRowBatch
	RowError     error // non-nil if rows were supplied but failed to create
}

// CreateDataTable creates a data table and, if rows are supplied, populates it.
//
// columns are written in slice order (columnIndex = position). rows is a list
// of positional value slices aligned to columns. CIDR columns are validated
// locally before the call. If row creation fails after the table is created,
// the error is returned in RowError (the table itself is still returned).
func (c *Client) CreateDataTable(
	ctx context.Context,
	name, description string,
	columns []CreateColumn,
	rows [][]string,
	scopes ...string,
) (*CreateDataTableResult, error) {
	if err := validateDataTableName(name); err != nil {
		return nil, err
	}

	body := createDataTableBody{Description: description}
	for i, col := range columns {
		cb := columnInfoBody{ColumnIndex: i, OriginalColumn: col.Name}
		switch {
		case col.MappedColumnPath != "":
			cb.MappedColumnPath = col.MappedColumnPath
		default:
			cb.ColumnType = string(col.Type)
		}
		body.ColumnInfo = append(body.ColumnInfo, cb)
	}
	if len(scopes) > 0 {
		body.ScopeInfo = &scopeInfoBody{DataAccessScopes: scopes}
	}

	// Validate CIDR columns locally (the wrapper does the same) so an invalid
	// entry fails fast rather than partway through row creation.
	if err := validateCIDRColumns(columns, rows); err != nil {
		return nil, err
	}

	q := url.Values{"dataTableId": {name}}
	var table DataTable
	if err := c.post(ctx, c.resourcePath("dataTables", false), body, &table, withQuery(q)); err != nil {
		return nil, err
	}

	res := &CreateDataTableResult{Table: &table}
	if len(rows) > 0 {
		batches, err := c.CreateDataTableRows(ctx, name, rows)
		res.RowResponses = batches
		res.RowError = err // surfaced, not swallowed; table is still returned
	}
	return res, nil
}

// validateCIDRColumns checks that every value in a CIDR-typed column parses as
// CIDR notation, mirroring the wrapper's validate_cidr_entries.
func validateCIDRColumns(columns []CreateColumn, rows [][]string) error {
	for i, col := range columns {
		if col.Type != ColumnTypeCIDR {
			continue
		}
		for _, row := range rows {
			if i >= len(row) {
				continue
			}
			entry := row[i]
			if entry == "" {
				continue
			}
			if _, _, err := net.ParseCIDR(entry); err != nil {
				// Accept a bare IP as well: net.ParseCIDR rejects host
				// addresses without a prefix, but those are valid entries.
				if net.ParseIP(entry) == nil {
					return fmt.Errorf("chronicle: invalid CIDR entry %q in column %q", entry, col.Name)
				}
			}
		}
	}
	return nil
}

// GetDataTable fetches a single data table by ID (the trailing path segment of
// DataTable.Name, e.g. "my_table").
func (c *Client) GetDataTable(ctx context.Context, name string) (*DataTable, error) {
	var dt DataTable
	if err := c.get(ctx, c.resourcePath("dataTables/"+name, false), &dt); err != nil {
		return nil, err
	}
	return &dt, nil
}

// DataTableUpdate is a partial update to a data table. Only the non-nil fields
// are sent, and the updateMask is derived from exactly those fields. RowTimeToLive
// is a duration string (e.g. "86400s") for the row TTL.
type DataTableUpdate struct {
	Description   *string // nil leaves description unchanged
	RowTimeToLive *string // nil leaves the row TTL unchanged
}

type updateDataTableBody struct {
	Description   string `json:"description,omitempty"`
	RowTimeToLive string `json:"row_time_to_live,omitempty"`
}

// UpdateDataTable patches a data table, sending only the fields set on upd and
// an updateMask covering exactly those fields.
//
// DEVIATION: the wrapper omits an empty body but lets the server infer the mask
// when none is given; we always build the mask from the set fields so the body
// and mask never drift (matching UpdateRuleDeployment).
func (c *Client) UpdateDataTable(ctx context.Context, name string, upd DataTableUpdate) (*DataTable, error) {
	if err := validateDataTableName(name); err != nil {
		return nil, err
	}

	var body updateDataTableBody
	var mask []string
	if upd.Description != nil {
		body.Description = *upd.Description
		mask = append(mask, "description")
	}
	if upd.RowTimeToLive != nil {
		body.RowTimeToLive = *upd.RowTimeToLive
		mask = append(mask, "row_time_to_live")
	}
	if len(mask) == 0 {
		return nil, &APIError{
			Method: "PATCH",
			URL:    c.resourcePath("dataTables/"+name, false),
			Body:   "no data table fields provided to update",
		}
	}

	q := url.Values{"updateMask": {strings.Join(mask, ",")}}
	var dt DataTable
	if err := c.patch(ctx, c.resourcePath("dataTables/"+name, false), body, &dt, withQuery(q)); err != nil {
		return nil, err
	}
	return &dt, nil
}

// DeleteDataTable deletes a data table. When force is true, any rows under the
// table are deleted too; otherwise the request only succeeds if the table is
// empty.
//
// DEVIATION: the wrapper catches the APIError and returns {} on failure,
// silently hiding a failed delete. We surface the *APIError so callers know the
// delete did not happen.
func (c *Client) DeleteDataTable(ctx context.Context, name string, force bool) error {
	q := url.Values{"force": {boolParam(force)}}
	return c.do(ctx, "DELETE", c.resourcePath("dataTables/"+name, false), nil, nil, withQuery(q))
}

// boolParam renders a bool as the lowercase string the API expects.
func boolParam(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
