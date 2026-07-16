package chronicle

import (
	"strings"
	"testing"
	"time"
)

// CreateDataExport validates client-side before any request is issued, so a
// zero client exercises the guards offline.
func TestCreateDataExportValidation(t *testing.T) {
	c := &Client{}
	start := time.Now().Add(-time.Hour)
	end := time.Now()

	cases := []struct {
		name    string
		bucket  string
		s, e    time.Time
		wantSub string
	}{
		{"empty bucket", "", start, end, "gcsBucket must be provided"},
		{"bare bucket name", "my-bucket", start, end, "projects/"},
		{"end not after start", "projects/p/buckets/b", end, end, "end time must be after start"},
	}
	for _, tc := range cases {
		_, err := c.CreateDataExport(t.Context(), tc.bucket, nil, tc.s, tc.e)
		if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
			t.Errorf("%s: CreateDataExport err = %v, want substring %q", tc.name, err, tc.wantSub)
		}
	}
}
