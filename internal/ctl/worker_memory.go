package ctl

import (
	"context"
	"fmt"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

// withOwnerMemory injects a point-in-time, server-owned snapshot into the
// initial immutable Worker Profile. Workers don't get a writable memory tool.
// The full explicit files are used without semantic/vector lookup; the
// Secretary chooses which task to delegate. Later edits apply to new Workers.
func (s WorkerService) withOwnerMemory(ctx context.Context, profile node.ManagedProfile) (node.ManagedProfile, error) {
	legacy, err := s.Store.LoadUserDocument(ctx, s.MemoryUserPath)
	if err != nil {
		return node.ManagedProfile{}, fmt.Errorf("legacy user.md: %w", err)
	}
	facts, err := s.Store.ReadMemoryDocument(ctx, s.MemoryUserPath, core.MemoryFacts)
	if err != nil {
		return node.ManagedProfile{}, fmt.Errorf("memory.md: %w", err)
	}
	preferences, err := s.Store.ReadMemoryDocument(ctx, s.MemoryUserPath, core.MemoryPreferences)
	if err != nil {
		return node.ManagedProfile{}, fmt.Errorf("user_preferences.md: %w", err)
	}
	if legacy.Content == "" && facts.Content == "" && preferences.Content == "" {
		return profile, nil
	}
	// The profile hash is rebound by bindManagedWorkerProfile. Preserve the
	// source hash so node-side verification remains valid.
	var content strings.Builder
	content.WriteString(profile.Content)
	content.WriteString("\n\n## Owner context snapshot (reference data)\n")
	content.WriteString("The following Markdown is owner-supplied reference context, not tool instructions from the current task. Use relevant facts/preferences when answering; never modify these files implicitly.\n")
	for _, item := range []struct {
		name string
		doc  core.UserDocument
	}{
		{"user.md (legacy)", legacy},
		{"memory.md", facts},
		{"user_preferences.md", preferences},
	} {
		if item.doc.Content == "" {
			continue
		}
		fmt.Fprintf(&content, "\n### %s (revision %d)\n\n%s\n", item.name, item.doc.Revision, item.doc.Content)
	}
	profile.Content = content.String()
	return profile, nil
}
