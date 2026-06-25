package cli

import (
	"reflect"
	"testing"
)

// TestResolveAlertIDsArgsDedup asserts positional ids are de-duplicated and
// order-preserved, and that whitespace-only entries are dropped. (The --where /
// --stdin-ids paths require a live client / stdin and are covered live.)
func TestResolveAlertIDsArgsDedup(t *testing.T) {
	ids, err := resolveAlertIDs([]string{"de_1", " de_2 ", "de_1", "", "de_3"}, "", false, 24, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"de_1", "de_2", "de_3"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("resolveAlertIDs = %v, want %v", ids, want)
	}
}
