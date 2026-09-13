package webapi

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/beruseruko/secretary/internal/acp"
)

// harnessDiagnosticDetail contains only a typed, redacted summary of one raw
// ACP record. It never carries the original JSON line or native session ID.
type harnessDiagnosticDetail struct {
	Sequence      int            `json:"sequence"`
	Direction     string         `json:"direction"`
	Method        string         `json:"method,omitempty"`
	SessionUpdate string         `json:"session_update,omitempty"`
	Tool          string         `json:"tool,omitempty"`
	Status        string         `json:"status,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
}

func readHarnessDiagnostics(dir, workerRef string) ([]harnessDiagnosticDetail, error) {
	if strings.TrimSpace(workerRef) == "" || filepath.Base(workerRef) != workerRef || strings.ContainsAny(workerRef, `/\\`) {
		return nil, errors.New("invalid worker reference")
	}
	paths, err := filepath.Glob(filepath.Join(dir, workerRef+".jsonl*"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	result := make([]harnessDiagnosticDetail, 0)
	for _, path := range paths {
		file, openErr := os.Open(path)
		if errors.Is(openErr, os.ErrNotExist) {
			continue
		}
		if openErr != nil {
			return nil, openErr
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() && len(result) < 500 {
			if detail, ok := parseHarnessDiagnostic(scanner.Bytes(), len(result)+1); ok {
				result = append(result, detail)
			}
		}
		scanErr := scanner.Err()
		_ = file.Close()
		if scanErr != nil {
			return nil, scanErr
		}
		if len(result) >= 500 {
			break
		}
	}
	return result, nil
}

func parseHarnessDiagnostic(line []byte, sequence int) (harnessDiagnosticDetail, bool) {
	var message acp.Message
	if json.Unmarshal(line, &message) != nil {
		return harnessDiagnosticDetail{}, false
	}
	params := map[string]any{}
	if len(message.Params) > 0 {
		_ = json.Unmarshal(message.Params, &params)
	}
	detail := harnessDiagnosticDetail{Sequence: sequence, Direction: "inbound", Method: message.Method}
	if message.Method != "" {
		detail.Direction = "outbound"
	}
	if update, ok := params["update"].(map[string]any); ok {
		if value := strings.TrimSpace(diagnosticValueString(update["sessionUpdate"])); value != "" {
			detail.SessionUpdate = value
		}
		if detail.SessionUpdate == "" {
			detail.SessionUpdate = strings.TrimSpace(diagnosticValueString(params["sessionUpdate"]))
		}
		detail.Tool = firstDiagnosticString(update, "title", "name", "tool")
		detail.Status = diagnosticValueString(update["status"])
	} else {
		detail.SessionUpdate = strings.TrimSpace(diagnosticValueString(params["sessionUpdate"]))
		detail.Tool = firstDiagnosticString(params, "title", "name", "tool")
		detail.Status = diagnosticValueString(params["status"])
	}
	if isThoughtUpdate(detail.SessionUpdate) {
		// Keep an audit marker, but never copy thought content into diagnostics.
		detail.SessionUpdate = "thinking"
		return detail, true
	}
	if cleaned, ok := sanitizeDiagnosticValue(params).(map[string]any); ok && len(cleaned) > 0 {
		detail.Details = cleaned
	}
	if detail.Method == "" && detail.SessionUpdate == "" && detail.Tool == "" && detail.Status == "" && len(detail.Details) == 0 {
		return harnessDiagnosticDetail{}, false
	}
	return detail, true
}

func diagnosticValueString(value any) string {
	text, _ := value.(string)
	return text
}

func firstDiagnosticString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := strings.TrimSpace(diagnosticValueString(value[key])); text != "" {
			return text
		}
	}
	return ""
}

func isThoughtUpdate(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(value, "thought") || strings.Contains(value, "reasoning") || strings.Contains(value, "chain")
}

func sanitizeDiagnosticValue(value any) any {
	switch current := value.(type) {
	case json.RawMessage:
		var decoded any
		if json.Unmarshal(current, &decoded) != nil {
			return "[redacted]"
		}
		return sanitizeDiagnosticValue(decoded)
	case map[string]any:
		result := make(map[string]any, len(current))
		for key, child := range current {
			if forbiddenDiagnosticKey(key) {
				continue
			}
			cleaned := sanitizeDiagnosticValue(child)
			if cleaned != nil {
				result[key] = cleaned
			}
		}
		return result
	case []any:
		result := make([]any, 0, len(current))
		for _, child := range current {
			if cleaned := sanitizeDiagnosticValue(child); cleaned != nil {
				result = append(result, cleaned)
			}
		}
		return result
	case string:
		trimmed := strings.TrimSpace(current)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			var nested any
			if json.Unmarshal([]byte(trimmed), &nested) == nil {
				cleaned := sanitizeDiagnosticValue(nested)
				if encoded, err := json.Marshal(cleaned); err == nil {
					return string(encoded)
				}
				return "[redacted]"
			}
		}
		if forbiddenDiagnosticString(current) {
			return "[redacted]"
		}
		if len(current) > 16<<10 {
			return current[:16<<10]
		}
		return current
	default:
		return value
	}
}

func forbiddenDiagnosticKey(key string) bool {
	compact := strings.ToLower(strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return character
		}
		return -1
	}, key))
	for _, marker := range []string{
		"secret", "credential", "callback", "token", "password", "authorization", "apikey", "accesskey", "privatekey",
		"runtimesession", "sessionid", "sessionidentifier", "runtimeid", "taskid", "token", "credential", "callback", "thought", "reasoning", "chainofthought", "analysis",
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func sanitizeDiagnosticText(value string) string {
	if cleaned, ok := sanitizeDiagnosticValue(value).(string); ok {
		return cleaned
	}
	return "[redacted]"
}

func forbiddenDiagnosticString(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"bearer ", "api_key=", "apikey=", "token=", "secret", "credential", "password=", "callback", "chain-of-thought", "internal reasoning", "thought process", "sk-", "ghp_", "xoxb-"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
