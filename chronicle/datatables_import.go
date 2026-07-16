package chronicle

import (
	"context"
	"fmt"
)

// Bulk row operations for data tables: create/update/replace/delete rows with
// batching. Table-level CRUD lives in datatables_write.go; shared types
// (DataTable, DataTableColumn, DataTableRow) live in datatables.go.

// Row-batching limits, mirroring the wrapper: at most 1000 rows per bulk call,
// with a per-request byte budget. Create/replace use a 4MB budget; update uses
// 2MB.
const (
	maxRowsPerBatch        = 1000
	maxCreateBatchBytes    = 4_000_000
	maxUpdateBatchBytes    = 2_000_000
	jsonRowStructureBudget = 30 // approx bytes for {"data_table_row":{"values":[...]}}
)

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

// batchBy splits items into batches of at most maxRowsPerBatch items that each
// stay within maxBytes as measured by sizeOf (a single oversized item is
// emitted alone so the caller can report it).
func batchBy[T any](items []T, maxBytes int, sizeOf func(T) int) [][]T {
	var batches [][]T
	cur := make([]T, 0, maxRowsPerBatch)
	var curBytes int
	for _, it := range items {
		b := sizeOf(it)
		if len(cur) > 0 && (len(cur) >= maxRowsPerBatch || curBytes+b > maxBytes) {
			batches = append(batches, cur)
			cur, curBytes = make([]T, 0, maxRowsPerBatch), 0
		}
		cur = append(cur, it)
		curBytes += b
	}
	if len(cur) > 0 {
		batches = append(batches, cur)
	}
	return batches
}

// batchRows splits rows into size-bounded batches.
func batchRows(rows [][]string, maxBytes int) [][][]string {
	return batchBy(rows, maxBytes, estimateRowBytes)
}

// batchRowUpdates is batchRows for RowUpdate, sizing by each update's Values.
func batchRowUpdates(updates []RowUpdate, maxBytes int) [][]RowUpdate {
	return batchBy(updates, maxBytes, func(u RowUpdate) int { return estimateRowBytes(u.Values) })
}
