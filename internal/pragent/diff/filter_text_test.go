package diff

import "testing"

func TestFilterUnifiedText(t *testing.T) {
	d := `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,1 +1,1 @@
-old
+new
diff --git a/vendor/x.go b/vendor/x.go
index 1..2 100644
--- a/vendor/x.go
+++ b/vendor/x.go
@@ -1,1 +1,1 @@
-old
+new
`
	out := FilterUnifiedText(d, func(path string) bool {
		return path != "vendor/x.go"
	})
	if out == "" {
		t.Fatalf("expected non-empty output")
	}
	if contains(out, "vendor/x.go") {
		t.Fatalf("expected vendor section removed")
	}
	if !contains(out, "diff --git a/a.go b/a.go") {
		t.Fatalf("expected a.go section kept")
	}
}

func contains(s, sub string) bool { return len(sub) == 0 || (len(s) >= len(sub) && (stringIndex(s, sub) >= 0)) }

func stringIndex(s, sub string) int {
	// tiny local helper to avoid importing strings in this test file
	n := len(sub)
	for i := 0; i+n <= len(s); i++ {
		if s[i:i+n] == sub {
			return i
		}
	}
	return -1
}

