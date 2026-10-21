package node

import "strings"

const (
	codexLegacyPhaseNamespace = "codex"
	codexPhaseNamespace       = "jetbrains"
	codexPhaseExtension       = "air"
	codexPhaseFinalAnswer     = "final_answer"
)

type acpTurnAnswer struct {
	messages         map[string]*strings.Builder
	order            []string
	progress         map[string]bool
	missingIdentity  bool
	conflictingPhase bool
	assistantChunks  uint64
	codexPhases      map[string]string
}

func newACPTurnAnswer() *acpTurnAnswer {
	return &acpTurnAnswer{messages: make(map[string]*strings.Builder), progress: make(map[string]bool)}
}

func parseTerminalStopReason(value string) TerminalStopReason {
	switch value {
	case "end_turn":
		return TerminalStopReasonEndTurn
	case "max_tokens":
		return TerminalStopReasonMaxTokens
	case "refusal":
		return TerminalStopReasonRefusal
	case "cancelled", "canceled":
		return TerminalStopReasonCanceled
	case "":
		return TerminalStopReasonUnspecified
	default:
		return TerminalStopReasonOther
	}
}

func (a *acpTurnAnswer) text(id, text string) {
	a.assistantChunks++
	if id == "" {
		a.missingIdentity = true
		return
	}
	a.appendMessage(id, text)
}

func (a *acpTurnAnswer) appendMessage(id, text string) {
	message := a.messages[id]
	if message == nil {
		message = &strings.Builder{}
		a.messages[id] = message
		a.order = append(a.order, id)
	}
	message.WriteString(text)
}

func (a *acpTurnAnswer) tool() {
	for id := range a.messages {
		a.progress[id] = true
	}
}

func (a *acpTurnAnswer) final(stopReason string) (string, bool) {
	if stopReason != "end_turn" || a.missingIdentity || len(a.order) == 0 {
		return "", false
	}
	id := a.order[len(a.order)-1]
	if a.progress[id] {
		return "", false
	}
	text := strings.TrimSpace(a.messages[id].String())
	return text, text != ""
}

func (a *acpTurnAnswer) codexText(id, phase, text string) {
	a.assistantChunks++
	if id == "" {
		return
	}
	a.appendMessage(id, text)
	if a.codexPhases == nil {
		a.codexPhases = map[string]string{}
	}
	if phase != "" {
		if previous := a.codexPhases[id]; previous != "" && previous != phase {
			a.conflictingPhase = true
		}
		a.codexPhases[id] = phase
	}
}

func (a *acpTurnAnswer) codexConflictingPhase() {
	a.conflictingPhase = true
}

func (a *acpTurnAnswer) codexFinal(stopReason string) (string, bool) {
	if stopReason != "end_turn" || a.conflictingPhase {
		return "", false
	}
	var final string
	for _, id := range a.order {
		if a.codexPhases[id] == codexPhaseFinalAnswer {
			if final != "" || a.progress[id] {
				return "", false
			}
			final = strings.TrimSpace(a.messages[id].String())
		}
	}
	return final, final != ""
}

func codexMessagePhase(metadata map[string]any) (string, bool) {
	legacy, legacyPresent := codexPhaseValue(metadata[codexLegacyPhaseNamespace])
	extension, extensionPresent := codexPhaseValue(codexPhaseExtensionValue(metadata))
	if legacyPresent && extensionPresent && legacy != extension {
		return "", false
	}
	if extensionPresent {
		return extension, true
	}
	return legacy, true
}

func codexPhaseExtensionValue(metadata map[string]any) any {
	namespace, _ := metadata[codexPhaseNamespace].(map[string]any)
	extension, _ := namespace[codexPhaseExtension].(map[string]any)
	return extension
}

func codexPhaseValue(value any) (string, bool) {
	record, _ := value.(map[string]any)
	phase, ok := record["phase"].(string)
	if !ok {
		return "", false
	}
	phase = strings.TrimSpace(phase)
	if phase == "" {
		return "", false
	}
	return phase, true
}

func (a *acpTurnAnswer) collectedText() string {
	if a == nil {
		return ""
	}
	var b strings.Builder
	for _, id := range a.order {
		if msg := a.messages[id]; msg != nil {
			b.WriteString(msg.String())
		}
	}
	return strings.TrimSpace(b.String())
}
