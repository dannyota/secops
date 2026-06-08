package cli

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestExtractValueField locks the value-list decode: both the flat-array shape
// (root-causes) and the objectsList-wrapped shape (tags/stages), de-duped, sorted,
// empties dropped.
func TestExtractValueField(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		field string
		want  []string
	}{
		{
			name:  "flat array rootCause",
			body:  `[{"rootCause":"Malware","id":1},{"rootCause":"Phishing","id":2},{"rootCause":"Malware","id":3},{"rootCause":"","id":4}]`,
			field: "rootCause",
			want:  []string{"Malware", "Phishing"},
		},
		{
			name:  "objectsList name",
			body:  `{"objectsList":[{"name":"Incident","id":1},{"name":"Assessment","id":2}],"metadata":{}}`,
			field: "name",
			want:  []string{"Assessment", "Incident"},
		},
		{
			name:  "empty objectsList",
			body:  `{"objectsList":[],"metadata":{}}`,
			field: "name",
			want:  []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractValueField(json.RawMessage(tc.body), tc.field)
			if err != nil {
				t.Fatalf("extractValueField: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
