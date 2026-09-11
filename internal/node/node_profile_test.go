package node

import "testing"

func TestNativeProfileDeliveryRecognizesOpenCodeAdapters(t *testing.T) {
	profile := ManagedProfile{Name: "worker"}
	if !nativeProfileDelivery(OpenCodeRuntime{}, profile) {
		t.Fatal("OpenCode runtime should use native delivery")
	}
	if !nativeProfileDelivery(RuntimeRouter{DefaultHarness: "opencode"}, profile) {
		t.Fatal("OpenCode router should use native delivery")
	}
	if nativeProfileDelivery(ACPRuntime{}, profile) {
		t.Fatal("generic ACP runtime should not claim native delivery")
	}
}
