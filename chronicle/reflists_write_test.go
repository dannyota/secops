package chronicle

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRefListUpdateClearEntries locks the high fix: a pointer to an empty slice
// serializes as "entries":[] (the clear-entries contract), while a
// description-only update omits the entries key entirely (no mask/body drift).
func TestRefListUpdateClearEntries(t *testing.T) {
	empty := []ReferenceListEntry{}
	b, err := json.Marshal(refListUpdateRequest{Entries: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"entries":[]`) {
		t.Errorf("clear must emit entries:[]; got %s", b)
	}

	desc := "new description"
	b2, err := json.Marshal(refListUpdateRequest{Description: &desc})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b2), "entries") {
		t.Errorf("description-only update must omit entries; got %s", b2)
	}
}
