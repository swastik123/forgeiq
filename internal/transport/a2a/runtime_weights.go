package a2a

import "time"

// ApplyRuntimeWeights best-effort injects computed runtime weights into routers that support it.
// weights: agentType -> taskType -> agentID -> weight
func ApplyRuntimeWeights(r Router, weights map[string]map[string]map[string]float64, maxAbs float64) bool {
	if r == nil {
		return false
	}
	switch rr := r.(type) {
	case *AgentRouter:
		if rr.Options.RuntimeWeights == nil {
			rr.Options.RuntimeWeights = map[string]map[string]map[string]float64{}
		}
		rr.Options.RuntimeWeightsEnabled = true
		rr.Options.RuntimeWeightsMax = maxAbs
		rr.Options.RuntimeWeights = weights
		if rr.Options.RuntimeWeights == nil {
			rr.Options.RuntimeWeights = map[string]map[string]map[string]float64{}
		}
		return true
	case *HybridRouter:
		if rr.Options.RuntimeWeights == nil {
			rr.Options.RuntimeWeights = map[string]map[string]map[string]float64{}
		}
		rr.Options.RuntimeWeightsEnabled = true
		rr.Options.RuntimeWeightsMax = maxAbs
		rr.Options.RuntimeWeights = weights
		return true
	default:
		return false
	}
}

// ApplyRuntimeWeightsStamp adds a timestamp for debugging (optional).
func ApplyRuntimeWeightsStamp(r Router) map[string]any {
	now := time.Now().Format(time.RFC3339)
	switch rr := r.(type) {
	case *AgentRouter:
		if rr.Options.RuntimeWeights == nil {
			return map[string]any{"runtime_weights_updated_at": now, "enabled": false}
		}
		return map[string]any{"runtime_weights_updated_at": now, "enabled": rr.Options.RuntimeWeightsEnabled}
	case *HybridRouter:
		return map[string]any{"runtime_weights_updated_at": now, "enabled": rr.Options.RuntimeWeightsEnabled}
	default:
		return map[string]any{"runtime_weights_updated_at": now, "enabled": false}
	}
}


