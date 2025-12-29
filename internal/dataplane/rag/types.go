package rag

// RetrievalResource is the normalized retrieval result schema (Dify-like).
type RetrievalResource struct {
	DocID     string         `json:"doc_id"`
	DatasetID string         `json:"dataset_id"`
	Source    string         `json:"source"` // "internal" | "external"
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Score     float64        `json:"score"`
}

type DatasetType string

const (
	DatasetInternal DatasetType = "internal"
	DatasetExternal DatasetType = "external"
)

type DatasetConfig struct {
	ID          string      `json:"id"`
	Type        DatasetType `json:"type"` // "internal" | "external"
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Tags        []string    `json:"tags,omitempty"`

	Internal *InternalDatasetConfig `json:"internal,omitempty"`
	External *ExternalDatasetConfig `json:"external,omitempty"`
}

type InternalDatasetConfig struct {
	Namespace string `json:"namespace"`
}

type ExternalDatasetConfig struct {
	Adapter string `json:"adapter"` // "elastic" | "confluence" | "vendor"

	// elastic
	Index string `json:"index,omitempty"`

	// confluence / vendor placeholders
	BaseURL string `json:"base_url,omitempty"`
}


