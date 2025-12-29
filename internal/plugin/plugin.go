package plugin

import (
	"context"

	"forgeiq/internal/controlplane/contracts"
	"forgeiq/internal/controlplane/interfaces"
)

// AgentPlugin defines the interface for custom agents
type AgentPlugin interface {
	// Name returns the plugin name
	Name() string

	// Version returns the plugin version
	Version() string

	// TaskTypes returns the task types this agent handles
	TaskTypes() []string

	// Execute executes the agent task
	Execute(ctx context.Context, task contracts.Task) (contracts.Artifact, error)
}

// ToolPlugin defines the interface for custom tools
type ToolPlugin interface {
	// Name returns the tool name
	Name() string

	// Version returns the tool version
	Version() string

	// Info returns tool metadata
	Info() interfaces.ToolInfo

	// Execute executes the tool
	Execute(ctx context.Context, args map[string]any) (map[string]any, error)
}

// Registry manages plugins
type Registry struct {
	agents map[string]AgentPlugin
	tools  map[string]ToolPlugin
}

// NewRegistry creates a new plugin registry
func NewRegistry() *Registry {
	return &Registry{
		agents: make(map[string]AgentPlugin),
		tools:  make(map[string]ToolPlugin),
	}
}

// RegisterAgent registers an agent plugin
func (r *Registry) RegisterAgent(agent AgentPlugin) {
	key := agent.Name() + ":" + agent.Version()
	r.agents[key] = agent
}

// RegisterTool registers a tool plugin
func (r *Registry) RegisterTool(tool ToolPlugin) {
	key := tool.Name() + ":" + tool.Version()
	r.tools[key] = tool
}

// GetAgent retrieves an agent plugin
func (r *Registry) GetAgent(name, version string) (AgentPlugin, bool) {
	key := name + ":" + version
	agent, ok := r.agents[key]
	return agent, ok
}

// GetTool retrieves a tool plugin
func (r *Registry) GetTool(name, version string) (ToolPlugin, bool) {
	key := name + ":" + version
	tool, ok := r.tools[key]
	return tool, ok
}

// ListAgents returns all registered agents
func (r *Registry) ListAgents() []AgentPlugin {
	agents := make([]AgentPlugin, 0, len(r.agents))
	for _, agent := range r.agents {
		agents = append(agents, agent)
	}
	return agents
}

// ListTools returns all registered tools
func (r *Registry) ListTools() []ToolPlugin {
	tools := make([]ToolPlugin, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}
