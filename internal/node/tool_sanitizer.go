package node

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/beruseruko/secretary/internal/core"
)

const maxSanitizedToolPayload = 16 << 10

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
		current = redactToolSensitiveText(current)
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
	lower := strings.ToLower(toolRedactedValue.ReplaceAllString(value, ""))
	for _, marker := range []string{
		"chain-of-thought", "chain of thought", "chain_of_thought", "raw thought", "raw_thought",
		"internal reasoning", "internal_reasoning", "thought process", "thought_process", "<think>", "</think>",
		"analysis:", "reasoning:", "thought:", "chain-of-thought:",
		"bearer ", "api_key=", "apikey=", "access_token", "api_token", "token=", "secret=", "credential=", "password=", "callback=",
		"runtime_session_id", "session_id", "sessionid", "sk-", "ghp_", "xoxb-", "xoxb_",
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

var (
	redactAuthHeaderStart = regexp.MustCompile(`(?i)\b(authorization\s*[:=]\s*)`)
	redactDigestStart     = regexp.MustCompile(`(?i)\b(Digest\s+)`)
	redactStatusSuffix    = regexp.MustCompile(`(?i)\s+\(status [0-9]{3}\)$`)
	redactBasicAuth       = regexp.MustCompile(`(?i)\bbasic\s+[a-zA-Z0-9+/=]{4,}`)
	redactBearerToken     = regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+`)
	redactSKToken         = regexp.MustCompile(`(?i)\bsk-[a-zA-Z0-9_\-]+`)
	redactXoxbToken       = regexp.MustCompile(`(?i)\bxoxb-[a-zA-Z0-9_\-]+`)
	redactGHPToken        = regexp.MustCompile(`(?i)\bghp_[a-zA-Z0-9_\-]+`)
	urlRegex              = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s\"\'<>]+`)
)

var allowedProviderHelpURLs = map[string]string{
	"https://chatgpt.com/explore/pro":              "https://chatgpt.com/explore/pro",
	"https://chatgpt.com/settings/usage":           "https://chatgpt.com/settings/usage",
	"https://chatgpt.com":                          "https://chatgpt.com",
	"help.openai.com":                              "https://help.openai.com",
	"https://help.openai.com":                      "https://help.openai.com",
	"https://platform.openai.com":                  "https://platform.openai.com",
	"https://platform.openai.com/usage":            "https://platform.openai.com/usage",
	"https://platform.openai.com/account/billing":  "https://platform.openai.com/account/billing",
	"https://openai.com":                           "https://openai.com",
	"https://console.anthropic.com":                "https://console.anthropic.com",
	"https://console.anthropic.com/settings/plans": "https://console.anthropic.com/settings/plans",
	"https://docs.anthropic.com":                   "https://docs.anthropic.com",
	"https://anthropic.com":                        "https://anthropic.com",
}

func sanitizeURLsInText(text string) string {
	return urlRegex.ReplaceAllStringFunc(text, func(match string) string {
		trailing := ""
		for len(match) > 0 && strings.ContainsRune(".,;:!?'\")]}", rune(match[len(match)-1])) {
			trailing = string(match[len(match)-1]) + trailing
			match = match[:len(match)-1]
		}
		parsed, err := url.Parse(match)
		if err != nil {
			return ""
		}
		scheme := strings.ToLower(parsed.Scheme)
		if scheme != "http" && scheme != "https" {
			return ""
		}
		host := strings.ToLower(parsed.Hostname())
		path := strings.ToLower(strings.TrimRight(parsed.Path, "/"))
		normalized := "https://" + host + path
		if canonical, ok := allowedProviderHelpURLs[normalized]; ok {
			return canonical + trailing
		}
		return ""
	})
}

func SanitizeFailureMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	if json.Valid([]byte(msg)) {
		var value any
		if json.Unmarshal([]byte(msg), &value) == nil {
			value = sanitizeFailureJSONStrings(value)
			if encoded, err := json.Marshal(value); err == nil {
				return core.SanitizeFailureMessage(string(encoded))
			}
		}
	}
	if strings.ContainsAny(msg, "{[") {
		return core.SanitizeFailureMessage(msg)
	}
	msg = sanitizeURLsInText(msg)
	msg = redactSKToken.ReplaceAllString(msg, "[redacted]")
	msg = redactXoxbToken.ReplaceAllString(msg, "[redacted]")
	msg = redactGHPToken.ReplaceAllString(msg, "[redacted]")
	msg = redactToolSensitiveText(msg)
	msg = core.SanitizeFailureMessage(msg)

	runes := []rune(msg)
	if len(runes) > 1000 {
		return string(runes[:997]) + "..."
	}
	return msg
}

func sanitizeFailureJSONStrings(value any) any {
	return sanitizeFailureJSONStringsAtDepth(value, 0)
}

func sanitizeFailureJSONStringsAtDepth(value any, nestedDepth int) any {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			current[key] = sanitizeFailureJSONStringsAtDepth(child, nestedDepth)
		}
		return current
	case []any:
		for index, child := range current {
			current[index] = sanitizeFailureJSONStringsAtDepth(child, nestedDepth)
		}
		return current
	case string:
		trimmed := strings.TrimSpace(current)
		if json.Valid([]byte(trimmed)) {
			if nestedDepth >= 8 {
				return "[redacted]"
			}
			if looksLikeJSONContainer(trimmed) {
				var nested any
				if json.Unmarshal([]byte(trimmed), &nested) == nil {
					nested = sanitizeFailureJSONStringsAtDepth(nested, nestedDepth+1)
					if encoded, err := json.Marshal(nested); err == nil {
						return string(encoded)
					}
				}
			} else if strings.HasPrefix(trimmed, `"`) {
				var nested string
				if json.Unmarshal([]byte(trimmed), &nested) == nil {
					nested = sanitizeFailureJSONStringsAtDepth(nested, nestedDepth+1).(string)
					if encoded, err := json.Marshal(nested); err == nil {
						return string(encoded)
					}
				}
			}
		}
		if strings.ContainsAny(current, "{[") {
			return core.SanitizeFailureMessage(current)
		}
		current = sanitizeURLsInText(current)
		current = redactSKToken.ReplaceAllString(current, "[redacted]")
		current = redactXoxbToken.ReplaceAllString(current, "[redacted]")
		current = redactGHPToken.ReplaceAllString(current, "[redacted]")
		return redactToolSensitiveText(current)
	default:
		return value
	}
}

func redactAuthValues(value string) string {
	value = redactOpaqueAuthValue(value, redactAuthHeaderStart)
	value = redactOpaqueAuthValue(value, redactDigestStart)
	value = redactBasicAuth.ReplaceAllString(value, "Basic [redacted]")
	return redactBearerToken.ReplaceAllString(value, "Bearer [redacted]")
}

func redactOpaqueAuthValue(value string, startPattern *regexp.Regexp) string {
	var out strings.Builder
	for cursor := 0; cursor < len(value); {
		location := startPattern.FindStringSubmatchIndex(value[cursor:])
		if location == nil {
			out.WriteString(value[cursor:])
			break
		}
		valueStart := cursor + location[1]
		out.WriteString(value[cursor:valueStart])
		lineEnd := valueStart
		for lineEnd < len(value) && value[lineEnd] != '\n' && value[lineEnd] != '\r' {
			lineEnd++
		}
		for lineEnd < len(value) {
			nextLineStart := lineEnd
			if value[nextLineStart] == '\r' {
				nextLineStart++
			}
			if nextLineStart >= len(value) || value[nextLineStart] != '\n' {
				break
			}
			nextLineStart++
			if nextLineStart >= len(value) || (value[nextLineStart] != ' ' && value[nextLineStart] != '\t') {
				break
			}
			lineEnd = nextLineStart
			for lineEnd < len(value) && value[lineEnd] != '\n' && value[lineEnd] != '\r' {
				lineEnd++
			}
		}
		suffix := ""
		if match := redactStatusSuffix.FindStringIndex(value[valueStart:lineEnd]); match != nil {
			suffix = value[valueStart+match[0] : valueStart+match[1]]
		}
		out.WriteString("[redacted]")
		out.WriteString(suffix)
		cursor = lineEnd
	}
	return out.String()
}
