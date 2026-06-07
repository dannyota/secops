package mirror

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"danny.vn/secops/chronicle"
	"danny.vn/secops/internal/mirror/reconcile"
)

// sampleDataTable decodes a table from JSON so each column's Raw is populated
// (rawColumns relies on it), mirroring what the live API returns.
func sampleDataTable(t *testing.T) chronicle.DataTable {
	t.Helper()
	const j = `{
	  "name":"projects/p/locations/r/instances/c/dataTables/my_table",
	  "displayName":"my_table",
	  "description":"test table",
	  "columnInfo":[
	    {"columnIndex":0,"originalColumn":"host","columnType":"STRING"},
	    {"columnIndex":1,"originalColumn":"net","columnType":"CIDR"}
	  ]
	}`
	var tbl chronicle.DataTable
	if err := json.Unmarshal([]byte(j), &tbl); err != nil {
		t.Fatalf("decode sample table: %v", err)
	}
	return tbl
}

// liveObject reproduces what dataTableLiveObject builds (without a live client):
// the canonical from the live spec plus the faithful Raw write-model.
func liveObject(t *testing.T, tbl chronicle.DataTable, rows [][]string) (canon, raw []byte) {
	t.Helper()
	c, err := canonicalDataTable(dataTableSpec{
		DisplayName: tbl.DisplayName,
		Description: tbl.Description,
		Columns:     normalizeLiveColumns(tbl.ColumnInfo),
		Rows:        rows,
	})
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	r, err := json.Marshal(dtRawModel{Table: tbl, Rows: rows})
	if err != nil {
		t.Fatalf("marshal raw: %v", err)
	}
	return c, r
}

// TestDataTableRoundTrip is the core guarantee: a live object written to disk and
// re-loaded canonicalizes identically — so a pull immediately pushes back in sync.
func TestDataTableRoundTrip(t *testing.T) {
	tbl := sampleDataTable(t)
	rows := [][]string{{"a", "10.0.0.0/8"}, {"b", "192.168.0.0/16"}}
	canon, raw := liveObject(t, tbl, rows)

	dir := t.TempDir()
	obj := reconcile.Object{Slug: "my_table", ServerID: tbl.Name, Canonical: canon, Raw: raw}
	if err := writeDataTableObject(dir, obj); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Both files exist.
	for _, f := range []string{"my_table.yaml", "my_table.csv"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("expected %s: %v", f, err)
		}
	}

	loaded, err := loadDataTables(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d objects, want 1", len(loaded))
	}
	if loaded[0].ServerID != tbl.Name {
		t.Errorf("ServerID = %q, want %q", loaded[0].ServerID, tbl.Name)
	}
	if !bytes.Equal(loaded[0].Canonical, canon) {
		t.Errorf("round-trip canonical mismatch:\n live: %s\n disk: %s", canon, loaded[0].Canonical)
	}
}

// TestDataTableEmptyRowsRoundTrip covers a table with no rows (header-only CSV).
func TestDataTableEmptyRowsRoundTrip(t *testing.T) {
	tbl := sampleDataTable(t)
	canon, raw := liveObject(t, tbl, nil)
	dir := t.TempDir()
	obj := reconcile.Object{Slug: "my_table", ServerID: tbl.Name, Canonical: canon, Raw: raw}
	if err := writeDataTableObject(dir, obj); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := loadDataTables(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 || !bytes.Equal(loaded[0].Canonical, canon) {
		t.Errorf("empty-rows round-trip mismatch:\n live: %s\n disk: %s", canon, loaded[0].Canonical)
	}
}

// TestNormalizeColumnsAgree: the live and on-disk column reducers must produce
// the same diff-stable view, or every pulled table would show a phantom update.
func TestNormalizeColumnsAgree(t *testing.T) {
	tbl := sampleDataTable(t)
	live := normalizeLiveColumns(tbl.ColumnInfo)
	fromYAML := normalizeAnyColumns(rawColumns(tbl.ColumnInfo))
	if !columnsEqual(live, fromYAML) {
		lb, _ := json.Marshal(live)
		yb, _ := json.Marshal(fromYAML)
		t.Errorf("column reducers disagree:\n live: %s\n yaml: %s", lb, yb)
	}
}

func TestColumnsEqual(t *testing.T) {
	a := []dtColumn{{OriginalColumn: "x", ColumnType: "STRING"}}
	b := []dtColumn{{OriginalColumn: "x", ColumnType: "STRING"}}
	if !columnsEqual(a, b) {
		t.Error("identical columns reported unequal")
	}
	if columnsEqual(a, []dtColumn{{OriginalColumn: "x", ColumnType: "CIDR"}}) {
		t.Error("a column-type change must be detected (columns are immutable)")
	}
}

func TestRowsEqual(t *testing.T) {
	if !rowsEqual([][]string{{"a"}}, [][]string{{"a"}}) {
		t.Error("identical rows reported unequal")
	}
	if !rowsEqual(nil, [][]string{}) {
		t.Error("nil and empty rows must compare equal")
	}
	if rowsEqual([][]string{{"a"}}, [][]string{{"b"}}) {
		t.Error("a row change must be detected")
	}
}

func TestReadDataTableCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.csv")

	// Header + 2 data rows → 2 rows (header dropped).
	if err := os.WriteFile(path, []byte("host,net\na,10.0.0.0/8\nb,192.168.0.0/16\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := readDataTableCSV(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(rows) != 2 || rows[0][0] != "a" || rows[1][1] != "192.168.0.0/16" {
		t.Errorf("unexpected rows: %v", rows)
	}

	// Header only → no data rows.
	if err := os.WriteFile(path, []byte("host,net\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rows, _ := readDataTableCSV(path); len(rows) != 0 {
		t.Errorf("header-only CSV should yield no rows, got %v", rows)
	}

	// Missing file → no rows, no error.
	if rows, err := readDataTableCSV(filepath.Join(dir, "missing.csv")); err != nil || rows != nil {
		t.Errorf("missing CSV: rows=%v err=%v", rows, err)
	}
}
