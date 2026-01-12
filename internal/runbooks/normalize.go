package runbooks

import (
	"strings"

	"forgeiq/internal/controlplane/contracts"
)

// NormalizeIDs prefixes each step ID with "<runbook_id>:<runbook_version>:" to avoid collisions
// across runbooks/versions (and to strengthen idempotency keys).
//
// If a step ID is already prefixed with that exact namespace, it is left unchanged.
func NormalizeIDs(rb contracts.Runbook) contracts.Runbook {
	prefix := strings.TrimSpace(rb.ID) + ":" + strings.TrimSpace(rb.Version) + ":"
	if strings.TrimSpace(rb.ID) == "" || strings.TrimSpace(rb.Version) == "" {
		// Validation should catch this; return unchanged to avoid surprise.
		return rb
	}

	out := rb
	out.Diagnostics = make([]contracts.RunbookStep, len(rb.Diagnostics))
	for i, s := range rb.Diagnostics {
		out.Diagnostics[i] = normalizeStepID(prefix, s)
	}
	out.Actions = make([]contracts.RunbookStep, len(rb.Actions))
	for i, s := range rb.Actions {
		out.Actions[i] = normalizeStepID(prefix, s)
	}
	return out
}

func normalizeStepID(prefix string, s contracts.RunbookStep) contracts.RunbookStep {
	out := s
	id := strings.TrimSpace(s.ID)
	if id == "" {
		return out
	}
	if strings.HasPrefix(id, prefix) {
		return out
	}
	out.ID = prefix + id
	return out
}



