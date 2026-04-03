package lang

import (
	"path/filepath"
	"strings"

	"forgeiq/internal/pragent"
)

func Detect(path string) pragent.Language {
	p := strings.ToLower(strings.TrimSpace(path))
	if p == "" {
		return pragent.LangUnknown
	}

	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".go":
		return pragent.LangGo
	case ".py":
		return pragent.LangPython
	case ".java":
		return pragent.LangJava
	case ".tf", ".tfvars", ".hcl":
		return pragent.LangTerraform
	}

	// Terraform common filenames without extensions we care about
	base := filepath.Base(p)
	switch strings.ToLower(base) {
	case "terraform.lock.hcl":
		return pragent.LangTerraform
	}

	return pragent.LangUnknown
}

