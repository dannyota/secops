package cli

import "testing"

func TestNormalizeSOARURL(t *testing.T) {
	cases := map[string]string{
		"tenant.siemplify-soar.com":          "https://tenant.siemplify-soar.com",
		"  tenant.siemplify-soar.com  ":      "https://tenant.siemplify-soar.com",
		"https://tenant.siemplify-soar.com/": "https://tenant.siemplify-soar.com",
		"https://tenant.example.com":         "https://tenant.example.com",
		"http://localhost:8080":              "http://localhost:8080",
		"":                                   "",
	}
	for in, want := range cases {
		if got := normalizeSOARURL(in); got != want {
			t.Errorf("normalizeSOARURL(%q) = %q, want %q", in, got, want)
		}
	}
}
