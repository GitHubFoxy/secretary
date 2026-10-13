package config

import (
	"io/fs"
	"strings"
	"testing"
)

func TestDefaultProfilesUseRussianUnlessUserExplicitlyRequestsAnotherLanguage(t *testing.T) {
	workerBytes, err := fs.ReadFile(defaults, "defaults/worker.md")
	if err != nil {
		t.Fatalf("read default worker profile: %v", err)
	}
	workerProfile := string(workerBytes)
	for _, phrase := range []string{
		"in Russian regardless of the language used in the task",
		"unless the user explicitly requested another response language for this report",
		"Do not infer a language preference from the language of the task",
	} {
		if !strings.Contains(workerProfile, phrase) {
			t.Errorf("worker.md missing language rule %q", phrase)
		}
	}
	if strings.Contains(workerProfile, "in the user's language") {
		t.Error("worker.md must not use the task language as the response language default")
	}

	secretaryBytes, err := fs.ReadFile(defaults, "defaults/secretary.md")
	if err != nil {
		t.Fatalf("read default secretary profile: %v", err)
	}
	secretaryProfile := string(secretaryBytes)
	for _, phrase := range []string{
		"Always answer the owner in Russian, regardless of the language used in their message.",
		"including any explicit user request about the language of that response",
		"preserve any explicit language request that applies to the requested response",
		"Do not infer a language preference from the language of the task",
		"This applies to both spawn_worker intent and message_worker text",
	} {
		if !strings.Contains(secretaryProfile, phrase) {
			t.Errorf("secretary.md missing language rule %q", phrase)
		}
	}
}
