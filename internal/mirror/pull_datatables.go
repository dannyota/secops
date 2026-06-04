package mirror

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"danny.vn/secops/chronicle"
)

// maxDataTableRows caps how many rows are written to a table's CSV mirror so a
// runaway table can't produce an unbounded file.
const maxDataTableRows = 100_000

// PullDataTables snapshots every data table in the instance into outDir.
//
// Per table it derives tableID from the last path segment of Name (falling back
// to displayName), then writes <slug>.csv (a header row from the column info
// followed by each row's positional Values, capped at maxDataTableRows) and
// <slug>.yaml (display name, name, description, the raw columns, row count, and
// timestamps). Returns the number of tables written.
func PullDataTables(ctx context.Context, c *chronicle.Client, outDir string) (int, error) {
	if _, err := EnsureDir(outDir); err != nil {
		return 0, err
	}

	tables, err := c.ListDataTables(ctx)
	if err != nil {
		return 0, err
	}

	written := 0
	for _, tbl := range tables {
		display := tbl.DisplayName
		if display == "" {
			display = lastSegment(tbl.Name)
		}
		slug := Slugify(display)

		// The rows endpoint is keyed by the table ID (the last path segment of
		// Name), not the display name. Fall back to displayName only when Name
		// is absent.
		tableID := lastSegment(tbl.Name)
		if tableID == "" {
			tableID = display
		}

		rows, err := c.ListDataTableRows(ctx, tableID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  (warn) list rows for %s: %v\n", tableID, err)
			rows = nil
		}

		if err := writeDataTableCSV(filepath.Join(outDir, slug+".csv"), tbl.ColumnInfo, rows); err != nil {
			return written, err
		}

		meta := dataTableMeta{
			DisplayName: display,
			Name:        tbl.Name,
			Description: tbl.Description,
			Columns:     rawColumns(tbl.ColumnInfo),
			RowCount:    len(rows),
			CreateTime:  tbl.CreateTime,
			UpdateTime:  tbl.UpdateTime,
		}
		if err := writeYAML(filepath.Join(outDir, slug+".yaml"), meta); err != nil {
			return written, err
		}
		written++
	}

	fmt.Printf("datatables:   wrote %d -> %s/\n", written, outDir)
	return written, nil
}

// dataTableMeta is the on-disk companion metadata for one data table. Columns is
// the raw column JSON preserved as-is so unmodeled fields survive the round-trip.
type dataTableMeta struct {
	DisplayName string `yaml:"display_name"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Columns     []any  `yaml:"columns"`
	RowCount    int    `yaml:"row_count"`
	CreateTime  string `yaml:"create_time,omitempty"`
	UpdateTime  string `yaml:"update_time,omitempty"`
}

// writeDataTableCSV writes a header row derived from cols followed by each row's
// positional Values (capped at maxDataTableRows) to path.
func writeDataTableCSV(path string, cols []chronicle.DataTableColumn, rows []chronicle.DataTableRow) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	// Capture the close error (a flush-to-disk failure) into the named return
	// unless an earlier error already took precedence.
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	w := csv.NewWriter(f)
	if header := columnHeader(cols); len(header) > 0 {
		if err := w.Write(header); err != nil {
			return err
		}
	}
	for i, r := range rows {
		if i >= maxDataTableRows {
			break
		}
		if err := w.Write(r.Values); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// columnHeader builds the CSV header from the column info, preferring
// OriginalColumn, then Name, then the (stringified) ColumnIndex.
//
// DEVIATION: the legacy Python tool ordered the fallbacks
// originalColumn->columnIndex->name; we prefer the human-readable Name over the
// numeric index so headers stay meaningful when OriginalColumn is empty.
func columnHeader(cols []chronicle.DataTableColumn) []string {
	if len(cols) == 0 {
		return nil
	}
	header := make([]string, len(cols))
	for i, col := range cols {
		switch {
		case col.OriginalColumn != "":
			header[i] = col.OriginalColumn
		case col.Name != "":
			header[i] = col.Name
		default:
			header[i] = strconv.Itoa(col.ColumnIndex)
		}
	}
	return header
}

// rawColumns returns each column's original JSON object (decoded to a generic
// value) so the companion YAML mirrors the API shape exactly.
func rawColumns(cols []chronicle.DataTableColumn) []any {
	if len(cols) == 0 {
		return []any{}
	}
	out := make([]any, len(cols))
	for i, col := range cols {
		var v any
		if len(col.Raw) > 0 {
			if err := json.Unmarshal(col.Raw, &v); err == nil {
				out[i] = v
				continue
			}
		}
		// Fall back to the typed fields when Raw is unavailable.
		out[i] = map[string]any{
			"originalColumn": col.OriginalColumn,
			"columnIndex":    col.ColumnIndex,
			"name":           col.Name,
		}
	}
	return out
}
