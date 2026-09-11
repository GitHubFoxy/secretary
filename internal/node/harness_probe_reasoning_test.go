package node

import "testing"

func TestPlainModelListDoesNotBecomeReasoningInventory(t *testing.T) {
	if levels := parseObservedReasoning("model-a\nmodel-b\n"); len(levels) != 0 {
		t.Fatalf("plain model output leaked into reasoning inventory: %#v", levels)
	}
	levels := parseObservedReasoning("reasoning: medium, high\n")
	if len(levels) != 2 || levels[0] != "medium" || levels[1] != "high" {
		t.Fatalf("explicit reasoning levels=%#v", levels)
	}
}
