package core

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretaryContextPromptIncludesPersistentWorkerContinuationContract(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(t.TempDir(), "user.md")
	if _, err := store.SaveUserDocument(ctx, userPath, "owner preferences"); err != nil {
		t.Fatal(err)
	}
	profile := "Use server-owned Worker lifecycle tools."
	if err := store.SetSecretaryPolicySnapshot(ctx, SecretaryPolicySnapshot{
		Version: "v1", Harness: "fx", Model: "test", Reasoning: "default",
		ProfileVersion: "v1", ProfileName: "secretary", ProfileHash: "test",
		ProfileContent: profile,
	}); err != nil {
		t.Fatal(err)
	}
	canonical, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 20)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := SecretaryContextPrompt(canonical, "An online clothing shop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(prompt, profile+"\n\nCanonical server-owned Secretary context:") {
		t.Fatalf("owner profile was not preserved at the beginning of the prompt")
	}
	for _, required := range []string{
		"Worker continuation contract:",
		"worker_ref in recent_entries",
		"last_result_summary",
		"use message_worker with that exact worker_ref",
		"Do not spawn a new Worker for that answer",
		"ask the owner which Worker",
		"Worker Results are already delivered",
		"User turn:\nAn online clothing shop",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("canonical turn prompt missing Worker continuation rule %q", required)
		}
	}
}
