package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitMemoryAndLegacyContextSurviveRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "secretary.db")
	userPath := filepath.Join(dir, "user.md")
	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := store.SaveUserDocument(ctx, userPath, "# Legacy owner data\nKeep existing notes.\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, SecretaryPolicySnapshot{
		Version: "v1", Harness: "fx", Model: "m", Reasoning: "high",
		ProfileVersion: "v1", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "policy",
	}); err != nil {
		t.Fatal(err)
	}
	memory, err := store.ReadMemoryDocument(ctx, userPath, MemoryFacts)
	if err != nil {
		t.Fatal(err)
	}
	if memory.Content != "" || memory.Revision != 1 {
		t.Fatalf("fresh memory=%+v", memory)
	}
	prefs, err := store.ReadMemoryDocument(ctx, userPath, MemoryPreferences)
	if err != nil {
		t.Fatal(err)
	}
	// A normal conversation cannot authorize a long-term write.
	ordinary, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "What is my preferred language?")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, ordinary.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RememberOwnerMemory(ctx, userPath, MemoryFacts, "Automatically inferred", memory.Revision, person.ID, capability, ordinary.ID, ordinary.InputID); !errors.Is(err, ErrMemoryWriteNotRequested) {
		t.Fatalf("normal conversation wrote memory: %v", err)
	}
	if _, err := store.FinishSecretaryTurn(ctx, ordinary.ID, SecretaryTurnSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	request, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "Запомни, что я предпочитаю краткие ответы.")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, request.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RememberOwnerMemory(ctx, userPath, MemoryFacts, "Wrong capability", memory.Revision, person.ID, "wrong", request.ID, request.InputID); err == nil {
		t.Fatal("unauthorized memory write succeeded")
	}
	written, err := store.RememberOwnerMemory(ctx, userPath, MemoryPreferences, "Короткие ответы", prefs.Revision, person.ID, capability, request.ID, request.InputID)
	if err != nil {
		t.Fatal(err)
	}
	if written.Revision != prefs.Revision+1 || !strings.Contains(written.Content, "Короткие ответы") {
		t.Fatalf("preference not persisted: %+v", written)
	}
	if _, err := store.RememberOwnerMemory(ctx, userPath, MemoryPreferences, "Duplicate", prefs.Revision, person.ID, capability, request.ID, request.InputID); !errors.Is(err, ErrMemoryRevisionConflict) {
		t.Fatalf("stale repeated write err=%v", err)
	}
	fact, err := store.SaveMemoryDocumentIfRevision(ctx, userPath, MemoryFacts, "- Prefers Go\n", memory.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveMemoryDocumentIfRevision(ctx, userPath, MemoryFacts, "stale", memory.Revision); !errors.Is(err, ErrMemoryRevisionConflict) {
		t.Fatalf("stale revision err=%v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.ReadMemoryDocument(ctx, userPath, MemoryFacts)
	if err != nil || got.Content != fact.Content || got.Revision != fact.Revision {
		t.Fatalf("fact after restart=%+v err=%v", got, err)
	}
	gotPref, err := store.ReadMemoryDocument(ctx, userPath, MemoryPreferences)
	if err != nil || gotPref.Content != written.Content || gotPref.Revision != written.Revision {
		t.Fatalf("preference after restart=%+v err=%v", gotPref, err)
	}
	canonical, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 20)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.MemoryDocument == nil || canonical.MemoryDocument.Content != fact.Content ||
		canonical.PreferencesDocument == nil || canonical.PreferencesDocument.Content != written.Content ||
		canonical.UserDocument.Content != legacy.Content {
		t.Fatalf("lost canonical/legacy context: %+v", canonical)
	}
	legacyOnDisk, err := os.ReadFile(userPath)
	if err != nil || string(legacyOnDisk) != legacy.Content {
		t.Fatalf("legacy file was replaced: %q, %v", legacyOnDisk, err)
	}
	// Manually edited Markdown is reflected in the next turn and revision.
	if err := os.WriteFile(got.Path, []byte("- Updated directly\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	edited, err := store.ReadMemoryDocument(ctx, userPath, MemoryFacts)
	if err != nil || edited.Revision != got.Revision+1 || edited.Content != "- Updated directly\n" {
		t.Fatalf("editable Markdown not imported: %+v %v", edited, err)
	}
	if err := os.WriteFile(got.Path, []byte("bad\x00file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadMemoryDocument(ctx, userPath, MemoryFacts); !errors.Is(err, ErrInvalidMemory) {
		t.Fatalf("malformed file silently accepted: %v", err)
	}
	if _, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 20); err == nil {
		t.Fatal("malformed memory was hidden from context reconstruction")
	}
}

func TestMemoryKindAndExplicitOwnerGrammar(t *testing.T) {
	for _, input := range []string{"Запомни, что я люблю Go", "Please remember I prefer short replies", "/remember that I prefer Go", "Сохрани в память это правило"} {
		if !ExplicitMemoryRequest(input) {
			t.Fatalf("explicit request rejected: %q", input)
		}
	}
	for _, input := range []string{"I use Go", "What do you remember?", "Please explain memory", "Сегодня тепло"} {
		if ExplicitMemoryRequest(input) {
			t.Fatalf("normal conversation accepted: %q", input)
		}
	}
	if _, err := MemoryDocumentPath("user.md", "../secret"); !errors.Is(err, ErrInvalidMemory) {
		t.Fatalf("unknown memory kind err=%v", err)
	}
}
