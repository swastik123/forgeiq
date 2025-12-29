package catalog

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/controlplane/interfaces"
)

// ToolDescriptor extends ToolInfo with catalog metadata like owner/compatibility.
type ToolDescriptor struct {
	Info interfaces.ToolInfo `json:"info"`

	Owner         string            `json:"owner,omitempty"`
	Compatibility map[string]string `json:"compatibility,omitempty"`
}

type Catalog struct {
	mu    sync.RWMutex
	tools map[string]ToolDescriptor // key: name:version
}

func New() *Catalog {
	return &Catalog{tools: map[string]ToolDescriptor{}}
}

func key(name, version string) string {
	return strings.TrimSpace(name) + ":" + strings.TrimSpace(version)
}

func (c *Catalog) Upsert(td ToolDescriptor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if td.Owner == "" && td.Info.Meta != nil {
		td.Owner = td.Info.Meta["owner"]
	}
	c.tools[key(td.Info.Name, td.Info.Version)] = td
}

func (c *Catalog) Get(name, version string) (ToolDescriptor, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	td, ok := c.tools[key(name, version)]
	return td, ok
}

func (c *Catalog) List() []ToolDescriptor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ToolDescriptor, 0, len(c.tools))
	for _, td := range c.tools {
		out = append(out, td)
	}
	return out
}

// RefreshFromToolClient populates the catalog from an MCP client tools.list.
func (c *Catalog) RefreshFromToolClient(ctx context.Context, tc interfaces.ToolClient) error {
	if tc == nil {
		return fmt.Errorf("tool client is nil")
	}
	tools, err := tc.ListTools(ctx)
	if err != nil {
		return err
	}
	for _, t := range tools {
		c.Upsert(ToolDescriptor{Info: t})
	}
	return nil
}

// ValidatePlan ensures every plan step references a known tool and args satisfy required schema fields.
func (c *Catalog) ValidatePlan(plan contracts.Plan) error {
	for _, step := range plan.Steps {
		if err := c.ValidateToolCall(step.ToolName, step.ToolVersion, step.Args); err != nil {
			return fmt.Errorf("plan step %s invalid: %w", step.StepID, err)
		}
	}
	return nil
}

// ValidateToolCall prevents runtime breakage (missing tool/version, missing required args).
func (c *Catalog) ValidateToolCall(name, version string, args map[string]any) error {
	td, ok := c.Get(name, version)
	if !ok {
		return fmt.Errorf("unknown tool: %s:%s", name, version)
	}
	return ValidateArgsAgainstSchema(td.Info.Schema, args)
}
