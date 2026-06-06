package mirror

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

// data_tables as code, on the shared reconcile engine. On disk it reuses the
// exact `<slug>.csv` (header + positional rows) + `<slug>.yaml` (display name,
// name, description, raw columns, row count) layout that `pull data_tables`
// already writes, so a pulled snapshot pushes back without conversion.
//
// Two write semantics make this surface distinct:
//   - Columns are IMMUTABLE after create. A column-structure change is reported
//     as an error on update (delete + recreate is the only path), never silently
//     dropped.
//   - Rows are a wholesale DESTROY-AND-REPLACE (ReplaceDataTableRows / bulkReplace)
//     — exactly what the dry-run guard exists for.
//
// A data table's display name IS its server id (letters/digits/underscores), so
// the canonical keys on display name and create uses it as the dataTableId.

// dtColumn is the normalized, diff-stable view of one column: the meaningful
// config (name + type or entity mapping), dropping server-managed fields like
// columnIndex (implied by order). Both the live and on-disk sides reduce to this.
type dtColumn struct {
	OriginalColumn   string `json:"original_column"`
	ColumnType       string `json:"column_type,omitempty"`
	MappedColumnPath string `json:"mapped_column_path,omitempty"`
}

// dataTableSpec is the diff basis for a data table: its meaningful config plus
// its full row set (rows are desired state for a data table).
type dataTableSpec struct {
	DisplayName string     `json:"display_name"`
	Description string     `json:"description,omitempty"`
	Columns     []dtColumn `json:"columns"`
	Rows        [][]string `json:"rows"`
}

// dtRawModel is carried in Object.Raw for LIVE/echo objects so the pull writer
// can render the faithful `<slug>.csv` + `<slug>.yaml` (full columns, timestamps)
// — the same files PullDataTables writes. Local objects never carry it.
type dtRawModel struct {
	Table chronicle.DataTable `json:"table"`
	Rows  [][]string          `json:"rows"`
}

func dataTablesSurface(c *chronicle.Client) reconcile.Surface {
	return reconcile.Surface{
		Name:    "data_tables",
		Dir:     DirDataTables,
		Product: reconcile.ProductSIEM,
		// Columns immutable + whole-table delete is high blast → not prune-eligible.
		Caps: reconcile.Capabilities{NoEtag: true},

		List:    dataTablesList(c),
		LoadDir: loadDataTables,
		Write:   writeDataTableObject,
		Create:  dataTablesCreate(c),
		Update:  dataTablesUpdate(c),
		Delete:  dataTablesDelete(c),
	}
}

// dataTablesList reads every live table plus its rows into engine objects.
func dataTablesList(c *chronicle.Client) func(context.Context) (reconcile.ListResult, error) {
	return func(ctx context.Context) (reconcile.ListResult, error) {
		tables, err := c.ListDataTables(ctx)
		if err != nil {
			return reconcile.ListResult{}, err
		}
		res := reconcile.ListResult{}
		for _, tbl := range tables {
			o, berr := dataTableLiveObject(ctx, c, tbl)
			if berr != nil {
				// A per-table read failure must not be mistaken for a deletion.
				warnf("data_tables: read %s: %v", lastSegment(tbl.Name), berr)
				res.Incomplete = true
				continue
			}
			res.Objects = append(res.Objects, o)
		}
		return res, nil
	}
}

// dataTableLiveObject fetches a table's rows and builds its engine object
// (canonical diff basis + identity + the faithful write model in Raw).
func dataTableLiveObject(ctx context.Context, c *chronicle.Client, tbl chronicle.DataTable) (reconcile.Object, error) {
	display := tbl.DisplayName
	if display == "" {
		display = lastSegment(tbl.Name)
	}
	tableID := lastSegment(tbl.Name)
	if tableID == "" {
		tableID = display
	}
	rows, err := c.ListDataTableRows(ctx, tableID)
	if err != nil {
		return reconcile.Object{}, err
	}
	values := rowValuesOf(rows)
	canon, err := canonicalDataTable(dataTableSpec{
		DisplayName: display,
		Description: tbl.Description,
		Columns:     normalizeLiveColumns(tbl.ColumnInfo),
		Rows:        values,
	})
	if err != nil {
		return reconcile.Object{}, err
	}
	raw, err := json.Marshal(dtRawModel{Table: tbl, Rows: values})
	if err != nil {
		return reconcile.Object{}, err
	}
	return reconcile.Object{Slug: Slugify(display), ServerID: tbl.Name, Canonical: canon, Raw: raw}, nil
}

// loadDataTables reads every `<slug>.yaml` + sibling `<slug>.csv` into objects.
func loadDataTables(dir string) ([]reconcile.Object, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var objs []reconcile.Object
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".yaml")
		var meta dataTableMeta
		if rerr := readYAMLFile(filepath.Join(dir, e.Name()), &meta); rerr != nil {
			return nil, rerr
		}
		rows, rerr := readDataTableCSV(filepath.Join(dir, stem+".csv"))
		if rerr != nil {
			return nil, rerr
		}
		display := meta.DisplayName
		if display == "" {
			display = lastSegment(meta.Name)
		}
		canon, cerr := canonicalDataTable(dataTableSpec{
			DisplayName: display,
			Description: meta.Description,
			Columns:     normalizeAnyColumns(meta.Columns),
			Rows:        rows,
		})
		if cerr != nil {
			return nil, cerr
		}
		objs = append(objs, reconcile.Object{Slug: stem, ServerID: meta.Name, Canonical: canon})
	}
	return objs, nil
}

// writeDataTableObject renders a LIVE/echo object back to the `<slug>.csv` +
// `<slug>.yaml` layout (byte-identical to PullDataTables). Local objects carry no
// Raw and are never written here.
func writeDataTableObject(dir string, o reconcile.Object) error {
	if len(o.Raw) == 0 {
		return fmt.Errorf("data_tables: cannot write %q without a live model", o.Slug)
	}
	var m dtRawModel
	if err := json.Unmarshal(o.Raw, &m); err != nil {
		return err
	}
	if _, err := EnsureDir(dir); err != nil {
		return err
	}
	display := m.Table.DisplayName
	if display == "" {
		display = lastSegment(m.Table.Name)
	}
	if err := writeDataTableCSV(filepath.Join(dir, o.Slug+".csv"), m.Table.ColumnInfo, toDataTableRows(m.Rows)); err != nil {
		return err
	}
	return writeYAML(filepath.Join(dir, o.Slug+".yaml"), dataTableMeta{
		DisplayName: display,
		Name:        m.Table.Name,
		Description: m.Table.Description,
		Columns:     rawColumns(m.Table.ColumnInfo),
		RowCount:    len(m.Rows),
		CreateTime:  m.Table.CreateTime,
		UpdateTime:  m.Table.UpdateTime,
	})
}

// dataTablesCreate creates a new table (and its rows) from a local spec, then
// re-reads it so the on-disk file matches live exactly.
func dataTablesCreate(c *chronicle.Client) func(context.Context, reconcile.Object) (reconcile.Object, error) {
	return func(ctx context.Context, local reconcile.Object) (reconcile.Object, error) {
		spec, err := decodeDataTableSpec(local.Canonical)
		if err != nil {
			return reconcile.Object{}, err
		}
		tableID := spec.DisplayName
		cols := make([]chronicle.CreateColumn, len(spec.Columns))
		for i, col := range spec.Columns {
			cols[i] = chronicle.CreateColumn{
				Name:             col.OriginalColumn,
				Type:             chronicle.DataTableColumnType(col.ColumnType),
				MappedColumnPath: col.MappedColumnPath,
			}
		}
		res, err := c.CreateDataTable(ctx, tableID, spec.Description, cols, nonNilRows(spec.Rows))
		if err != nil {
			return reconcile.Object{}, err
		}
		if res.RowError != nil {
			return reconcile.Object{}, fmt.Errorf("table created but rows failed: %w", res.RowError)
		}
		return dataTableLiveObject(ctx, c, *res.Table)
	}
}

// dataTablesUpdate applies the minimal change: description (PATCH) and/or a
// wholesale row replace. Column-structure changes are rejected (immutable).
func dataTablesUpdate(c *chronicle.Client) func(context.Context, reconcile.Object, reconcile.Object) (reconcile.Object, error) {
	return func(ctx context.Context, local, live reconcile.Object) (reconcile.Object, error) {
		want, err := decodeDataTableSpec(local.Canonical)
		if err != nil {
			return reconcile.Object{}, err
		}
		have, err := decodeDataTableSpec(live.Canonical)
		if err != nil {
			return reconcile.Object{}, err
		}
		tableID := lastSegment(live.ServerID)

		if !columnsEqual(want.Columns, have.Columns) {
			return reconcile.Object{}, fmt.Errorf(
				"data_tables: column structure of %q changed; columns are immutable after create "+
					"(delete and recreate the table to change columns)", tableID)
		}
		if want.Description != have.Description {
			if _, err := c.UpdateDataTable(ctx, tableID, chronicle.DataTableUpdate{Description: &want.Description}); err != nil {
				return reconcile.Object{}, err
			}
		}
		if !rowsEqual(want.Rows, have.Rows) {
			if _, err := c.ReplaceDataTableRows(ctx, tableID, nonNilRows(want.Rows)); err != nil {
				return reconcile.Object{}, err
			}
		}
		dt, err := c.GetDataTable(ctx, tableID)
		if err != nil {
			return reconcile.Object{}, err
		}
		return dataTableLiveObject(ctx, c, *dt)
	}
}

// dataTablesDelete removes a table and all its rows (force). Used by the live
// write-smoke for cleanup; not reachable via --prune (PruneEligible is false).
func dataTablesDelete(c *chronicle.Client) func(context.Context, reconcile.Object) error {
	return func(ctx context.Context, live reconcile.Object) error {
		return c.DeleteDataTable(ctx, lastSegment(live.ServerID), true)
	}
}

// --- helpers ----------------------------------------------------------------

func canonicalDataTable(spec dataTableSpec) ([]byte, error) {
	spec.Rows = nonNilRows(spec.Rows)
	if spec.Columns == nil {
		spec.Columns = []dtColumn{}
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	return reconcile.Canonicalize(raw)
}

func decodeDataTableSpec(canonical []byte) (dataTableSpec, error) {
	var spec dataTableSpec
	err := json.Unmarshal(canonical, &spec)
	return spec, err
}

// normalizeLiveColumns reduces live column info to the diff-stable view.
func normalizeLiveColumns(cols []chronicle.DataTableColumn) []dtColumn {
	out := make([]dtColumn, len(cols))
	for i, col := range cols {
		out[i] = dtColumn{
			OriginalColumn:   col.OriginalColumn,
			ColumnType:       col.ColumnType,
			MappedColumnPath: col.MappedColumnPath,
		}
	}
	return out
}

// normalizeAnyColumns reduces the YAML's raw column objects to the same view.
func normalizeAnyColumns(cols []any) []dtColumn {
	out := make([]dtColumn, 0, len(cols))
	for _, c := range cols {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, dtColumn{
			OriginalColumn:   asString(m["originalColumn"]),
			ColumnType:       asString(m["columnType"]),
			MappedColumnPath: asString(m["mappedColumnPath"]),
		})
	}
	return out
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func columnsEqual(a, b []dtColumn) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(ab, bb)
}

func rowsEqual(a, b [][]string) bool {
	ab, _ := json.Marshal(nonNilRows(a))
	bb, _ := json.Marshal(nonNilRows(b))
	return bytes.Equal(ab, bb)
}

// rowValuesOf extracts positional values from API rows.
func rowValuesOf(rows []chronicle.DataTableRow) [][]string {
	out := make([][]string, len(rows))
	for i, r := range rows {
		out[i] = r.Values
	}
	return out
}

// toDataTableRows wraps positional values back as API rows for the CSV writer.
func toDataTableRows(rows [][]string) []chronicle.DataTableRow {
	out := make([]chronicle.DataTableRow, len(rows))
	for i, r := range rows {
		out[i] = chronicle.DataTableRow{Values: r}
	}
	return out
}

// nonNilRows guarantees a non-nil slice so an empty row set canonicalizes as
// `[]` on both the live and on-disk sides (never null vs []).
func nonNilRows(rows [][]string) [][]string {
	if rows == nil {
		return [][]string{}
	}
	return rows
}

// readDataTableCSV reads a `<slug>.csv` and returns its data rows (the header row
// is dropped — column definitions live in the companion YAML). A missing file
// means no rows.
func readDataTableCSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // tolerate ragged rows
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) <= 1 {
		return nil, nil // header only (or empty) → no data rows
	}
	return records[1:], nil
}
