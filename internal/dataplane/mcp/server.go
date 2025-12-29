package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"

	"forgeiq/internal/controlplane/interfaces"
)

// ToolEntry contains both handler and metadata for a tool
type ToolEntry struct {
	Handler ToolHandler
	Info    interfaces.ToolInfo
}

// Server implements an MCP (Model Context Protocol) server
type Server struct {
	tools map[string]*ToolEntry // key: "name:version"
}

// ToolHandler is a function that executes a tool
type ToolHandler func(args map[string]any) (map[string]any, error)

// New creates a new MCP server
func New() *Server {
	return &Server{
		tools: make(map[string]*ToolEntry),
	}
}

// RegisterTool registers a tool with the server
func (s *Server) RegisterTool(name, version string, handler ToolHandler) {
	key := fmt.Sprintf("%s:%s", name, version)
	s.tools[key] = &ToolEntry{
		Handler: handler,
		Info: interfaces.ToolInfo{
			Name:    name,
			Version: version,
		},
	}
}

// RegisterToolWithInfo registers a tool with full metadata
func (s *Server) RegisterToolWithInfo(info interfaces.ToolInfo, handler ToolHandler) {
	key := fmt.Sprintf("%s:%s", info.Name, info.Version)
	s.tools[key] = &ToolEntry{
		Handler: handler,
		Info:    info,
	}
}

// Handler returns the HTTP handler for the MCP server
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc", s.handleRPC)
	return mux
}

// RPC request structure
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPC response structure
type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

// RPC error structure
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// handleRPC handles JSON-RPC requests
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "", -32700, "Parse error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var resp rpcResponse
	resp.JSONRPC = "2.0"
	resp.ID = req.ID

	switch req.Method {
	case "tools.list":
		tools := s.listTools()
		resp.Result = tools
	case "tools.call":
		var params struct {
			Name    string         `json:"name"`
			Version string         `json:"version"`
			Args    map[string]any `json:"args"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "Invalid params"}
			json.NewEncoder(w).Encode(resp)
			return
		}

		result, err := s.callTool(params.Name, params.Version, params.Args)
		if err != nil {
			resp.Error = &rpcError{Code: -32000, Message: err.Error()}
		} else {
			resp.Result = result
		}
	default:
		resp.Error = &rpcError{Code: -32601, Message: "Method not found"}
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// sendError sends an RPC error response
func (s *Server) sendError(w http.ResponseWriter, id string, code int, message string, err error) {
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message + ": " + err.Error(),
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// listTools returns all registered tools
func (s *Server) listTools() []interfaces.ToolInfo {
	tools := make([]interfaces.ToolInfo, 0, len(s.tools))
	for _, entry := range s.tools {
		tools = append(tools, entry.Info)
	}
	return tools
}

// callTool executes a tool by name and version
func (s *Server) callTool(name, version string, args map[string]any) (map[string]any, error) {
	key := fmt.Sprintf("%s:%s", name, version)
	entry, ok := s.tools[key]
	if !ok {
		return nil, fmt.Errorf("tool not found: %s:%s", name, version)
	}

	return entry.Handler(args)
}
