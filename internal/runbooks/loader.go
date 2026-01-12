package runbooks

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"forgeiq/internal/controlplane/contracts"

	"gopkg.in/yaml.v3"
)

// LoadDir loads runbooks from a directory. Supported formats: .yaml/.yml/.json
// The returned map is keyed by runbook ID.
func LoadDir(dir string) (map[string]contracts.Runbook, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read runbooks dir: %w", err)
	}

	out := make(map[string]contracts.Runbook)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			continue
		}
		full := filepath.Join(dir, name)

		b, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}

		var rb contracts.Runbook
		switch ext {
		case ".json":
			if err := json.Unmarshal(b, &rb); err != nil {
				return nil, fmt.Errorf("parse json %s: %w", name, err)
			}
		default:
			dec := yaml.NewDecoder(strings.NewReader(string(b)))
			dec.KnownFields(true)
			if err := dec.Decode(&rb); err != nil {
				return nil, fmt.Errorf("parse yaml %s: %w", name, err)
			}
		}

		if err := ValidateRunbook(rb); err != nil {
			return nil, fmt.Errorf("invalid runbook %s: %w", name, err)
		}
		if _, exists := out[rb.ID]; exists {
			return nil, fmt.Errorf("duplicate runbook id: %s", rb.ID)
		}
		out[rb.ID] = rb
	}

	if len(out) == 0 {
		return nil, fs.ErrNotExist
	}
	return out, nil
}



