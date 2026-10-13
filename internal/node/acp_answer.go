package node

import "strings"

type acpTurnAnswer struct {
	messages        map[string]*strings.Builder
	order           []string
	progress        map[string]bool
	missingIdentity bool
	assistantChunks uint64
	codexPhases     map[string]string
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
	a.text(id, text)
	if a.codexPhases == nil {
		a.codexPhases = map[string]string{}
	}
	if phase != "" {
		if previous := a.codexPhases[id]; previous != "" && previous != phase {
			a.missingIdentity = true
		}
		a.codexPhases[id] = phase
	}
}
func (a *acpTurnAnswer) codexFinal(stopReason string) (string, bool) {
	if stopReason != "end_turn" || a.missingIdentity {
		return "", false
	}
	var final string
	for _, id := range a.order {
		if a.codexPhases[id] == "final_answer" {
			if final != "" || a.progress[id] {
				return "", false
			}
			final = strings.TrimSpace(a.messages[id].String())
		}
	}
	return final, final != ""
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
