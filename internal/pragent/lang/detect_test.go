package lang

import (
	"testing"

	"forgeiq/internal/pragent"
)

func TestDetect(t *testing.T) {
	cases := map[string]pragent.Language{
		"main.go":                 pragent.LangGo,
		"src/Main.java":           pragent.LangJava,
		"app/service/handler.py":  pragent.LangPython,
		"infra/main.tf":           pragent.LangTerraform,
		"terraform.lock.hcl":      pragent.LangTerraform,
		"README.md":               pragent.LangUnknown,
		"":                        pragent.LangUnknown,
	}
	for path, want := range cases {
		if got := Detect(path); got != want {
			t.Fatalf("Detect(%q)=%q want %q", path, got, want)
		}
	}
}

