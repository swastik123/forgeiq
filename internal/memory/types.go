package memory

import "time"

type Sensitivity string

const (
	SensitivityPublic     Sensitivity = "public"
	SensitivityInternal   Sensitivity = "internal"
	SensitivityRestricted Sensitivity = "restricted"
	SensitivitySecret     Sensitivity = "secret"
)

func SensitivityRank(s Sensitivity) int {
	switch s {
	case SensitivityPublic:
		return 0
	case SensitivityInternal:
		return 1
	case SensitivityRestricted:
		return 2
	case SensitivitySecret:
		return 3
	default:
		// Unknown treated as high.
		return 3
	}
}

type Record struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id"`
	RecordType  string         `json:"record_type"`
	Payload     map[string]any `json:"payload,omitempty"`
	EvidenceRef []string       `json:"evidence_refs,omitempty"`
	Confidence  float64        `json:"confidence,omitempty"`
	Sensitivity Sensitivity    `json:"sensitivity,omitempty"`
	ExpiresAt   *time.Time     `json:"expires_at,omitempty"`
	Version     int            `json:"version,omitempty"`
	CreatedAt   time.Time      `json:"created_at,omitempty"`
	UpdatedAt   time.Time      `json:"updated_at,omitempty"`
}

