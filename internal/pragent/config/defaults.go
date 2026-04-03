package config

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed default_pr_reviewer.yaml
var defaultYAML []byte

func DefaultFile() (File, error) {
	var f File
	if err := yaml.Unmarshal(defaultYAML, &f); err != nil {
		return File{}, fmt.Errorf("default pr_reviewer.yaml invalid: %w", err)
	}
	if err := f.Validate(); err != nil {
		return File{}, fmt.Errorf("default pr_reviewer.yaml invalid: %w", err)
	}
	return f, nil
}

