package node

import (
	"strings"
	"testing"
)

func openCodeAuthenticatedWorkerInstructions(marker string) string {
	return "Read the requested file using the read tool on EVERY request, even if you read it earlier. Never guess or reuse its old contents. " +
		"Definitions: CURRENT means the exact file contents just read for this request. FIRST means the exact file contents read on the very first request in this native session; preserve FIRST across later requests. " +
		"CURRENT and FIRST are placeholders: replace them with those actual file contents. Never output the literal words CURRENT or FIRST. " +
		"Use the following formats only after substituting the placeholders; each | is a literal separator. Output exactly one line, with no spaces, prefixes, explanations or other text.\n" +
		"First-request format: CURRENT|" + marker + "\n" +
		"Later-request format: CURRENT|FIRST|" + marker
}

func TestOpenCodeLiveWorkerPromptDefinesSubstitutionsBeforeFormats(t *testing.T) {
	marker := strings.Repeat("c", 32)
	prompt := openCodeAuthenticatedWorkerInstructions(marker)
	firstFormat := strings.Index(prompt, "First-request format:")
	for _, definition := range []string{"CURRENT means the exact file contents just read for this request", "FIRST means the exact file contents read on the very first request in this native session", "Never output the literal words CURRENT or FIRST"} {
		index := strings.Index(prompt, definition)
		if index < 0 || firstFormat < 0 || index >= firstFormat {
			t.Fatal("placeholder definitions must precede answer formats")
		}
	}
	if !strings.Contains(prompt, "read tool on EVERY request") || !strings.Contains(prompt, "First-request format: CURRENT|"+marker) || !strings.Contains(prompt, "Later-request format: CURRENT|FIRST|"+marker) {
		t.Fatal("live prompt lost actual read, private marker or original history requirements")
	}
}

// This structure contains safe flags/lengths only. Never attach Summary, file
// contents or the private system marker to diagnostics or test failures.
type openCodeWorkerAnswerMetadata struct {
	SummaryBytes         int
	ActualCurrentPresent bool
	OriginalFirstPresent bool
	ProfileMarkerPresent bool
	LiteralCurrent       bool
	LiteralFirst         bool
	SeparatorCount       int
}

func inspectOpenCodeWorkerAnswer(summary, current, first, marker string) openCodeWorkerAnswerMetadata {
	return openCodeWorkerAnswerMetadata{
		SummaryBytes:         len(summary),
		ActualCurrentPresent: current != "" && strings.Contains(summary, current),
		OriginalFirstPresent: first != "" && strings.Contains(summary, first),
		ProfileMarkerPresent: marker != "" && strings.Contains(summary, marker),
		LiteralCurrent:       strings.Contains(summary, "CURRENT"),
		LiteralFirst:         strings.Contains(summary, "FIRST"),
		SeparatorCount:       strings.Count(summary, "|"),
	}
}

func TestOpenCodeWorkerAnswerSafeDiagnostics(t *testing.T) {
	current, first, marker := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)
	for _, test := range []struct {
		name, answer string
		want         openCodeWorkerAnswerMetadata
	}{
		{"literal-current", "CURRENT|" + marker, openCodeWorkerAnswerMetadata{SummaryBytes: 40, ProfileMarkerPresent: true, LiteralCurrent: true, SeparatorCount: 1}},
		{"literal-first", current + "|FIRST|" + marker, openCodeWorkerAnswerMetadata{SummaryBytes: 71, ActualCurrentPresent: true, ProfileMarkerPresent: true, LiteralFirst: true, SeparatorCount: 2}},
		{"actual-first-turn", current + "|" + marker, openCodeWorkerAnswerMetadata{SummaryBytes: 65, ActualCurrentPresent: true, ProfileMarkerPresent: true, SeparatorCount: 1}},
		{"actual-resumed-turn", current + "|" + first + "|" + marker, openCodeWorkerAnswerMetadata{SummaryBytes: 98, ActualCurrentPresent: true, OriginalFirstPresent: true, ProfileMarkerPresent: true, SeparatorCount: 2}},
		{"empty", "", openCodeWorkerAnswerMetadata{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if inspectOpenCodeWorkerAnswer(test.answer, current, first, marker) != test.want {
				t.Fatal("safe answer metadata mismatch")
			}
		})
	}
	if inspectOpenCodeWorkerAnswer("unrelated", "", "", "").ActualCurrentPresent || inspectOpenCodeWorkerAnswer("unrelated", "", "", "").ProfileMarkerPresent {
		t.Fatal("empty expected values must not produce positive evidence")
	}
}
