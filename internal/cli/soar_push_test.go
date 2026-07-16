package cli

import (
	"testing"
)

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
