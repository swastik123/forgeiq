package diff

import "testing"

func TestParseUnified_Basic(t *testing.T) {
	d := `diff --git a/foo.go b/foo.go
index 111..222 100644
--- a/foo.go
+++ b/foo.go
@@ -1,2 +1,3 @@ package main
 func main() {
-    println("old")
+    println("new")
+    println("more")
 }
`
	p, err := ParseUnified(d)
	if err != nil {
		t.Fatalf("ParseUnified: %v", err)
	}
	if len(p.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(p.Files))
	}
	fp := p.Files[0]
	if fp.NewPath != "foo.go" {
		t.Fatalf("expected new path foo.go, got %q", fp.NewPath)
	}
	if len(fp.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(fp.Hunks))
	}
	h := fp.Hunks[0]
	if h.NewStart != 1 {
		t.Fatalf("expected new start 1, got %d", h.NewStart)
	}
	// Ensure added lines have new line numbers.
	var adds []HunkLine
	for _, ln := range h.Lines {
		if ln.Kind == LineAdd {
			adds = append(adds, ln)
		}
	}
	if len(adds) != 2 {
		t.Fatalf("expected 2 added lines, got %d", len(adds))
	}
	if adds[0].NewLine != 2 {
		t.Fatalf("expected first add new line 2, got %d", adds[0].NewLine)
	}
	if adds[1].NewLine != 3 {
		t.Fatalf("expected second add new line 3, got %d", adds[1].NewLine)
	}
}

