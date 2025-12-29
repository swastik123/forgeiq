package strategy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"forgeiq/internal/controlplane/interfaces"
)

// ReActStrategy (v0.1) is a lightweight fallback.
// It uses simple heuristics to pick a tool based on the latest user message,
// executes at most one tool call per iteration, and asks the model again.
//
// IMPORTANT: We do not return chain-of-thought. Trace is structured only.
type ReActStrategy struct{}

func (s *ReActStrategy) Name() string { return "react" }

func (s *ReActStrategy) Run(ctx context.Context, in Input) (Result, error) {
	if in.Tooler == nil {
		return Result{}, fmt.Errorf("tool client is required")
	}
	if in.Modeler == nil {
		return Result{}, fmt.Errorf("model client is required")
	}
	if in.Budget.MaxIterations <= 0 {
		in.Budget.MaxIterations = 8
	}
	if in.Budget.MaxToolCalls <= 0 {
		in.Budget.MaxToolCalls = 6
	}
	if in.Budget.MaxWallTime <= 0 {
		in.Budget.MaxWallTime = 20 * time.Second
	}

	start := time.Now()
	trace := Trace{Strategy: s.Name(), StartedAt: start}
	state := in.State

	toolCalls := 0
	var toolOutputs []ToolCallOut

	for iter := 1; iter <= in.Budget.MaxIterations; iter++ {
		if time.Since(start) > in.Budget.MaxWallTime {
			trace.Iterations = append(trace.Iterations, IterationTrace{Iteration: iter, StopReason: "max_wall_time"})
			break
		}

		// 1) Ask model for either final or tool_call (still via ModelClient)
		t0 := time.Now()
		dec, err := in.Modeler.Decide(ctx, ModelRequest{
			State: state,
			Tools: in.Tools.Tools,
			Model: in.Model,
			Meta:  in.Meta,
		})
		step := IterationTrace{Iteration: iter, LatencyMS: time.Since(t0).Milliseconds(), ModelOutput: map[string]any{"summary": dec.Summary}}
		if err != nil {
			step.Error = err.Error()
			step.StopReason = "error"
			trace.Iterations = append(trace.Iterations, step)
			trace.FinishedAt = time.Now()
			return Result{Trace: trace}, err
		}

		// If model already final, return.
		if dec.FinalAnswer != "" && dec.ToolCall == nil {
			step.StopReason = "final"
			trace.Iterations = append(trace.Iterations, step)
			trace.FinishedAt = time.Now()
			return Result{FinalAnswer: dec.FinalAnswer, ToolOutputs: toolOutputs, Trace: trace}, nil
		}

		// 2) If model didn't choose a tool, do a naive selection based on latest user message.
		if dec.ToolCall == nil || dec.ToolCall.Name == "" {
			if toolCalls >= in.Budget.MaxToolCalls {
				step.StopReason = "max_tool_calls"
				trace.Iterations = append(trace.Iterations, step)
				break
			}
			msg := latestUserText(state)
			tc := pickToolHeuristic(msg, in.Tools.Tools)
			if tc == nil {
				step.StopReason = "final"
				step.ModelOutput = map[string]any{"summary": "no tool needed"}
				trace.Iterations = append(trace.Iterations, step)
				trace.FinishedAt = time.Now()
				return Result{FinalAnswer: msg, ToolOutputs: toolOutputs, Trace: trace}, nil
			}
			dec.ToolCall = tc
		}

		toolCalls++
		step.ToolCall = dec.ToolCall
		out, terr := in.Tooler.CallTool(ctx, dec.ToolCall.Name, dec.ToolCall.Version, dec.ToolCall.Args)
		if terr != nil {
			step.Error = terr.Error()
			toolOutputs = append(toolOutputs, ToolCallOut{Tool: *dec.ToolCall, Err: terr.Error()})
			state.Messages = append(state.Messages, Message{Role: RoleTool, Name: dec.ToolCall.Name, Content: "tool_error", Data: map[string]any{"error": terr.Error()}, TS: time.Now()})
			trace.Iterations = append(trace.Iterations, step)
			continue
		}
		step.ToolResult = out
		toolOutputs = append(toolOutputs, ToolCallOut{Tool: *dec.ToolCall, Out: out})
		state.Messages = append(state.Messages, Message{Role: RoleTool, Name: dec.ToolCall.Name, Content: "tool_result", Data: out, TS: time.Now()})
		trace.Iterations = append(trace.Iterations, step)
	}

	trace.FinishedAt = time.Now()
	return Result{FinalAnswer: "", ToolOutputs: toolOutputs, Trace: trace}, nil
}

func latestUserText(state ConversationState) string {
	for i := len(state.Messages) - 1; i >= 0; i-- {
		if state.Messages[i].Role == RoleUser {
			return state.Messages[i].Content
		}
	}
	return ""
}

func pickToolHeuristic(msg string, tools []interfaces.ToolInfo) *ToolCall {
	// tiny heuristic: if user mentions "logs" pick logs.search; if "rag" pick rag.single_retrieve
	q := strings.ToLower(msg)
	for _, t := range tools {
		if strings.Contains(q, "logs") && t.Name == "logs.search" {
			return &ToolCall{Name: "logs.search", Version: t.Version, Args: map[string]any{"query": msg, "limit": 5}}
		}
		if strings.Contains(q, "rag") && t.Name == "rag.single_retrieve" {
			return &ToolCall{Name: "rag.single_retrieve", Version: t.Version, Args: map[string]any{"query": msg, "available_datasets": []any{}, "router_mode": "rule_based", "top_k": 5}}
		}
	}
	return nil
}
