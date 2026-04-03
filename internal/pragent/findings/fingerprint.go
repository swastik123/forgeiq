package findings

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"forgeiq/internal/pragent"
)

func Fingerprint(f pragent.Finding) string {
	// Keep stable across runs. Avoid including suggestion text (often changes).
	// Include rule + path + line + message (normalized).
	key := strings.Join([]string{
		strings.TrimSpace(f.RuleID),
		strings.TrimSpace(f.Path),
		fmt.Sprintf("%d", f.Line),
		normalize(f.Message),
	}, "|")
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Join(strings.Fields(s), " ")
	return s
}
