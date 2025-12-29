package eval

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ComputeRuntimeWeights aggregates eval_runs into a bounded weight signal per (taskType, agentID).
// It reads task_type from meta JSONB: meta->>'task_type'.
//
// Reward model (v0.1):
// - completed => +1
// - failed    => -1
// - denied    => 0
// - running   => ignored (not completed)
//
// Weight = clamp(avg_reward * maxAbsWeight, -maxAbsWeight, +maxAbsWeight)
func (s *Store) ComputeRuntimeWeights(ctx context.Context, agentType string, windowHours int, minRuns int, maxAbsWeight float64) (map[string]map[string]float64, error) {
	if s == nil {
		return nil, fmt.Errorf("eval store is nil")
	}
	agentType = strings.ToLower(strings.TrimSpace(agentType))
	col := "decision_agent_id"
	if agentType == "rule" {
		col = "rule_agent_id"
	}
	if maxAbsWeight <= 0 {
		maxAbsWeight = 5.0
	}
	if minRuns <= 0 {
		minRuns = 30
	}
	if windowHours <= 0 {
		windowHours = 168
	}

	// Only consider completed/failed/denied
	q := fmt.Sprintf(`
SELECT
  (meta->>'task_type') as task_type,
  %s as agent_id,
  COUNT(*) as runs,
  AVG(CASE
        WHEN status='completed' THEN 1
        WHEN status='failed' THEN -1
        ELSE 0
      END) as avg_reward
FROM eval_runs
WHERE %s IS NOT NULL
  AND meta ? 'task_type'
  AND started_at >= NOW() - ($1 || ' hours')::interval
  AND status IN ('completed','failed','denied')
GROUP BY task_type, agent_id
`, col, col)

	rows, err := s.db.QueryContext(ctx, q, fmt.Sprintf("%d", windowHours))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]map[string]float64{}
	for rows.Next() {
		var taskType string
		var agentID string
		var runs int
		var avg sql.NullFloat64
		if err := rows.Scan(&taskType, &agentID, &runs, &avg); err != nil {
			return nil, err
		}
		taskType = strings.TrimSpace(taskType)
		agentID = strings.TrimSpace(agentID)
		if taskType == "" || agentID == "" || runs < minRuns || !avg.Valid {
			continue
		}
		w := avg.Float64 * maxAbsWeight
		if w > maxAbsWeight {
			w = maxAbsWeight
		}
		if w < -maxAbsWeight {
			w = -maxAbsWeight
		}
		m, ok := out[taskType]
		if !ok {
			m = map[string]float64{}
			out[taskType] = m
		}
		m[agentID] = w
	}
	return out, rows.Err()
}

// RefreshRuntimeWeights periodically recomputes weights for both rule/decision agents and returns agentType->taskType->agentID->weight.
func (s *Store) RefreshRuntimeWeights(ctx context.Context, windowHours, minRuns int, maxAbsWeight float64) (map[string]map[string]map[string]float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	out := map[string]map[string]map[string]float64{}
	for _, typ := range []string{"rule", "decision"} {
		w, err := s.ComputeRuntimeWeights(ctx, typ, windowHours, minRuns, maxAbsWeight)
		if err != nil {
			return nil, err
		}
		out[typ] = w
	}
	return out, nil
}


