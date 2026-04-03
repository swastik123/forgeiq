package diff

import (
	"bufio"
	"regexp"
	"strings"
)

// FilterUnifiedText keeps only diff sections where keep(path)==true.
// It is intentionally "text level" to preserve original diff formatting for LLM prompts.
func FilterUnifiedText(diffText string, keep func(path string) bool) string {
	if strings.TrimSpace(diffText) == "" || keep == nil {
		return diffText
	}
	reDiffHeader := regexp.MustCompile(`^diff --git a/(.+?) b/(.+?)\s*$`)
	sc := bufio.NewScanner(strings.NewReader(diffText))
	var out strings.Builder

	section := []string{}
	keepSection := true

	flush := func() {
		if keepSection {
			for _, l := range section {
				out.WriteString(l)
				out.WriteString("\n")
			}
		}
		section = section[:0]
	}

	for sc.Scan() {
		line := sc.Text()
		if m := reDiffHeader.FindStringSubmatch(line); m != nil {
			// new section
			flush()
			newPath := strings.TrimSpace(m[2])
			oldPath := strings.TrimSpace(m[1])
			path := newPath
			if path == "" {
				path = oldPath
			}
			keepSection = keep(path)
		}
		section = append(section, line)
	}
	flush()
	return out.String()
}

