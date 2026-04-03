package config

import (
	"regexp"
	"strings"
)

// MatchPathGlob matches a path against a glob supporting:
// - "*"  within a path segment
// - "**" across path separators
//
// It treats paths as forward-slash separated.
func MatchPathGlob(glob string, path string) bool {
	glob = strings.TrimSpace(glob)
	path = strings.TrimSpace(path)
	if glob == "" || path == "" {
		return false
	}
	g := strings.ReplaceAll(glob, "\\", "/")
	p := strings.ReplaceAll(path, "\\", "/")

	// Special-case: leading "**/" should match either at root or nested.
	leadAnyDir := false
	if strings.HasPrefix(g, "**/") {
		leadAnyDir = true
		g = strings.TrimPrefix(g, "**/")
	}

	// Escape regex metacharacters, then re-introduce glob tokens.
	re := regexp.QuoteMeta(g)
	// order matters: replace \*\* first
	re = strings.ReplaceAll(re, `\*\*`, `.*`)
	re = strings.ReplaceAll(re, `\*`, `[^/]*`)
	if leadAnyDir {
		re = "^(?:.*/)?" + re + "$"
	} else {
		re = "^" + re + "$"
	}
	rx, err := regexp.Compile(re)
	if err != nil {
		return false
	}
	return rx.MatchString(p)
}

func BuildIgnoreMatcher(globs []string) func(path string) bool {
	gs := make([]string, 0, len(globs))
	for _, g := range globs {
		g = strings.TrimSpace(g)
		if g != "" {
			gs = append(gs, g)
		}
	}
	return func(path string) bool {
		for _, g := range gs {
			if MatchPathGlob(g, path) {
				return true
			}
		}
		return false
	}
}

