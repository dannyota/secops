package cli

import "testing"

func TestValidateCaseFilter(t *testing.T) {
	cases := []struct {
		name    string
		filter  string
		wantErr bool
	}{
		{"empty is fine", "", false},
		{"sql-style equality", "status = 'OPENED'", false},
		{"odata eq rejected", "status eq 'OPENED'", true},
		{"odata gt rejected case-insensitive", "priority GT 2", true},
		{"compound warns but passes", "status = 'OPENED' AND priority = 'HIGH'", false},
		{"contains passes", "contains(displayName,'phish')", false},
	}
	for _, tc := range cases {
		err := validateCaseFilter(tc.filter)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: validateCaseFilter(%q) err = %v, wantErr %v", tc.name, tc.filter, err, tc.wantErr)
		}
	}
}
