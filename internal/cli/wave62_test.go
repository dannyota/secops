package cli

import (
	"encoding/json"
	"testing"
)

// TestCommandsCatalogJSONColumn asserts the per-command --json support reported
// by `secopsctl commands` (the JSON field) is accurate for a representative
// sample: commands whose output honors --json are marked, and ones that never
// emit JSON (pull writes files; config is interactive) are not.
func TestCommandsCatalogJSONColumn(t *testing.T) {
	byPath := map[string]commandRow{}
	for _, r := range collectCommands(rootCmd, "") {
		byPath[r.Path] = r
	}

	wantJSON := []string{
		"alerts list",      // emits a JSON snapshot under --json
		"soar case counts", // structured counts under --json
		"commands",         // the catalog itself
		"info",             // resolved config as JSON
		"doctor",           // {ok, version, checks[]}
		"drift",            // per-surface drift report
		"soar case close",  // guarded verb: dry-run/apply metadata under --json
		"rules alerts",     // always raw JSON regardless of the flag
		"query udm",        // raw event array under --json
		"soar case get",    // raw case object under --json
		"watchlists get",   // always JSON
		"parsers run",      // parsed UDM is always JSON
	}
	for _, path := range wantJSON {
		r, ok := byPath[path]
		if !ok {
			t.Errorf("command %q missing from the catalog", path)
			continue
		}
		if !r.JSON {
			t.Errorf("%q should be marked as honoring --json", path)
		}
	}

	wantNoJSON := []string{
		"pull",   // text-only: its output is the files it writes
		"config", // interactive form; never emits JSON
	}
	for _, path := range wantNoJSON {
		r, ok := byPath[path]
		if !ok {
			t.Errorf("command %q missing from the catalog", path)
			continue
		}
		if r.JSON {
			t.Errorf("%q must not be marked as honoring --json", path)
		}
	}
}

// TestCommandRowJSONFieldRoundTrips confirms the catalog row's new json field is
// present and round-trips in the --json output shape (so an agent reading
// `secopsctl commands --json` sees a stable boolean key).
func TestCommandRowJSONFieldRoundTrips(t *testing.T) {
	row := commandRow{Path: "alerts list", Kind: "read", JSON: true, Short: "x"}
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		JSON *bool `json:"json"`
	}
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.JSON == nil {
		t.Fatalf("json field absent from marshaled row: %s", b)
	}
	if *back.JSON != true {
		t.Errorf("json field = %v, want true", *back.JSON)
	}

	// A read-only-with-no-JSON row marshals the field as false (not omitted), so
	// the column is unambiguous for every row.
	noJSON := commandRow{Path: "pull", Kind: "read"}
	b2, err := json.Marshal(noJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b2, &back); err != nil {
		t.Fatal(err)
	}
	if back.JSON == nil || *back.JSON != false {
		t.Errorf("json field for a non-JSON row = %v, want false (present)", back.JSON)
	}
}
