# Agentic Loop - Quick Summary

## What is an Agentic Loop?

An **agentic loop** is a pattern where workflows:
1. **Iterate** - Execute plans multiple times
2. **Learn** - Use results from previous iterations
3. **Refine** - Improve plans based on evidence
4. **Interact** - Get human feedback when needed
5. **Adapt** - Adjust strategy dynamically

## Why Temporal is Perfect for Agentic Loops

✅ **Stateful** - Maintains state across iterations  
✅ **Signals** - Receive user input asynchronously  
✅ **Queries** - Check status without blocking  
✅ **Long-running** - Can run for hours/days  
✅ **Durable** - State persists across restarts  
✅ **Deterministic** - Reliable execution  

## Implementation

### Three Workflow Types Available:

1. **`IncidentWorkflow`** - Standard single-execution workflow
2. **`IncidentWorkflowIterative`** - Iterative with auto-refinement
3. **`IncidentWorkflowWithAgenticLoop`** - Full agentic loop with human feedback

### Key Features:

- **Max Iterations**: Configurable limit (default: 5)
- **Auto-Refinement**: Plans improve automatically based on evidence
- **Human Feedback**: Users can provide feedback at any point
- **Error Recovery**: Automatically retry with refined plans
- **Goal Detection**: Stops when goal is achieved

## API Endpoints

```bash
# Start workflow
POST /run

# Provide feedback
POST /feedback/{taskID}
Body: {"step_id": "s1", "feedback": "Looks good", "action": "continue"}

# Request refinement
POST /refine/{taskID}

# Stop loop
POST /stop/{taskID}

# Approve write operations
POST /approve/{taskID}

# Check status
GET /status/{taskID}

# Get result
GET /result/{taskID}
```

## Example Usage

```bash
# 1. Start workflow
TASK_ID=$(curl -X POST http://localhost:8080/run \
  -d '{"tenant_id":"acme","type":"incident_triage_agentic","input":{"incident_id":"INC-123","service":"payments","symptom":"high error rate"}}' \
  | jq -r '.task_id')

# 2. Check status (workflow is iterating)
curl http://localhost:8080/status/$TASK_ID | jq

# 3. Provide feedback when workflow waits
curl -X POST http://localhost:8080/feedback/$TASK_ID \
  -d '{"action":"continue","feedback":"Proceed"}'

# 4. Request plan refinement if needed
curl -X POST http://localhost:8080/refine/$TASK_ID

# 5. Get final result
curl http://localhost:8080/result/$TASK_ID | jq
```

## Benefits

1. **Smooth UX** - Continuous interaction vs one-shot
2. **Resilience** - Auto-recovery from errors
3. **Adaptability** - Plans improve with experience
4. **Transparency** - Users see what's happening
5. **Control** - Users can intervene when needed

## Configuration

```go
config := AgenticLoopConfig{
    MaxIterations:    5,              // Max iterations
    IterationTimeout: 5 * time.Minute, // Timeout per iteration
    EnableFeedback:   true,            // Enable human feedback
    AutoRefine:       true,            // Auto-refine plans
}
```

## Signals

- `feedback` - User feedback
- `refine` - Request plan refinement
- `continue` - Continue iteration
- `stop` - Stop the loop
- `approval` - Approve write operation

See [AGENTIC_LOOP.md](AGENTIC_LOOP.md) for complete documentation.


