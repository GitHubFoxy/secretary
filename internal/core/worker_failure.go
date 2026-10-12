package core

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var (
	failureAuthorizationValue = regexp.MustCompile(`(?i)(\bauthorization\s*[:=]\s*|\bDigest\s+)`)
	failureStatusSuffix       = regexp.MustCompile(`(?i)\s+\(status [0-9]{3}\)$`)
	failureSecretAssignment   = regexp.MustCompile(`(?i)\b(api[_-]?(?:key|token)|access[_-]?(?:key|token)|private[_-]?key|authorization|bearer|token|secret|password|credential|client[_-]?secret|session[_-]?id|native[_-]?session[_-]?id|worker[_-]?(?:ref|id)|attempt[_-]?id|turn[_-]?id|node[_-]?id|request[_-]?id|call[_-]?id|analysis|reasoning|thought)\b(["']?\s*[:=]\s*)("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s,;&]+)`)
	failureSecretFlag         = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_-])(--?(?:api-key|access-key|private-key|authorization|bearer|token|secret|password|credential|session-id|worker-ref|attempt-id|turn-id|node-id|request-id|call-id)(?:\s+|=))(` + failureShellArgument + `)`)
	failureCurlUser           = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_-])(--user(?:=|\s+))(` + failureShellArgument + `)`)
	failureBearerToken        = regexp.MustCompile(`(?i)\bBearer\s+[^\s,;&]+`)
	failureBasicCredentials   = regexp.MustCompile(`(?i)\bBasic\s+[a-zA-Z0-9+/=]{4,}`)
	failureCredentialToken    = regexp.MustCompile(`(?i)\b(?:sk-[a-z0-9_-]+|gh[pousr]_[a-z0-9_-]+|xox[baprs]-[a-z0-9_-]+)\b`)
	failureURL                = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"'<>]+`)
)

const maxNestedFailureJSONDepth = 8

const failureShellArgument = `(?:[^\s"'\\]|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\\.)+`

var allowedFailureURLs = map[string]string{
	"https://chatgpt.com/explore/pro":              "https://chatgpt.com/explore/pro",
	"https://chatgpt.com/settings/usage":           "https://chatgpt.com/settings/usage",
	"https://chatgpt.com":                          "https://chatgpt.com",
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

// SanitizeFailureMessage is the shared boundary for bounded, user-visible and
// persisted Worker failure text. Unknown URLs and credentials are removed.
func SanitizeFailureMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	if json.Valid([]byte(message)) {
		var value any
		if json.Unmarshal([]byte(message), &value) == nil {
			if encoded, err := json.Marshal(sanitizeFailureJSON(value)); err == nil {
				message = string(encoded)
			}
		}
	} else {
		message = sanitizeFailureTextWithEmbeddedJSON(message, 0)
	}
	runes := []rune(message)
	if len(runes) > 1000 {
		return string(runes[:997]) + "..."
	}
	return message
}

func sanitizeFailureJSON(value any) any {
	return sanitizeFailureJSONAtDepth(value, 0)
}

func sanitizeFailureJSONAtDepth(value any, nestedDepth int) any {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			if sensitiveFailureJSONKey(key) {
				current[key] = "[redacted]"
			} else {
				current[key] = sanitizeFailureJSONAtDepth(child, nestedDepth)
			}
		}
		return current
	case []any:
		for index, child := range current {
			current[index] = sanitizeFailureJSONAtDepth(child, nestedDepth)
		}
		return current
	case string:
		trimmed := strings.TrimSpace(current)
		if json.Valid([]byte(trimmed)) {
			if nestedDepth >= maxNestedFailureJSONDepth {
				return "[redacted]"
			}
			if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
				var nested any
				if json.Unmarshal([]byte(trimmed), &nested) == nil {
					nested = sanitizeFailureJSONAtDepth(nested, nestedDepth+1)
					if encoded, err := json.Marshal(nested); err == nil {
						return string(encoded)
					}
				}
			} else if strings.HasPrefix(trimmed, `"`) {
				var nested string
				if json.Unmarshal([]byte(trimmed), &nested) == nil {
					nested = sanitizeFailureJSONAtDepth(nested, nestedDepth+1).(string)
					if encoded, err := json.Marshal(nested); err == nil {
						return string(encoded)
					}
				}
			}
		}
		return sanitizeFailureTextWithEmbeddedJSON(current, nestedDepth)
	default:
		return value
	}
}

func sanitizeFailureTextWithEmbeddedJSON(message string, nestedDepth int) string {
	// Remove the full Authorization/Digest value before splitting text around
	// embedded JSON, so JSON-looking fragments inside credentials can't break
	// the header's redaction context.
	message = redactFailureAuthorization(message)
	if sanitized, found := sanitizeFailureEmbeddedJSON(message, nestedDepth); found {
		return sanitized
	}
	return sanitizeFailureText(message)
}

func sanitizeFailureEmbeddedJSON(message string, nestedDepth int) (string, bool) {
	var output strings.Builder
	cursor, search := 0, 0
	found := false
	assignments := sensitiveFailureSpans(message)
	assignmentIndex := 0
	for search < len(message) {
		relative := strings.IndexAny(message[search:], "{[\"")
		if relative < 0 {
			break
		}
		start := search + relative
		for assignmentIndex < len(assignments) && assignments[assignmentIndex][1] <= start {
			assignmentIndex++
		}
		if assignmentIndex < len(assignments) && assignments[assignmentIndex][0] <= start && start < assignments[assignmentIndex][1] {
			search = assignments[assignmentIndex][1]
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(message[start:]))
		var value any
		if err := decoder.Decode(&value); err != nil {
			search = start + 1
			continue
		}
		if message[start] == '"' && !containsFailureJSONContainer(value, 0) {
			search = start + 1
			continue
		}
		end := start + int(decoder.InputOffset())
		if end <= start || !json.Valid([]byte(message[start:end])) {
			search = start + 1
			continue
		}
		output.WriteString(sanitizeFailureText(message[cursor:start]))
		if nestedDepth >= maxNestedFailureJSONDepth {
			output.WriteString("[redacted]")
		} else if encoded, err := json.Marshal(sanitizeFailureJSONAtDepth(value, nestedDepth+1)); err == nil {
			output.Write(encoded)
		} else {
			output.WriteString("[redacted]")
		}
		cursor = end
		search = end
		found = true
	}
	if !found {
		return message, false
	}
	output.WriteString(sanitizeFailureText(message[cursor:]))
	return output.String(), true
}

func containsFailureJSONContainer(value any, depth int) bool {
	switch current := value.(type) {
	case map[string]any, []any:
		return true
	case string:
		trimmed := strings.TrimSpace(current)
		if !json.Valid([]byte(trimmed)) {
			return false
		}
		if depth >= maxNestedFailureJSONDepth {
			return true
		}
		var nested any
		if json.Unmarshal([]byte(trimmed), &nested) == nil {
			return containsFailureJSONContainer(nested, depth+1)
		}
	}
	return false
}

func sensitiveFailureSpans(message string) [][2]int {
	var spans [][2]int
	for _, pattern := range []*regexp.Regexp{failureSecretAssignment, failureSecretFlag, failureCurlUser} {
		for _, match := range pattern.FindAllStringIndex(message, -1) {
			spans = append(spans, [2]int{match[0], match[1]})
		}
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i][0] == spans[j][0] {
			return spans[i][1] > spans[j][1]
		}
		return spans[i][0] < spans[j][0]
	})
	merged := spans[:0]
	for _, span := range spans {
		if len(merged) > 0 && span[0] <= merged[len(merged)-1][1] {
			if span[1] > merged[len(merged)-1][1] {
				merged[len(merged)-1][1] = span[1]
			}
			continue
		}
		merged = append(merged, span)
	}
	return merged
}

func sensitiveFailureJSONKey(key string) bool {
	compact := strings.ToLower(strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, key))
	for _, marker := range []string{"apikey", "apitoken", "accesstoken", "accesskey", "privatekey", "authorization", "bearer", "token", "secret", "password", "credential", "clientsecret", "sessionid", "workerref", "workerid", "attemptid", "turnid", "nodeid", "requestid", "callid", "analysis", "reasoning", "thought"} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func sanitizeFailureText(message string) string {
	message = sanitizeFailureURLs(message)
	message = redactFailureAuthorization(message)
	message = failureSecretAssignment.ReplaceAllStringFunc(message, func(match string) string {
		parts := failureSecretAssignment.FindStringSubmatch(match)
		replacement := "[redacted]"
		if strings.Contains(parts[2], `"`) {
			replacement = `"[redacted]"`
		} else if strings.Contains(parts[2], "'") && strings.HasPrefix(strings.TrimSpace(parts[3]), "'") {
			replacement = "'[redacted]'"
		}
		return parts[1] + parts[2] + replacement
	})
	message = failureSecretFlag.ReplaceAllString(message, "$1$2[redacted]")
	message = failureCurlUser.ReplaceAllString(message, "$1$2[redacted]")
	message = failureBearerToken.ReplaceAllString(message, "Bearer [redacted]")
	message = failureBasicCredentials.ReplaceAllString(message, "Basic [redacted]")
	return failureCredentialToken.ReplaceAllString(message, "[redacted]")
}

func redactFailureAuthorization(message string) string {
	var out strings.Builder
	redactContinuation := false
	for start := 0; start < len(message); {
		end := start + strings.IndexAny(message[start:], "\r\n")
		if end < start {
			end = len(message)
		}
		line := message[start:end]
		if redactContinuation && len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			out.WriteString("[redacted]")
		} else if location := failureAuthorizationValue.FindStringIndex(line); location != nil {
			out.WriteString(line[:location[0]])
			out.WriteString(line[location[0]:location[1]])
			out.WriteString("[redacted]")
			if suffix := failureStatusSuffix.FindStringIndex(line[location[1]:]); suffix != nil {
				out.WriteString(line[location[1]+suffix[0]:])
			}
			redactContinuation = true
		} else {
			out.WriteString(line)
			redactContinuation = false
		}
		if end == len(message) {
			break
		}
		if message[end] == '\r' {
			out.WriteByte('\r')
			end++
		}
		if end < len(message) && message[end] == '\n' {
			out.WriteByte('\n')
			end++
		}
		start = end
	}
	return out.String()
}

func sanitizeFailureURLs(message string) string {
	return failureURL.ReplaceAllStringFunc(message, func(match string) string {
		trailing := ""
		for len(match) > 0 && strings.ContainsRune(".,;:!?')]}", rune(match[len(match)-1])) {
			trailing = string(match[len(match)-1]) + trailing
			match = match[:len(match)-1]
		}
		parsed, err := url.Parse(match)
		if err != nil || (strings.ToLower(parsed.Scheme) != "http" && strings.ToLower(parsed.Scheme) != "https") {
			return trailing
		}
		normalized := "https://" + strings.ToLower(parsed.Hostname()) + strings.ToLower(strings.TrimRight(parsed.Path, "/"))
		if safe, ok := allowedFailureURLs[normalized]; ok {
			return safe + trailing
		}
		return trailing
	})
}

func SanitizeFailureCategory(category FailureCategory) FailureCategory {
	if category == "" || category.Valid() {
		return category
	}
	return FailureCategoryInternalError
}

func SanitizeWorkerFailure(failure WorkerFailure) WorkerFailure {
	failure.Category = SanitizeFailureCategory(failure.Category)
	failure.ProviderCode = SanitizeFailureProviderCode(failure.ProviderCode)
	failure.Message = SanitizeFailureMessage(failure.Message)
	failure.Technical = SanitizeFailureMessage(failure.Technical)
	return failure
}

func sanitizeAttemptOutcomeInput(input AttemptOutcomeInput) AttemptOutcomeInput {
	if input.Status == OutcomeSucceeded {
		return input
	}
	hadSummary := strings.TrimSpace(input.Summary) != ""
	input.Summary = SanitizeFailureMessage(input.Summary)
	input.ErrorCode = SanitizeFailureMessage(input.ErrorCode)
	input.ErrorMessage = SanitizeFailureMessage(input.ErrorMessage)
	input.Diagnostics = SanitizeFailureMessage(input.Diagnostics)
	input.FailureCode = SanitizeFailureProviderCode(input.FailureCode)
	input.FailureCategory = SanitizeFailureCategory(input.FailureCategory)
	if hadSummary && input.Classification == OutcomeFinal && input.Summary == "" {
		input.Summary = string(input.Status)
	}
	return input
}

// SanitizeWorkerCommandReceipt keeps authenticated Node text bounded and safe
// before the receipt is stored or used in logs and public responses.
func SanitizeWorkerCommandReceipt(receipt WorkerCommandReceipt) WorkerCommandReceipt {
	receipt.FailureCategory = SanitizeFailureCategory(receipt.FailureCategory)
	receipt.ProviderCode = SanitizeFailureProviderCode(receipt.ProviderCode)
	receipt.ErrorCode = SanitizeFailureMessage(receipt.ErrorCode)
	receipt.ErrorMessage = SanitizeFailureMessage(receipt.ErrorMessage)
	receipt.Technical = SanitizeFailureMessage(receipt.Technical)
	return receipt
}
