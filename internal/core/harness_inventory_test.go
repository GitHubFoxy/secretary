package core

import (
	"errors"
	"testing"
	"time"
)

func TestObservedInventoryRejectsMissingModelAndReasoningPins(t *testing.T) {
	instance := HarnessInstance{
		ID: "macbook/codex", Node: "macbook", Kind: HarnessCodex, Version: "1.0.0",
		Authentication: HarnessAuthentication{Authenticated: true}, Status: HarnessReady,
		ModelIDs: []ObservedModelID{"gpt-5-codex"}, ReasoningLevels: []ObservedReasoningLevel{"high"},
	}
	if err := instance.ValidateSelection("gpt-5-codex", "high"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		model     string
		reasoning string
	}{
		{name: "model", model: "not-observed", reasoning: "high"},
		{name: "reasoning", model: "gpt-5-codex", reasoning: "low"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := instance.ValidateSelection(test.model, test.reasoning)
			if !errors.Is(err, ErrObservedPinUnavailable) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	inventory := HarnessInventorySnapshot{Node: "macbook", Instances: []HarnessInstance{instance}, ObservedAt: time.Now().UTC()}
	if err := inventory.ValidateSelection(instance.ID, "missing", "high"); !errors.Is(err, ErrObservedPinUnavailable) {
		t.Fatalf("inventory pin validation err=%v", err)
	}
	if selected, err := ValidateHarnessSelection(inventory, instance.ID, "gpt-5-codex", "high"); err != nil || selected.ID != instance.ID {
		t.Fatalf("selected=%#v err=%v", selected, err)
	}
	if err := inventory.ValidateSelection("macbook/fx", "", ""); !errors.Is(err, ErrHarnessUnavailable) {
		t.Fatalf("missing instance err=%v", err)
	}
}

func TestHarnessSelectionRejectsUnavailableInstanceWithoutFallback(t *testing.T) {
	instance := HarnessInstance{ID: "home-server/fx", Node: "home-server", Kind: HarnessFX, Version: "unavailable", Status: HarnessUnavailable}
	if err := instance.ValidateSelection("gpt-5.6-luna", "default"); !errors.Is(err, ErrHarnessUnavailable) {
		t.Fatalf("err=%v", err)
	}
}
