package validation

import (
	"fmt"
	"strings"

	"forgeiq/internal/controlplane/contracts"
)

// ValidateTask validates a task request
func ValidateTask(task contracts.Task) error {
	if task.ID == "" {
		return fmt.Errorf("task ID is required")
	}
	if task.Type == "" {
		return fmt.Errorf("task type is required")
	}
	if task.Input == nil {
		return fmt.Errorf("task input is required")
	}

	// Validate task type
	validTypes := []string{"incident_triage", "remediation", "monitoring"}
	if !contains(validTypes, task.Type) {
		return fmt.Errorf("invalid task type: %s (valid types: %s)", task.Type, strings.Join(validTypes, ", "))
	}

	// Validate input based on task type
	switch task.Type {
	case "incident_triage":
		if _, ok := task.Input["service"]; !ok {
			return fmt.Errorf("'service' field is required for incident_triage tasks")
		}
		if _, ok := task.Input["symptom"]; !ok {
			return fmt.Errorf("'symptom' field is required for incident_triage tasks")
		}
	}

	return nil
}

// ValidatePlanStep validates a plan step
func ValidatePlanStep(step contracts.PlanStep) error {
	if step.StepID == "" {
		return fmt.Errorf("step ID is required")
	}
	if step.ToolName == "" {
		return fmt.Errorf("tool name is required")
	}
	if step.ToolVersion == "" {
		return fmt.Errorf("tool version is required")
	}
	if len(step.Tags) == 0 {
		return fmt.Errorf("at least one tag is required")
	}
	if len(step.Scopes) == 0 {
		return fmt.Errorf("at least one scope is required")
	}

	// Validate tags
	validTags := []string{"read", "write", "logs", "incidents", "remediation"}
	for _, tag := range step.Tags {
		if !contains(validTags, tag) {
			return fmt.Errorf("invalid tag: %s (valid tags: %s)", tag, strings.Join(validTags, ", "))
		}
	}

	return nil
}

// SanitizeInput sanitizes user input to prevent injection attacks
func SanitizeInput(input map[string]any) map[string]any {
	sanitized := make(map[string]any)
	for k, v := range input {
		// Sanitize key
		key := sanitizeString(k)

		// Sanitize value based on type
		switch val := v.(type) {
		case string:
			sanitized[key] = sanitizeString(val)
		case map[string]any:
			sanitized[key] = SanitizeInput(val)
		case []any:
			sanitized[key] = sanitizeArray(val)
		default:
			sanitized[key] = v
		}
	}
	return sanitized
}

func sanitizeString(s string) string {
	// Remove null bytes and control characters
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	return s
}

func sanitizeArray(arr []any) []any {
	sanitized := make([]any, len(arr))
	for i, v := range arr {
		switch val := v.(type) {
		case string:
			sanitized[i] = sanitizeString(val)
		case map[string]any:
			sanitized[i] = SanitizeInput(val)
		case []any:
			sanitized[i] = sanitizeArray(val)
		default:
			sanitized[i] = v
		}
	}
	return sanitized
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
