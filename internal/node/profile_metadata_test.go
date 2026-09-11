package node

import "testing"

func TestProfileMetadataIsSafeForACPTranscript(t *testing.T) {
	metadata := profileMetadata(ManagedProfile{Name: "worker", Version: "v1", Hash: "h", Delivery: "native", Content: "secret prompt"})
	if metadata["secretaryProfile"] != "worker" || metadata["secretaryProfileDelivery"] != "native" || metadata["secretaryProfileVersion"] != "v1" || metadata["secretaryProfileHash"] != "h" {
		t.Fatalf("metadata=%#v", metadata)
	}
	if _, leaked := metadata["content"]; leaked {
		t.Fatal("profile prompt leaked into ACP metadata")
	}
	if profileMetadata(ManagedProfile{}) != nil {
		t.Fatal("empty profile should not add ACP metadata")
	}
}
