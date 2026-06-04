package chronicle

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Write-side data table operations: create/get/update/delete tables, and
// bulk create/update/replace/delete of their rows. Reuses DataTable,
// DataTableColumn, and DataTableRow from datatables.go.
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

// Row-batching limits, mirroring the wrapper: at most 1000 rows per bulk call,
// with a per-request byte budget. Create/replace use a 4MB budget; update uses
// 2MB.
const (
	maxRowsPerBatch        = 1000
	maxCreateBatchBytes    = 4_000_000
	maxUpdateBatchBytes    = 2_000_000
	jsonRowStructureBudget = 30 // approx bytes for {"data_table_row":{"values":[...]}}
)

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

// DataTableRowBatch is the response from one bulk row operation. The returned
// rows carry server-assigned resource Names.
type DataTableRowBatch struct {
	DataTableRows []DataTableRow `json:"dataTableRows,omitempty"`
}

// rowCreateRequest is one entry of a bulkCreate request body. The wrapper uses
// the snake_case "data_table_row" key here, which we preserve.
type rowCreateRequest struct {
	DataTableRow rowValues `json:"data_table_row"`
}

type rowValues struct {
	Values []string `json:"values"`
}

// CreateDataTableRows appends rows to a data table, batching by the wrapper's
// limits (<=1000 rows and <=4MB per request). It returns one batch response per
// underlying bulkCreate call.
func (c *Client) CreateDataTableRows(ctx context.Context, name string, rows [][]string) ([]DataTableRowBatch, error) {
	var out []DataTableRowBatch
	for _, batch := range batchRows(rows, maxCreateBatchBytes) {
		if len(batch) == 1 && estimateBatchBytes(batch) > maxCreateBatchBytes {
			return out, fmt.Errorf("chronicle: single row too large to process (>%d bytes)", maxCreateBatchBytes)
		}
		reqs := make([]rowCreateRequest, len(batch))
		for i, r := range batch {
			reqs[i] = rowCreateRequest{DataTableRow: rowValues{Values: r}}
		}
		body := struct {
			Requests []rowCreateRequest `json:"requests"`
		}{Requests: reqs}

		var resp DataTableRowBatch
		path := c.resourcePath("dataTables/"+name+"/dataTableRows:bulkCreate", false)
		if err := c.post(ctx, path, body, &resp); err != nil {
			return out, err
		}
		out = append(out, resp)
	}
	return out, nil
}

// ReplaceDataTableRows replaces all existing rows in a data table with rows.
//
// The first batch (up to 1000 rows / 4MB) is sent via bulkReplace, which clears
// the table and inserts that batch; any remaining rows are appended via
// bulkCreate. An empty rows slice is a no-op.
//
// DEVIATION: the wrapper prints progress to stdout; an SDK must not, so this is
// silent.
func (c *Client) ReplaceDataTableRows(ctx context.Context, name string, rows [][]string) ([]DataTableRowBatch, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	for i, r := range rows {
		if estimateRowBytes(r) > maxCreateBatchBytes {
			return nil, fmt.Errorf("chronicle: row %d too large to process (>%d bytes)", i, maxCreateBatchBytes)
		}
	}

	// Build the first bulkReplace batch: up to 1000 rows, bounded by 4MB.
	var firstBatch [][]string
	var firstBytes int
	limit := min(maxRowsPerBatch, len(rows))
	for _, r := range rows[:limit] {
		rb := estimateRowBytes(r)
		if len(firstBatch) > 0 && firstBytes+rb > maxCreateBatchBytes {
			break
		}
		firstBatch = append(firstBatch, r)
		firstBytes += rb
	}

	var out []DataTableRowBatch
	if len(firstBatch) > 0 {
		reqs := make([]rowCreateRequest, len(firstBatch))
		for i, r := range firstBatch {
			reqs[i] = rowCreateRequest{DataTableRow: rowValues{Values: r}}
		}
		body := struct {
			Requests []rowCreateRequest `json:"requests"`
		}{Requests: reqs}

		var resp DataTableRowBatch
		path := c.resourcePath("dataTables/"+name+"/dataTableRows:bulkReplace", false)
		if err := c.post(ctx, path, body, &resp); err != nil {
			return out, err
		}
		out = append(out, resp)
	}

	// Append everything not covered by the first bulkReplace via bulkCreate.
	remaining := append([][]string{}, rows[len(firstBatch):]...)
	if len(remaining) > 0 {
		batches, err := c.CreateDataTableRows(ctx, name, remaining)
		out = append(out, batches...)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// RowUpdate is a single-row update for UpdateDataTableRows. Name is the full
// resource name of the row to update; Values are the new values; UpdateMask is
// an optional comma-separated field mask.
type RowUpdate struct {
	Name       string
	Values     []string
	UpdateMask string // optional, e.g. "values"
}

// rowUpdateRequest is one entry of a bulkUpdate request body. Note the
// camelCase "dataTableRow" key (the wrapper uses snake_case for create/replace
// but camelCase for update — we mirror that exactly).
type rowUpdateRequest struct {
	DataTableRow rowUpdateRow `json:"dataTableRow"`
	UpdateMask   string       `json:"updateMask,omitempty"`
}

type rowUpdateRow struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// UpdateDataTableRows updates existing rows in bulk, batching by the wrapper's
// update limits (<=1000 rows and <=2MB per request). Each update must carry a
// Name and Values.
func (c *Client) UpdateDataTableRows(ctx context.Context, name string, updates []RowUpdate) ([]DataTableRowBatch, error) {
	for i, u := range updates {
		if u.Name == "" {
			return nil, fmt.Errorf("chronicle: row update %d missing name", i)
		}
		if u.Values == nil {
			return nil, fmt.Errorf("chronicle: row update %d missing values", i)
		}
	}

	var out []DataTableRowBatch
	for _, batch := range batchRowUpdates(updates, maxUpdateBatchBytes) {
		if len(batch) == 1 && estimateRowBytes(batch[0].Values) > maxUpdateBatchBytes {
			return out, fmt.Errorf("chronicle: single row too large to process (>%d bytes)", maxUpdateBatchBytes)
		}
		reqs := make([]rowUpdateRequest, len(batch))
		for i, u := range batch {
			reqs[i] = rowUpdateRequest{
				DataTableRow: rowUpdateRow{Name: u.Name, Values: u.Values},
				UpdateMask:   u.UpdateMask,
			}
		}
		body := struct {
			Requests []rowUpdateRequest `json:"requests"`
		}{Requests: reqs}

		var resp DataTableRowBatch
		path := c.resourcePath("dataTables/"+name+"/dataTableRows:bulkUpdate", false)
		if err := c.post(ctx, path, body, &resp); err != nil {
			return out, err
		}
		out = append(out, resp)
	}
	return out, nil
}

// DeleteDataTableRows deletes rows by ID from a data table. rowIDs are the
// trailing GUID segments of each row's resource name. Each row is deleted with
// its own request (the API exposes no bulk delete). On the first failure the
// error is returned along with the IDs deleted so far.
//
// DEVIATION: the wrapper returns a list of per-row response dicts; we return the
// slice of IDs successfully deleted, which is what a caller actually needs to
// reconcile state, and surface the first error instead of continuing blindly.
func (c *Client) DeleteDataTableRows(ctx context.Context, name string, rowIDs []string) ([]string, error) {
	var deleted []string
	for _, id := range rowIDs {
		path := c.resourcePath("dataTables/"+name+"/dataTableRows/"+id, false)
		if err := c.do(ctx, "DELETE", path, nil, nil); err != nil {
			return deleted, err
		}
		deleted = append(deleted, id)
	}
	return deleted, nil
}

// --- batching helpers -------------------------------------------------------

// estimateRowBytes approximates the JSON-encoded size of one row's values,
// mirroring the wrapper's _estimate_row_json_size: a fixed structural budget
// plus each value's length, quotes, and ~10% escaping overhead.
func estimateRowBytes(values []string) int {
	size := jsonRowStructureBudget
	for _, v := range values {
		size += len(v) + 3 + len(v)/10
	}
	return size
}

// estimateBatchBytes sums the estimated size of every row in a batch.
func estimateBatchBytes(batch [][]string) int {
	total := 0
	for _, r := range batch {
		total += estimateRowBytes(r)
	}
	return total
}

// batchRows splits rows into batches of at most maxRowsPerBatch rows that each
// stay within maxBytes (a single oversized row is emitted alone so the caller
// can report it).
func batchRows(rows [][]string, maxBytes int) [][][]string {
	var batches [][][]string
	cur := make([][]string, 0, maxRowsPerBatch)
	var curBytes int
	for _, r := range rows {
		rb := estimateRowBytes(r)
		if len(cur) > 0 && (len(cur) >= maxRowsPerBatch || curBytes+rb > maxBytes) {
			batches = append(batches, cur)
			cur, curBytes = make([][]string, 0, maxRowsPerBatch), 0
		}
		cur = append(cur, r)
		curBytes += rb
	}
	if len(cur) > 0 {
		batches = append(batches, cur)
	}
	return batches
}

// batchRowUpdates is batchRows for RowUpdate, sizing by each update's Values.
func batchRowUpdates(updates []RowUpdate, maxBytes int) [][]RowUpdate {
	var batches [][]RowUpdate
	cur := make([]RowUpdate, 0, maxRowsPerBatch)
	var curBytes int
	for _, u := range updates {
		rb := estimateRowBytes(u.Values)
		if len(cur) > 0 && (len(cur) >= maxRowsPerBatch || curBytes+rb > maxBytes) {
			batches = append(batches, cur)
			cur, curBytes = make([]RowUpdate, 0, maxRowsPerBatch), 0
		}
		cur = append(cur, u)
		curBytes += rb
	}
	if len(cur) > 0 {
		batches = append(batches, cur)
	}
	return batches
}
