package diff

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
)

// Unified diff parser for Bitbucket/Git style patches.

type LineKind string

const (
	LineContext LineKind = "context"
	LineAdd     LineKind = "add"
	LineDel     LineKind = "del"
)

type HunkLine struct {
	Kind    LineKind `json:"kind"`
	OldLine int      `json:"old_line,omitempty"` // 1-based, 0 if not applicable
	NewLine int      `json:"new_line,omitempty"` // 1-based, 0 if not applicable
	Content string   `json:"content"`
}

type Hunk struct {
	OldStart int        `json:"old_start"`
	OldLines int        `json:"old_lines"`
	NewStart int        `json:"new_start"`
	NewLines int        `json:"new_lines"`
	Header   string     `json:"header,omitempty"`
	Lines    []HunkLine `json:"lines"`
}

type FilePatch struct {
	OldPath  string `json:"old_path,omitempty"`
	NewPath  string `json:"new_path,omitempty"`
	IsNew    bool   `json:"is_new,omitempty"`
	IsDelete bool   `json:"is_delete,omitempty"`
	IsRename bool   `json:"is_rename,omitempty"`

	Hunks []Hunk `json:"hunks"`
}

type Patch struct {
	Files []FilePatch `json:"files"`
}

var (
	reDiffHeader = regexp.MustCompile(`^diff --git a/(.+?) b/(.+?)\s*$`)
	reHunkHeader = regexp.MustCompile(`^@@\s+-(\d+)(?:,(\d+))?\s+\+(\d+)(?:,(\d+))?\s+@@\s*(.*)$`)
)

func ParseUnified(s string) (Patch, error) {
	p := Patch{Files: []FilePatch{}}
	sc := bufio.NewScanner(strings.NewReader(s))

	var cur *FilePatch
	var curHunk *Hunk

	flushHunk := func() {
		if cur != nil && curHunk != nil {
			cur.Hunks = append(cur.Hunks, *curHunk)
			curHunk = nil
		}
	}
	flushFile := func() {
		if cur != nil {
			flushHunk()
			p.Files = append(p.Files, *cur)
			cur = nil
		}
	}

	oldLine := 0
	newLine := 0

	for sc.Scan() {
		line := sc.Text()

		if m := reDiffHeader.FindStringSubmatch(line); m != nil {
			flushFile()
			cur = &FilePatch{
				OldPath: strings.TrimSpace(m[1]),
				NewPath: strings.TrimSpace(m[2]),
				Hunks:   []Hunk{},
			}
			continue
		}

		// If we haven't hit a diff header yet, ignore leading noise.
		if cur == nil {
			continue
		}

		switch {
		case strings.HasPrefix(line, "new file mode "):
			cur.IsNew = true
		case strings.HasPrefix(line, "deleted file mode "):
			cur.IsDelete = true
		case strings.HasPrefix(line, "rename from "):
			cur.IsRename = true
			cur.OldPath = strings.TrimSpace(strings.TrimPrefix(line, "rename from "))
		case strings.HasPrefix(line, "rename to "):
			cur.IsRename = true
			cur.NewPath = strings.TrimSpace(strings.TrimPrefix(line, "rename to "))
		case strings.HasPrefix(line, "--- "):
			// ignore; paths already captured
		case strings.HasPrefix(line, "+++ "):
			// ignore; paths already captured
		default:
			// continue below
		}

		if m := reHunkHeader.FindStringSubmatch(line); m != nil {
			flushHunk()
			os := atoi(m[1])
			ol := atoiDefault(m[2], 1)
			ns := atoi(m[3])
			nl := atoiDefault(m[4], 1)
			curHunk = &Hunk{
				OldStart: os,
				OldLines: ol,
				NewStart: ns,
				NewLines: nl,
				Header:   strings.TrimSpace(m[5]),
				Lines:    []HunkLine{},
			}
			oldLine = os
			newLine = ns
			continue
		}

		if curHunk == nil {
			continue
		}

		if strings.HasPrefix(line, "\\ No newline at end of file") {
			continue
		}
		if line == "" {
			// Empty context line is represented as " " in unified diffs; tolerate empty.
		}

		prefix := byte(0)
		if len(line) > 0 {
			prefix = line[0]
		}
		body := ""
		if len(line) > 0 {
			body = line[1:]
		}

		switch prefix {
		case ' ':
			curHunk.Lines = append(curHunk.Lines, HunkLine{Kind: LineContext, OldLine: oldLine, NewLine: newLine, Content: body})
			oldLine++
			newLine++
		case '+':
			curHunk.Lines = append(curHunk.Lines, HunkLine{Kind: LineAdd, OldLine: 0, NewLine: newLine, Content: body})
			newLine++
		case '-':
			curHunk.Lines = append(curHunk.Lines, HunkLine{Kind: LineDel, OldLine: oldLine, NewLine: 0, Content: body})
			oldLine++
		default:
			// Some diffs include metadata lines inside hunks; keep as context without line increments.
			curHunk.Lines = append(curHunk.Lines, HunkLine{Kind: LineContext, OldLine: 0, NewLine: 0, Content: line})
		}
	}

	if err := sc.Err(); err != nil {
		return Patch{}, fmt.Errorf("scan diff: %w", err)
	}
	flushFile()
	return p, nil
}

func atoi(s string) int {
	s = strings.TrimSpace(s)
	n, _ := atoiE(s)
	return n
}

func atoiDefault(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	n, err := atoiE(s)
	if err != nil {
		return def
	}
	return n
}

func atoiE(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not int: %q", s)
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

