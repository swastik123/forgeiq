package findings

import (
	"sort"

	"forgeiq/internal/pragent"
)

// DedupeAndCap deduplicates by Fingerprint, sorts by severity/path/line, and applies caps.
func DedupeAndCap(in []pragent.Finding, maxTotal int, maxPerFile int) []pragent.Finding {
	if len(in) == 0 {
		return nil
	}
	byFP := map[string]pragent.Finding{}
	for _, f := range in {
		if f.Fingerprint == "" {
			f.Fingerprint = Fingerprint(f)
		}
		// Prefer higher severity if collision happens
		if prev, ok := byFP[f.Fingerprint]; ok {
			if severityRank(f.Severity) < severityRank(prev.Severity) {
				byFP[f.Fingerprint] = f
			}
			continue
		}
		byFP[f.Fingerprint] = f
	}

	out := make([]pragent.Finding, 0, len(byFP))
	for _, f := range byFP {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if severityRank(out[i].Severity) != severityRank(out[j].Severity) {
			return severityRank(out[i].Severity) < severityRank(out[j].Severity)
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Line < out[j].Line
	})

	if maxPerFile > 0 {
		per := map[string]int{}
		filtered := make([]pragent.Finding, 0, len(out))
		for _, f := range out {
			k := f.Path
			per[k]++
			if per[k] > maxPerFile {
				continue
			}
			filtered = append(filtered, f)
		}
		out = filtered
	}
	if maxTotal > 0 && len(out) > maxTotal {
		out = out[:maxTotal]
	}
	return out
}

func severityRank(s pragent.Severity) int {
	// Lower is worse.
	switch s {
	case pragent.SeverityBlocker:
		return 0
	case pragent.SeverityHigh:
		return 1
	case pragent.SeverityMedium:
		return 2
	case pragent.SeverityLow:
		return 3
	case pragent.SeverityNit:
		return 4
	default:
		return 5
	}
}
