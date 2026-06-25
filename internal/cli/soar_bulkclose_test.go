package cli

import "testing"

// TestSoarCaseIntID parses the integer case id from a resource name or bare id,
// the form the legacy bulk-close endpoint takes.
func TestSoarCaseIntID(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"projects/p/locations/l/instances/i/cases/1234", 1234, false},
		{"1234", 1234, false},
		{" 1234 ", 1234, false},
		{"cases/not-a-number", 0, true},
		{"", 0, true},
	}
	for _, tc := range cases {
		got, err := soarCaseIntID(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("soarCaseIntID(%q) = %d, want error", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("soarCaseIntID(%q) = %d, %v; want %d", tc.in, got, err, tc.want)
		}
	}
}

// TestCasesBrokenVerbsHidden asserts the 500ing Chronicle-host case verbs are
// hidden from help (still runnable) while the working uuid→id bridge stays
// visible — so the surface stops reading as usable-but-broken.
func TestCasesBrokenVerbsHidden(t *testing.T) {
	cases := newCasesCmd()
	want := map[string]bool{"list": true, "get": true, "search": true, "soar-id": false}
	for _, sub := range cases.Commands() {
		hideExpected, tracked := want[sub.Name()]
		if !tracked {
			continue
		}
		if sub.Hidden != hideExpected {
			t.Errorf("cases %s: Hidden = %v, want %v", sub.Name(), sub.Hidden, hideExpected)
		}
	}
}
