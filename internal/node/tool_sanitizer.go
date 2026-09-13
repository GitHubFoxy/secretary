package node

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
)

const maxSanitizedToolPayload = 16 << 10

// SanitizeToolArguments is the production boundary for tool-call arguments.
// Invalid or unsafe payloads are rejected, while forbidden fields are removed
// recursively from otherwise useful JSON.
func SanitizeToolArguments(raw json.RawMessage) (json.RawMessage, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage(`{}`), true
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, false
	}
	clean, ok := sanitizeToolValue(value)
	if !ok {
		return nil, false
	}
	encoded, err := json.Marshal(clean)
	if err != nil || len(encoded) > maxSanitizedToolPayload {
		return nil, false
	}
	return encoded, true
}

// SanitizeToolResult applies the same fail-closed policy to a tool result or
// error. JSON strings are decoded so nested ACP payloads cannot bypass it.
func SanitizeToolResult(result string) (string, bool) {
	result = strings.TrimSpace(result)
	if result == "" {
		return "", true
	}
	if looksLikeJSONContainer(result) {
		var value any
		if json.Unmarshal([]byte(result), &value) == nil {
			clean, ok := sanitizeToolValue(value)
			if !ok {
				return "", false
			}
			encoded, err := json.Marshal(clean)
			if err != nil || len(encoded) > maxSanitizedToolPayload {
				return "", false
			}
			return string(encoded), true
		}
	}
	if unsafeToolText(result) || len(result) > maxSanitizedToolPayload {
		return "", false
	}
	return result, true
}

func sanitizeToolValue(value any) (any, bool) {
	switch current := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(current))
		for key, child := range current {
			if forbiddenToolKey(key) {
				continue
			}
			cleaned, ok := sanitizeToolValue(child)
			if !ok {
				return nil, false
			}
			result[key] = cleaned
		}
		return result, true
	case []any:
		result := make([]any, len(current))
		for index, child := range current {
			cleaned, ok := sanitizeToolValue(child)
			if !ok {
				return nil, false
			}
			result[index] = cleaned
		}
		return result, true
	case string:
		if looksLikeJSONContainer(current) {
			var nested any
			if json.Unmarshal([]byte(current), &nested) == nil {
				cleaned, ok := sanitizeToolValue(nested)
				if !ok {
					return nil, false
				}
				encoded, err := json.Marshal(cleaned)
				if err != nil {
					return nil, false
				}
				return string(encoded), true
			}
		}
		if unsafeToolText(current) {
			return nil, false
		}
		return current, true
	default:
		return value, true
	}
}

func forbiddenToolKey(key string) bool {
	compact := strings.ToLower(strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return character
		}
		return -1
	}, key))
	for _, marker := range []string{
		"secret", "credential", "callback", "token", "password", "authorization", "apikey", "accesskey", "privatekey",
		"task", "session", "analysis", "reasoning", "thought", "chainofthought",
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func unsafeToolText(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"chain-of-thought", "chain of thought", "chain_of_thought", "raw thought", "raw_thought",
		"internal reasoning", "internal_reasoning", "thought process", "thought_process", "<think>", "</think>",
		"analysis:", "reasoning:", "thought:", "chain-of-thought:",
		"bearer ", "api_key=", "apikey=", "access_token", "api_token", "token=", "secret=", "credential=", "password=", "callback=",
		"runtime_session_id", "session_id", "sessionid", "sk-", "ghp_", "xoxb-",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func looksLikeJSONContainer(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}
