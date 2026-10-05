package node

import "strings"

// acpTurnAnswer is private to one prompt, never a session history accumulator.
// OpenCode v2.0.22 gives every assistant step its own messageId. end_turn
// completes the last step; earlier steps (including text before a tool) remain
// Activity. This contract must not be assumed for arbitrary ACP adapters.
type acpTurnAnswer struct {
	messages        map[string]*strings.Builder
	order           []string
	progress        map[string]bool
	missingIdentity bool
}

func newACPTurnAnswer() *acpTurnAnswer {
	return &acpTurnAnswer{messages: make(map[string]*strings.Builder), progress: make(map[string]bool)}
}

func (a *acpTurnAnswer) text(id, text string) {
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
	// Even unnamed native tool calls establish an execution boundary. Do not
	// infer their identity from title/kind/arguments, or split a message by text.
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
