package node

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestToolArgumentPreviewCompactsCommandsByUnicodeRunes(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"exactly 25", "1234567890123456789012345", "1234567890123456789012345"},
		{"26 characters", "12345678901234567890123456", "1234567890123456789012345…"},
		{"unicode boundary", "123456789012345678901234🎯x", "123456789012345678901234🎯…"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			arguments, _ := json.Marshal(map[string]string{"command": test.input})
			if got := ToolArgumentPreview("bash", arguments, ""); got != test.want {
				t.Fatalf("preview=%q want=%q (%d runes)", got, test.want, utf8.RuneCountInString(got))
			}
		})
	}
}

func TestToolArgumentPreviewFlattensLongMultilineCommands(t *testing.T) {
	arguments := json.RawMessage(`{"command":"python3 - <<'PY'\nimport pathlib\nprint('synthetic')\nPY"}`)
	got := ToolArgumentPreview("bash", arguments, "")
	if got != "python3 - <<'PY' import p…" || strings.ContainsAny(got, "\r\n") {
		t.Fatalf("multiline command preview=%q", got)
	}
}

func TestToolArgumentPreviewUsesWorkspaceRelativeAndCompactPaths(t *testing.T) {
	const workspace = "/worker/project"
	for _, test := range []struct {
		name, tool, path, workspace, want string
	}{
		{"relative read", "read", "src/config.toml", workspace, "src/config.toml"},
		{"absolute edit", "edit", "/worker/project/src/very/long/subdirectory/adapter.go", workspace, "src/…/adapter.go"},
		{"absolute without workspace", "read_file", "/private/tree/long/directory/adapter.go", "", "/…/adapter.go"},
	} {
		t.Run(test.name, func(t *testing.T) {
			arguments, _ := json.Marshal(map[string]string{"path": test.path})
			got := ToolArgumentPreview(test.tool, arguments, test.workspace)
			if got != test.want {
				t.Fatalf("path preview=%q want=%q", got, test.want)
			}
		})
	}
}

func TestToolArgumentPreviewPreservesExtensionOfVeryLongBasename(t *testing.T) {
	arguments, _ := json.Marshal(map[string]string{"path": "/worker/project/" + strings.Repeat("a", 100) + ".config.json"})
	got := ToolArgumentPreview("mcp__filesystem__read_file", arguments, "/worker/project")
	if !strings.Contains(got, "…") || !strings.HasSuffix(got, ".json") || utf8.RuneCountInString(got) > toolPreviewRunes+1 {
		t.Fatalf("long basename preview=%q (%d runes)", got, utf8.RuneCountInString(got))
	}
}

func TestToolArgumentPreviewRedactsBeforeTruncation(t *testing.T) {
	arguments := json.RawMessage(`{"command":"echo 123 token=synthetic-secret-after-prefix"}`)
	got := ToolArgumentPreview("bash", arguments, "")
	if strings.Contains(got, "synthetic-secret") || !strings.Contains(got, "token=[redacted]") {
		t.Fatalf("preview did not redact before truncation: %q", got)
	}

	laterSecret := json.RawMessage(`{"command":"123456789012345678901234567890 api_token=synthetic-secret"}`)
	got = ToolArgumentPreview("bash", laterSecret, "")
	if strings.Contains(got, "synthetic-secret") {
		t.Fatalf("secret beyond visible prefix was retained: %q", got)
	}
	sanitized, ok := SanitizeToolArguments(laterSecret)
	if !ok || strings.Contains(string(sanitized), "synthetic-secret") {
		t.Fatalf("normalized tool arguments were not redacted: %s ok=%v", sanitized, ok)
	}

	longPath, _ := json.Marshal(map[string]string{"path": "/worker/project/src/" + strings.Repeat("d", 30) + "/token=synthetic-secret/adapter.go"})
	pathPreview := ToolArgumentPreview("read", longPath, "/worker/project")
	if strings.Contains(pathPreview, "synthetic-secret") {
		t.Fatalf("path secret survived sanitization or middle truncation: %q", pathPreview)
	}
}

func TestToolArgumentPreviewRedactsURLAndCurlCredentialsBeforeTruncation(t *testing.T) {
	cases := []struct {
		name, tool, key, value string
	}{
		{"command URL userinfo", "bash", "command", "curl https://alice:fake-password@example.invalid/resource"},
		{"argument URL userinfo", "fetch", "url", "https://alice:fake-password@example.invalid/resource"},
		{"percent-encoded userinfo", "fetch", "url", "https://ali%63e:fake%2Dpassword@example.invalid/resource"},
		{"curl -u", "bash", "command", "curl -u alice:synthetic-password https://example.invalid/resource"},
		{"curl --user", "bash", "command", "curl --user=alice:synthetic-password https://example.invalid/resource"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			arguments, _ := json.Marshal(map[string]string{test.key: test.value})
			got := ToolArgumentPreview(test.tool, arguments, "")
			for _, credential := range []string{"alice", "ali%63e", "fake-password", "fake%2Dpassword", "synthetic-password"} {
				if strings.Contains(got, credential) {
					t.Fatalf("preview exposed credential %q: %q", credential, got)
				}
			}
			if !strings.Contains(got, "[redacted]") {
				t.Fatalf("preview omitted redaction marker: %q", got)
			}
		})
	}
}

func TestToolArgumentPreviewDoesNotSerializeUnknownArgumentsOrReadFilesystem(t *testing.T) {
	if got := ToolArgumentPreview("unknown_tool", json.RawMessage(`{"payload":{"secret":"synthetic"}}`), "/path/that/must/not/be/read"); got != "" {
		t.Fatalf("unknown schema was guessed into preview: %q", got)
	}
	if got := ToolArgumentPreview("read", json.RawMessage(`not-json`), ""); got != "" {
		t.Fatalf("invalid argument preview=%q", got)
	}
}
