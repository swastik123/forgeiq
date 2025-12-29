package strategy

import (
	"context"
	"fmt"
	"time"
)

// FunctionCallingStrategy runs an iterative loop:
// - ask ModelClient for either final_answer or tool_call
// - if tool_call: execute via ToolClient, append tool result to conversation
// - repeat until final or budget hit
type FunctionCallingStrategy struct{}

func (s *FunctionCallingStrategy) Name() string { return "function_calling" }

func (s *FunctionCallingStrategy) Run(ctx context.Context, in Input) (Result, error) {
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
		in.Budget.MaxToolCalls = 8
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
		if toolCalls >= in.Budget.MaxToolCalls {
			trace.Iterations = append(trace.Iterations, IterationTrace{Iteration: iter, StopReason: "max_tool_calls"})
			break
		}

		t0 := time.Now()
		dec, err := in.Modeler.Decide(ctx, ModelRequest{
			State: state,
			Tools: in.Tools.Tools,
			Model: in.Model,
			Meta:  in.Meta,
		})
		lat := time.Since(t0).Milliseconds()
		step := IterationTrace{Iteration: iter, LatencyMS: lat, ModelOutput: map[string]any{"summary": dec.Summary}}
		if err != nil {
			step.Error = err.Error()
			step.StopReason = "error"
			trace.Iterations = append(trace.Iterations, step)
			trace.FinishedAt = time.Now()
			return Result{Trace: trace}, err
		}

		if dec.FinalAnswer != "" && dec.ToolCall == nil {
			step.StopReason = "final"
			trace.Iterations = append(trace.Iterations, step)
			trace.FinishedAt = time.Now()
			return Result{
				FinalAnswer: dec.FinalAnswer,
				ToolOutputs: toolOutputs,
				Trace:       trace,
			}, nil
		}

		if dec.ToolCall == nil || dec.ToolCall.Name == "" {
			step.Error = "model returned neither final_answer nor tool_call"
			step.StopReason = "error"
			trace.Iterations = append(trace.Iterations, step)
			trace.FinishedAt = time.Now()
			return Result{Trace: trace}, fmt.Errorf("%s", step.Error)
		}

		toolCalls++
		step.ToolCall = dec.ToolCall

		out, terr := in.Tooler.CallTool(ctx, dec.ToolCall.Name, dec.ToolCall.Version, dec.ToolCall.Args)
		if terr != nil {
			step.Error = terr.Error()
			toolOutputs = append(toolOutputs, ToolCallOut{Tool: *dec.ToolCall, Err: terr.Error()})
			// add tool message for model context
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
	return Result{
		FinalAnswer: "",
		ToolOutputs: toolOutputs,
		Trace:       trace,
	}, nil
}


