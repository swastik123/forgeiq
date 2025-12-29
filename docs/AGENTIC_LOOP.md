# Agentic Loop Pattern

## Overview

The Agentic Loop is a powerful pattern that enables iterative refinement and human-in-the-loop interactions in workflows. It allows workflows to:

1. **Iterate**: Execute plans multiple times with refinements
2. **Learn**: Use evidence from previous iterations to improve
3. **Interact**: Get human feedback at critical decision points
4. **Adapt**: Automatically refine plans based on results
5. **Recover**: Handle errors gracefully with retry logic

## Why Agentic Loop?

Traditional workflows execute a plan once and stop. Agentic loops enable:

- **Iterative Refinement**: Plans improve with each iteration based on evidence
- **Error Recovery**: Automatically retry with refined plans when errors occur
- **Human-in-the-Loop**: Get approval/feedback at critical points
- **Adaptive Behavior**: Adjust strategy based on results
- **Smooth User Experience**: Continuous interaction rather than one-shot execution

## Temporal's Built-in Support

Temporal workflows are **perfect** for agentic loops because:

1. **Stateful**: Maintain state across iterations
2. **Signals**: Receive user input asynchronously
3. **Queries**: Check status without blocking
4. **Long-running**: Can run for hours/days
5. **Durable**: State persists across restarts
6. **Deterministic**: Reliable execution

## Implementation

### Basic Agentic Loop

```go
func AgenticLoop(ctx workflow.Context, task Task, config AgenticLoopConfig) {
    for iteration := 1; iteration <= config.MaxIterations; iteration++ {
        // 1. Get plan (may be refined based on previous evidence)
        plan := getPlan(ctx, task, evidence)
        
        // 2. Execute plan
        results := executePlan(ctx, plan)
        
        // 3. Collect evidence
        evidence = mergeEvidence(evidence, results)
        
        // 4. Check if goal achieved
        if isGoalAchieved(evidence) {
            return success(evidence)
        }
        
        // 5. Optionally refine plan
        if shouldRefine(evidence) {
            plan = refinePlan(ctx, task, evidence)
        }
    }
}
```

### With Human Feedback

```go
func AgenticLoopWithFeedback(ctx workflow.Context, task Task) {
    feedbackChan := workflow.GetSignalChannel(ctx, "feedback")
    
    for iteration := 1; iteration <= maxIterations; iteration++ {
        plan := getPlan(ctx, task, evidence)
        
        // Execute steps
        for _, step := range plan.Steps {
            if needsApproval(step) {
                // Wait for user feedback
                var feedback Feedback
                feedbackChan.Receive(ctx, &feedback)
                
                if feedback.Action == "stop" {
                    return stopped()
                } else if feedback.Action == "modify" {
                    plan = applyModifications(plan, feedback)
                }
            }
            
            executeStep(ctx, step)
        }
    }
}
```

## Usage

### Starting a Workflow with Agentic Loop

```bash
# Start workflow (uses agentic loop by default)
curl -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{
    "incident_id": "INC-123",
    "service": "payments",
    "symptom": "high error rate"
  }'
```

### Providing Feedback

```bash
# Provide feedback during execution
curl -X POST http://localhost:8080/feedback/{taskID} \
  -H "Content-Type: application/json" \
  -d '{
    "step_id": "s2",
    "feedback": "This step looks good, proceed",
    "action": "continue"
  }'
```

### Requesting Plan Refinement

```bash
# Request plan refinement
curl -X POST http://localhost:8080/refine/{taskID}
```

### Stopping the Loop

```bash
# Stop the agentic loop
curl -X POST http://localhost:8080/stop/{taskID}
```

## Configuration

### Agentic Loop Config

```go
config := AgenticLoopConfig{
    MaxIterations:    5,              // Max iterations
    IterationTimeout: 5 * time.Minute, // Timeout per iteration
    EnableFeedback:   true,            // Enable human feedback
    AutoRefine:       true,            // Auto-refine plans
}
```

### Environment Variables

```bash
# Configure agentic loop behavior
export AGENTIC_LOOP_MAX_ITERATIONS=5
export AGENTIC_LOOP_ENABLE_FEEDBACK=true
export AGENTIC_LOOP_AUTO_REFINE=true
```

## Workflow Types

### 1. Standard Workflow (`IncidentWorkflow`)
- Single execution
- No iteration
- Fast and simple

### 2. Iterative Workflow (`IncidentWorkflowIterative`)
- Multiple iterations
- Auto-refinement
- Error recovery

### 3. Full Agentic Loop (`IncidentWorkflowWithAgenticLoop`)
- Maximum flexibility
- Human feedback
- Plan refinement
- Adaptive behavior

## Example Flow

```
1. User starts workflow
   ↓
2. Policy evaluation
   ↓
3. Get initial plan
   ↓
4. Execute plan (Iteration 1)
   ├─ Step 1: ✅ Success
   ├─ Step 2: ❌ Error
   └─ Step 3: ⏸️ Needs approval
   ↓
5. Wait for approval signal
   ↓
6. User approves → Continue
   ↓
7. Auto-refine plan based on error
   ↓
8. Execute refined plan (Iteration 2)
   ├─ Step 1: ✅ Success
   ├─ Step 2 (refined): ✅ Success
   └─ Step 3: ✅ Success
   ↓
9. Goal achieved → Complete
```

## Benefits

1. **Resilience**: Automatically recover from errors
2. **Adaptability**: Plans improve with experience
3. **Transparency**: Users see what's happening
4. **Control**: Users can intervene when needed
5. **Efficiency**: Fewer failed workflows

## Best Practices

1. **Set Reasonable Limits**: Don't loop forever
2. **Collect Evidence**: Track what works/doesn't work
3. **Request Feedback**: At critical decision points
4. **Log Everything**: For debugging and improvement
5. **Monitor Iterations**: Track iteration count and duration

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/run` | POST | Start workflow (with agentic loop) |
| `/status/{taskID}` | GET | Get workflow status |
| `/feedback/{taskID}` | POST | Provide feedback |
| `/refine/{taskID}` | POST | Request plan refinement |
| `/stop/{taskID}` | POST | Stop the loop |
| `/approve/{taskID}` | POST | Approve write operations |

## Signals

| Signal | Type | Description |
|--------|------|-------------|
| `feedback` | FeedbackEntry | User feedback |
| `refine` | bool | Request refinement |
| `continue` | bool | Continue iteration |
| `stop` | bool | Stop the loop |
| `approval` | bool | Approve write operation |

## Queries

| Query | Returns | Description |
|-------|---------|-------------|
| `status` | WorkflowStatus | Current workflow status |
| `result` | Artifact | Final result (if completed) |

## Example: Incident Response with Agentic Loop

```bash
# 1. Start incident response
TASK_ID=$(curl -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{"incident_id":"INC-123","service":"payments","symptom":"high error rate"}' \
  | jq -r '.task_id')

# 2. Check status (workflow is iterating)
curl http://localhost:8080/status/$TASK_ID | jq

# 3. Provide feedback when prompted
curl -X POST http://localhost:8080/feedback/$TASK_ID \
  -H "Content-Type: application/json" \
  -d '{"action":"continue","feedback":"Looks good"}'

# 4. Request refinement if needed
curl -X POST http://localhost:8080/refine/$TASK_ID

# 5. Get final result
curl http://localhost:8080/result/$TASK_ID | jq
```

## Conclusion

The Agentic Loop pattern transforms workflows from static one-shot executions into dynamic, adaptive processes that learn and improve. Combined with Temporal's powerful features, it provides a smooth, interactive user experience while maintaining reliability and durability.


