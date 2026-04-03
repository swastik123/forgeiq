package config

import "testing"

func TestMatchPathGlob(t *testing.T) {
	cases := []struct {
		glob string
		path string
		want bool
	}{
		{"**/vendor/**", "a/vendor/x.go", true},
		{"**/vendor/**", "vendor/x.go", true},
		{"**/vendor/**", "a/b/c.go", false},
		{"**/*.lock", "go.sum.lock", true},
		{"**/*.lock", "go.sum", false},
		{"terraform.lock.hcl", "terraform.lock.hcl", true},
		{"terraform.lock.hcl", "x/terraform.lock.hcl", false},
	}
	for _, tc := range cases {
		if got := MatchPathGlob(tc.glob, tc.path); got != tc.want {
			t.Fatalf("MatchPathGlob(%q,%q)=%v want %v", tc.glob, tc.path, got, tc.want)
		}
	}
}

