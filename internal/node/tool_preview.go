package node

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const toolPreviewRunes = 25

var (
	toolSecretAssignment = regexp.MustCompile(`(?i)\b(api[_-]?key|api[_-]?token|access[_-]?(?:key|token)|private[_-]?key|authorization|bearer|token|secret|password|credential|client[_-]?secret|session[_-]?id|native[_-]?session[_-]?id|worker[_-]?(?:ref|id)|attempt[_-]?id|turn[_-]?id|node[_-]?id|request[_-]?id|call[_-]?id|analysis|reasoning|thought)\b(\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;&]+)`)
	toolSecretFlag       = regexp.MustCompile(`(?i)(--?(?:api-key|access-key|private-key|authorization|bearer|token|secret|password|credential|session-id|worker-ref|attempt-id|turn-id|node-id|request-id|call-id)(?:\s+|=))("[^"]*"|'[^']*'|[^\s,;&]+)`)
	toolBearerValue      = regexp.MustCompile(`(?i)\bBearer\s+[^\s,;&]+`)
	toolCredentialPrefix = regexp.MustCompile(`(?i)(?:sk-[a-z0-9_-]{8,}|gh[pousr]_[a-z0-9]{8,}|xox[baprs]-[a-z0-9-]{8,})`)
	toolRedactedValue    = regexp.MustCompile(`(?i)\b(?:api[_-]?(?:key|token)|access[_-]?(?:key|token)|private[_-]?key|authorization|bearer|token|secret|password|credential|client[_-]?secret|session[_-]?id|native[_-]?session[_-]?id|worker[_-]?(?:ref|id)|attempt[_-]?id|turn[_-]?id|node[_-]?id|request[_-]?id|call[_-]?id)\s*[:=]\s*\[redacted\]`)
	toolURLUserinfo      = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/@\s?#]+@`)
	toolCurlUserLong     = regexp.MustCompile(`(?i)(--user(?:=|\s+))("[^"]*"|'[^']*'|[^\s,;&]+)`)
	toolCurlUserShort    = regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_-])(-u(?:=|\s+)?)("[^"]*"|'[^']*'|[^\s,;&]+)`)
	toolReasoningTail    = regexp.MustCompile(`(?i)\b(analysis|reasoning|thought)\s*[:=].*$`)
)

// ToolArgumentPreview returns only one schema-recognized argument, after
// redaction of the entire value. It never serializes an arbitrary argument map.
func ToolArgumentPreview(tool string, raw json.RawMessage, workspace string) string {
	var arguments map[string]any
	if json.Unmarshal(raw, &arguments) != nil {
		return ""
	}
	toolKind := classifyPreviewTool(tool)
	keys := []string{}
	pathValue := false
	switch toolKind {
	case "shell":
		keys = []string{"command", "cmd"}
	case "file":
		keys = []string{"path", "file_path", "filePath", "filename"}
		pathValue = true
	default:
		keys = []string{"query", "url", "pattern", "path", "file_path", "filePath", "filename", "command", "cmd"}
	}
	for _, key := range keys {
		value, ok := arguments[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		value = sanitizeToolPreviewText(value)
		if pathValue || key == "path" || key == "file_path" || key == "filePath" || key == "filename" {
			value = relativeToolPath(value, workspace)
			return compactToolPath(value)
		}
		return truncateToolPreview(value)
	}
	return ""
}

func classifyPreviewTool(tool string) string {
	name := strings.ToLower(strings.TrimSpace(tool))
	last := name
	for _, separator := range []string{"__", ".", "/", ":"} {
		if index := strings.LastIndex(last, separator); index >= 0 {
			last = last[index+len(separator):]
		}
	}
	if last == "bash" || last == "shell" || last == "exec" || last == "command" || last == "run_command" || last == "exec_command" || last == "execute_command" {
		return "shell"
	}
	for _, fileTool := range []string{"read", "edit", "write", "open", "read_file", "edit_file", "write_file", "open_file", "file_read", "file_edit", "file_write", "list_directory", "list_files", "search_files", "apply_patch"} {
		if last == fileTool {
			return "file"
		}
	}
	return "other"
}

// redactToolSensitiveText masks credentials and private identifiers without
// normalizing whitespace. Normalized tool payloads and structured output use
// this boundary; only user-facing previews additionally flatten lines.
func redactToolSensitiveText(value string) string {
	value = toolSecretAssignment.ReplaceAllString(value, "$1$2[redacted]")
	value = toolSecretFlag.ReplaceAllString(value, "$1[redacted]")
	value = toolCurlUserLong.ReplaceAllString(value, "$1[redacted]")
	value = toolCurlUserShort.ReplaceAllString(value, "$1$2[redacted]")
	value = toolBearerValue.ReplaceAllString(value, "Bearer [redacted]")
	value = toolURLUserinfo.ReplaceAllString(value, "$1[redacted]@")
	value = toolCredentialPrefix.ReplaceAllString(value, "[redacted]")
	value = toolReasoningTail.ReplaceAllString(value, "$1=[redacted]")
	return value
}

// sanitizeToolPreviewText redacts the complete value before flattening and
// truncation so a credential beyond the visible prefix cannot leak.
func sanitizeToolPreviewText(value string) string {
	return strings.Join(strings.Fields(redactToolSensitiveText(value)), " ")
}

func relativeToolPath(path, workspace string) string {
	path = strings.ReplaceAll(strings.TrimSpace(path), `\`, "/")
	if path == "" || strings.HasPrefix(path, "[redacted]") {
		return path
	}
	if workspace == "" {
		return path
	}
	cleanWorkspace := filepath.Clean(workspace)
	cleanPath := filepath.Clean(path)
	if !filepath.IsAbs(cleanPath) {
		return filepath.ToSlash(cleanPath)
	}
	relative, err := filepath.Rel(cleanWorkspace, cleanPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(cleanPath)
	}
	return filepath.ToSlash(relative)
}

func truncateToolPreview(value string) string {
	runes := []rune(value)
	if len(runes) <= toolPreviewRunes {
		return value
	}
	return string(runes[:toolPreviewRunes]) + "…"
}

func compactToolPath(path string) string {
	path = strings.ReplaceAll(path, `\`, "/")
	path = strings.TrimSpace(path)
	runes := []rune(path)
	if len(runes) <= toolPreviewRunes {
		return path
	}
	base := path
	if index := strings.LastIndex(path, "/"); index >= 0 {
		base = path[index+1:]
	}
	baseRunes := []rune(base)
	if len(baseRunes) > toolPreviewRunes {
		return compactToolBasename(base)
	}
	prefix := ""
	if index := strings.Index(path, "/"); index >= 0 {
		prefix = path[:index]
	}
	candidate := "…/" + base
	if strings.HasPrefix(path, "/") {
		candidate = "/…/" + base
	} else if prefix != "" {
		candidate = prefix + "/…/" + base
	}
	if utf8.RuneCountInString(candidate) <= toolPreviewRunes+1 {
		return candidate
	}
	return compactToolBasename(base)
}

func compactToolBasename(base string) string {
	runes := []rune(base)
	if len(runes) <= toolPreviewRunes {
		return base
	}
	extension := filepath.Ext(base)
	if extension == base && strings.Count(base, ".") == 1 {
		extension = "" // A long dotfile has no extension to preserve.
	}
	extRunes := []rune(extension)
	if len(extRunes) > toolPreviewRunes-5 {
		// Preserve the informative end of an unusually long extension while
		// keeping the complete preview within the agreed bound.
		extRunes = extRunes[len(extRunes)-(toolPreviewRunes-5):]
		extension = string(extRunes)
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	stemRunes := []rune(stem)
	room := toolPreviewRunes + 1 - len(extRunes) - 1 // one middle ellipsis
	if room < 2 {
		room = 2
	}
	left := (room + 1) / 2
	right := room - left
	if left+right > len(stemRunes) {
		left = (len(stemRunes) + 1) / 2
		right = len(stemRunes) - left
		maxExtension := toolPreviewRunes + 1 - len(stemRunes) - 1
		if len(extRunes) > maxExtension {
			extRunes = extRunes[len(extRunes)-maxExtension:]
			extension = string(extRunes)
		}
	}
	return string(stemRunes[:left]) + "…" + string(stemRunes[len(stemRunes)-right:]) + extension
}
