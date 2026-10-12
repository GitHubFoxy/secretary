package core

import "strings"

func HasSemanticOutput(v any) bool {
	switch current := v.(type) {
	case string:
		return strings.TrimSpace(current) != ""
	case map[string]any:
		if len(current) == 0 {
			return false
		}
		hasOutputKeys := false
		for _, key := range []string{"formatted_output", "output", "result", "stdout", "stderr", "error", "text"} {
			if child, exists := current[key]; exists {
				hasOutputKeys = true
				if HasSemanticOutput(child) {
					return true
				}
			}
		}
		if hasOutputKeys {
			return false
		}
		for _, child := range current {
			if HasSemanticOutput(child) {
				return true
			}
		}
		return false
	case []any:
		if len(current) == 0 {
			return false
		}
		for _, child := range current {
			if HasSemanticOutput(child) {
				return true
			}
		}
		return false
	case nil:
		return false
	default:
		return true
	}
}
