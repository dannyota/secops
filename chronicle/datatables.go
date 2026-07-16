package chronicle

import (
	"context"
	"encoding/json"
	"net/url"
)

// Data tables are named, columnar lookup tables referenced from YARA-L rules.
// They live under the project-ID (string) form of the instance path.

// DataTableColumn describes one column of a data table.
//
// The named fields cover the common shape; column entries can also carry
// columnType, mappedColumnPath, and other freeform options the API may add, so
// Raw preserves the full original JSON object for callers that need it (e.g. a
// faithful mirror round-trip).
type DataTableColumn struct {
	OriginalColumn string `json:"originalColumn,omitempty"`
	ColumnIndex    int    `json:"columnIndex"`
	Name           string `json:"name,omitempty"`
	// ColumnType is the value type for a plain column (one of the
	// DataTableColumnType* enums). It is mutually exclusive with MappedColumnPath.
	ColumnType string `json:"columnType,omitempty"`
	// MappedColumnPath maps the column to an entity-graph field path instead of a
	// value type; mutually exclusive with ColumnType.
	MappedColumnPath string `json:"mappedColumnPath,omitempty"`
	// Raw is the column object exactly as returned by the API. It is not part
	// of the JSON contract itself (json:"-"); UnmarshalJSON fills it.
	Raw json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the known column fields and also retains the complete
// raw object so freeform/unmodeled fields survive a round-trip.
//
// DEVIATION: the official Python wrapper returns columns as bare dicts; we keep
// a typed view for the common fields while still preserving everything via Raw.
func (col *DataTableColumn) UnmarshalJSON(data []byte) error {
	type alias DataTableColumn // avoid recursion
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*col = DataTableColumn(a)
	col.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// DataTable is a Chronicle data table resource.
type DataTable struct {
	Name        string            `json:"name,omitempty"`
	DisplayName string            `json:"displayName,omitempty"`
	Description string            `json:"description,omitempty"`
	CreateTime  string            `json:"createTime,omitempty"`
	UpdateTime  string            `json:"updateTime,omitempty"`
	ColumnInfo  []DataTableColumn `json:"columnInfo,omitempty"`
}

// DataTableRow is a single row of a data table. Values is positional, aligned
// with the table's ColumnInfo order.
type DataTableRow struct {
	Name   string   `json:"name,omitempty"`
	Values []string `json:"values"`
}

// ListDataTables returns every data table in the instance.
func (c *Client) ListDataTables(ctx context.Context) ([]DataTable, error) {
	var all []DataTable
	err := paginate(50, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			DataTables    []DataTable `json:"dataTables"`
			NextPageToken string      `json:"nextPageToken"`
		}
		// DEVIATION: data tables take the project-ID (string) form, not the
		// numeric project number — encoded explicitly here (numeric=false).
		if err := c.get(ctx, c.resourcePath("dataTables", false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.DataTables...)
		return resp.NextPageToken, nil
	})
	return all, err
}

// ListDataTableRows returns every row of the data table identified by tableID,
// which is the last path segment of DataTable.Name (the display-name-derived
// table ID, e.g. "my_table").
func (c *Client) ListDataTableRows(ctx context.Context, tableID string) ([]DataTableRow, error) {
	sub := "dataTables/" + tableID + "/dataTableRows"
	var all []DataTableRow
	err := paginate(1000, func(token string) (string, error) {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var resp struct {
			DataTableRows []DataTableRow `json:"dataTableRows"`
			NextPageToken string         `json:"nextPageToken"`
		}
		if err := c.get(ctx, c.resourcePath(sub, false), &resp, withQuery(q)); err != nil {
			return "", err
		}
		all = append(all, resp.DataTableRows...)
		return resp.NextPageToken, nil
	})
	return all, err
}
