package secretary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

const workerRef = "secretary"

var ErrNotStarted = errors.New("secretary: runtime not started")

// Runtime owns the single long-lived Secretary session. It never receives a
// raw store handle. Task lifecycle authority is exposed only through ctl.
type Runtime struct {
	node           *node.LocalNode
	capability     string
	mcpCommand     string
	mcpServerURL   string
	dataDir        string
	profile        func() node.ManagedProfile
	store          *core.Store
	conversationID string
	identity       core.SecretaryIdentity
	turnLoader     func(context.Context, string) (core.SecretaryTurn, error)

	mu           sync.Mutex
	session      node.Session
	busy         bool
	activeTurnID string
	queued       []string
	errors       chan error
}

func NewRuntime(local *node.LocalNode, capability string) *Runtime {
	return &Runtime{node: local, capability: capability, errors: make(chan error, 8)}
}

func (r *Runtime) AttachMCP(command, dataDir string) {
	r.AttachMCPServer(command, dataDir, "")
}

func (r *Runtime) AttachMCPServer(command, dataDir, serverURL string) {
	r.mu.Lock()
	r.mcpCommand = command
	r.mcpServerURL = serverURL
	r.dataDir = dataDir
	r.mu.Unlock()
}

func (r *Runtime) AttachProfile(profile func() node.ManagedProfile) {
	r.mu.Lock()
	r.profile = profile
	r.mu.Unlock()
}

func (r *Runtime) AttachConversation(store *core.Store, conversationID string) {
	r.mu.Lock()
	r.store = store
	r.conversationID = conversationID
	r.mu.Unlock()
}

// AttachIdentity binds a replaceable runtime to the durable Secretary identity.
// It does not persist or expose the native runtime session identifier.
func (r *Runtime) AttachIdentity(identity core.SecretaryIdentity) {
	r.mu.Lock()
	r.identity = identity
	r.mu.Unlock()
}

func (r *Runtime) Identity() core.SecretaryIdentity {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.identity
}

// QueueMessage persists an ordered Secretary turn. Execution is deliberately
// separate so a restart cannot silently claim an unknown runtime continuation.
func (r *Runtime) QueueMessage(ctx context.Context, text string) (core.SecretaryTurn, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return core.SecretaryTurn{}, errors.New("secretary: message is empty")
	}
	r.mu.Lock()
	store, identity := r.store, r.identity
	r.mu.Unlock()
	if store == nil || identity.ID == "" {
		return core.SecretaryTurn{}, errors.New("secretary: durable identity is not attached")
	}
	return store.EnqueueSecretaryTurn(ctx, identity.ID, text)
}

func (r *Runtime) Start(ctx context.Context) error {
	if r.node == nil {
		return errors.New("secretary: local node is required")
	}
	r.mu.Lock()
	if r.session != nil {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()
	if r.capability == "" {
		return errors.New("secretary: capability is required")
	}
	r.mu.Lock()
	mcpCommand, mcpServerURL, dataDir, profileFn, store, identity := r.mcpCommand, r.mcpServerURL, r.dataDir, r.profile, r.store, r.identity
	r.mu.Unlock()
	if store != nil && dataDir != "" {
		if _, err := store.LoadUserDocument(ctx, filepath.Join(dataDir, "user.md")); err != nil {
			return err
		}
	}
	if store != nil && identity.ID != "" {
		if err := store.RecoverSecretaryTurn(ctx, identity.ID, "runtime restarted before completion was proven"); err != nil {
			return err
		}
	}
	prompt := "Persistent Secretary session; the first prompt is supplied by the user."
	request := node.StartRequest{WorkerRef: workerRef, Task: prompt, DeferInitialPrompt: true}
	if profileFn != nil {
		request.Profile = profileFn()
		if store != nil && identity.ID != "" {
			if err := r.persistPolicySnapshot(ctx, request.Profile); err != nil {
				return err
			}
		}

	}
	if mcpCommand != "" {
		request.MCPServers = []node.MCPServer{node.SecretaryMCPServerAt(mcpCommand, dataDir, r.capability, mcpServerURL)}
	}
	session, err := r.node.Dispatch(ctx, request)
	if err != nil {
		return err
	}
	if request.Profile.Name != "" && store != nil {
		_, _ = store.RecordEvent(ctx, "secretary.profile_delivery", workerRef, "", session.ID(), map[string]string{
			"profile": request.Profile.Name, "profile_version": request.Profile.Version,
			"profile_hash": request.Profile.Hash, "delivery": request.Profile.Delivery, "runtime": request.Profile.Runtime,
		})
	}
	r.mu.Lock()
	r.session = session
	r.busy = !request.DeferInitialPrompt
	r.mu.Unlock()
	go r.consumeResults(session)
	go r.consumeActivity(session)
	// Telegram may persist a message while the Node session is still pairing.
	// Start consumes that durable backlog once the ACP session is ready.
	r.startNextDurable(context.Background())
	return nil
}

func (r *Runtime) Errors() <-chan error { return r.errors }

func (r *Runtime) HandleMessage(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("secretary: message is empty")
	}
	r.mu.Lock()
	session := r.session
	durable := r.store != nil && r.identity.ID != ""
	if durable {
		r.mu.Unlock()
		if strings.HasPrefix(text, "/q") {
			text = strings.TrimSpace(strings.TrimPrefix(text, "/q"))
			if text == "" {
				return errors.New("secretary: queued message is empty")
			}
		}
		_, err := r.QueueMessage(ctx, text)
		if err == nil {
			// The durable turn outlives the Telegram/HTTP acknowledgement context.
			r.startNextDurable(context.Background())
		}
		return err
	}
	if session == nil {
		r.mu.Unlock()
		return ErrNotStarted
	}
	if strings.HasPrefix(text, "/q") {
		queued := strings.TrimSpace(strings.TrimPrefix(text, "/q"))
		if queued == "" {
			r.mu.Unlock()
			return errors.New("secretary: queued message is empty")
		}
		r.queued = append(r.queued, queued)
		r.mu.Unlock()
		return nil
	}
	if !r.busy {
		r.busy = true
		r.mu.Unlock()
		r.runPrompt(ctx, session, text)
		return nil
	}
	r.mu.Unlock()

	injected, err := session.Steer(ctx, text)
	if err != nil {
		return err
	}
	if injected {
		return nil
	}
	// A runtime that cannot steer is handled at the next idle boundary.
	r.mu.Lock()
	r.queued = append(r.queued, text)
	startNow := !r.busy
	r.mu.Unlock()
	if startNow {
		r.startNext(ctx)
	}
	return nil
}

func (r *Runtime) consumeResults(session node.Session) {
	initial := true
	for result := range session.Result() {
		r.mu.Lock()
		if r.session != session {
			r.mu.Unlock()
			continue
		}
		store, conversationID, activeTurnID := r.store, r.conversationID, r.activeTurnID
		r.busy = false
		r.activeTurnID = ""
		r.mu.Unlock()
		if activeTurnID != "" && store != nil {
			state := core.SecretaryTurnSucceeded
			if result.Status == "failed" {
				state = core.SecretaryTurnFailed
			}
			if result.Status == "canceled" || result.Status == "cancelled" {
				state = core.SecretaryTurnCanceled
			}
			errorMessage := ""
			response := ""
			if state == core.SecretaryTurnSucceeded {
				response = result.Summary
			} else {
				errorMessage = result.Summary
			}
			if _, _, err := store.FinishSecretaryTurnWithResponse(context.Background(), activeTurnID, state, errorMessage, response); err != nil {
				r.reportError(err)
			}
		}
		if activeTurnID == "" && !initial && store != nil && conversationID != "" && result.Summary != "" {
			if _, err := store.AppendEntry(context.Background(), conversationID, core.EntrySecretary, result.Summary); err != nil {
				r.reportError(err)
			}
		}
		initial = false
		r.startNextDurable(context.Background())
		r.startNext(context.Background())
	}
}

func (r *Runtime) consumeActivity(session node.Session) {
	for activity := range session.Activity() {
		r.mu.Lock()
		current := r.session == session
		store, turnID := r.store, r.activeTurnID
		r.mu.Unlock()
		if !current {
			continue
		}
		// Secretary has no approval or input UI round-trip. Answer every reverse
		// request explicitly rather than leaving the harness blocked forever.
		if activity.Kind == node.ActivityPermission || activity.Kind == node.ActivityUserInput {
			responder, ok := session.(node.Responder)
			if !ok || strings.TrimSpace(activity.RequestID) == "" {
				r.reportError(errors.New("secretary: ACP interaction cannot be answered"))
				continue
			}
			response := "denied"
			if activity.Kind == node.ActivityUserInput {
				response = "cancel"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := responder.Respond(ctx, activity.RequestID, response)
			cancel()
			if err != nil {
				r.reportError(fmt.Errorf("secretary: deny ACP interaction: %w", err))
			}
			continue
		}
		if store == nil || turnID == "" {
			continue
		}
		var err error
		switch activity.Kind {
		case node.ActivityText:
			_, err = store.RecordSecretaryTextDelta(context.Background(), turnID, activity.Text)
		case node.ActivityThinkingSummary:
			summary := strings.TrimSpace(activity.Summary)
			if summary == "" {
				summary = strings.TrimSpace(activity.Text)
			}
			if safeSecretarySummary(summary) {
				_, err = store.RecordSecretaryThinkingSummary(context.Background(), turnID, summary)
			}
		case node.ActivityTool, node.ActivityToolCall:
			tool := strings.TrimSpace(activity.Tool)
			if tool == "" {
				tool = strings.TrimSpace(activity.Text)
			}
			if tool != "" {
				arguments, safe := node.SanitizeToolArguments(activity.Arguments)
				if !safe {
					continue
				}
				_, err = store.RecordSecretaryToolCall(context.Background(), turnID, tool, string(arguments))
			}
		case node.ActivityToolResult:
			tool := strings.TrimSpace(activity.Tool)
			if tool == "" {
				tool = strings.TrimSpace(activity.Text)
			}
			if tool != "" {
				status := strings.TrimSpace(activity.Status)
				if status == "" {
					status = "ok"
				}
				result, safe := node.SanitizeToolResult(activity.Result)
				if !safe {
					continue
				}
				errorText, safe := node.SanitizeToolResult(activity.Error)
				if !safe {
					continue
				}
				_, err = store.RecordSecretaryToolResult(context.Background(), turnID, tool, result, status, errorText)
			}
		}
		if err != nil {
			r.reportError(err)
		}
	}
}

func safeSecretarySummary(summary string) bool {
	if summary == "" || len(summary) > 1000 {
		return false
	}
	lower := strings.ToLower(summary)
	for _, marker := range []string{"chain-of-thought", "chain of thought", "raw thought", "internal reasoning", "thought process", "analysis:", "reasoning:", "thought:", "<think>", "</think>"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func sanitizeToolArguments(raw json.RawMessage) string {
	cleaned, ok := node.SanitizeToolArguments(raw)
	if !ok {
		return "{}"
	}
	return string(cleaned)
}

func sanitizeToolResult(result string) string {
	cleaned, ok := node.SanitizeToolResult(result)
	if !ok {
		return "[redacted]"
	}
	return cleaned
}

func (r *Runtime) reportError(err error) {
	if err == nil {
		return
	}
	select {
	case r.errors <- err:
	default:
	}
}

func (r *Runtime) ownsDurablePrompt(session node.Session, turnID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.session == session && r.activeTurnID == turnID && r.busy
}

// clearOwnedDurablePrompt releases only the turn owned by this exact ACP
// session. A late error from a stopped session must never clear a newer turn.
func (r *Runtime) clearOwnedDurablePrompt(session node.Session, turnID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session != session || r.activeTurnID != turnID {
		return false
	}
	r.busy = false
	r.activeTurnID = ""
	return true
}

func (r *Runtime) abandonDurablePrompt(store *core.Store, turnID string, release bool) {
	if release {
		if err := store.ReleaseSecretaryTurnClaims(context.Background(), turnID); err != nil {
			r.reportError(err)
		}
	}
	if _, err := store.FinishSecretaryTurn(context.Background(), turnID, core.SecretaryTurnInterrupted, "runtime/session changed before Prompt"); err != nil {
		r.reportError(err)
	}
	r.mu.Lock()
	if r.activeTurnID == turnID {
		r.busy = false
		r.activeTurnID = ""
	}
	r.mu.Unlock()
}

func (r *Runtime) persistPolicySnapshot(ctx context.Context, profile node.ManagedProfile) error {
	r.mu.Lock()
	store, identity := r.store, r.identity
	r.mu.Unlock()
	if store == nil || identity.ID == "" {
		return nil
	}
	harness, model, reasoning := profile.Runtime, profile.Model, profile.Reasoning
	if harness == "" {
		harness = identity.RuntimeHarness
	}
	if model == "" {
		model = identity.RuntimeModel
	}
	if reasoning == "" {
		reasoning = identity.RuntimeReasoning
	}
	if harness != "" && model != "" && reasoning != "" && (identity.RuntimeHarness != harness || identity.RuntimeModel != model || identity.RuntimeReasoning != reasoning) {
		updated, err := store.ReplaceSecretaryRuntime(ctx, identity.ID, harness, model, reasoning)
		if err != nil {
			return err
		}
		r.mu.Lock()
		r.identity = updated
		r.mu.Unlock()
	}
	return store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{
		Version: profile.Version, Harness: harness, Model: model, Reasoning: reasoning,
		ProfileVersion: profile.Version, ProfileName: profile.Name, ProfileHash: profile.Hash,
		ProfileContent: profile.Content, ProfileRuntime: profile.Runtime, ProfileModel: profile.Model,
		ProfileReasoning: profile.Reasoning, ProfileDelivery: profile.Delivery, AllowedTools: profile.AllowTools,
	})
}

func (r *Runtime) startNextDurable(ctx context.Context) {
	r.mu.Lock()
	if r.busy || r.session == nil || r.store == nil || r.identity.ID == "" {
		r.mu.Unlock()
		return
	}
	session, store, identity, profileFn, dataDir := r.session, r.store, r.identity, r.profile, r.dataDir
	r.mu.Unlock()
	if dataDir != "" {
		if _, err := store.LoadUserDocument(ctx, filepath.Join(dataDir, "user.md")); err != nil {
			r.reportError(err)
			return
		}
	}
	if profileFn != nil {
		if err := r.persistPolicySnapshot(ctx, profileFn()); err != nil {
			r.reportError(err)
			return
		}
		identity = r.Identity()
	}
	turn, err := store.StartNextSecretaryTurn(ctx, identity.ID)
	if errors.Is(err, core.ErrNotFound) {
		return
	}
	if err != nil {
		r.reportError(err)
		return
	}
	r.mu.Lock()
	if r.busy || r.session != session {
		r.mu.Unlock()
		if _, finishErr := store.FinishSecretaryTurn(context.Background(), turn.ID, core.SecretaryTurnInterrupted, "runtime changed before queued turn could be prompted"); finishErr != nil {
			r.reportError(finishErr)
		}
		go r.startNextDurable(context.Background())
		return
	}
	r.busy, r.activeTurnID = true, turn.ID
	r.mu.Unlock()
	r.runPrompt(ctx, session, turn.Input)
}

func (r *Runtime) startNext(ctx context.Context) {
	r.mu.Lock()
	if r.busy || len(r.queued) == 0 || r.session == nil {
		r.mu.Unlock()
		return
	}
	text := r.queued[0]
	r.queued = r.queued[1:]
	session := r.session
	r.busy = true
	r.mu.Unlock()
	r.runPrompt(ctx, session, text)
}

func (r *Runtime) runPrompt(ctx context.Context, session node.Session, text string) {
	go func() {
		r.mu.Lock()
		if r.session != session {
			r.mu.Unlock()
			return
		}
		store, turnID := r.store, r.activeTurnID
		r.mu.Unlock()
		prompt := text
		if store != nil && turnID != "" {
			loadTurn := r.turnLoader
			if loadTurn == nil {
				loadTurn = store.SecretaryTurn
			}
			turn, err := loadTurn(ctx, turnID)
			if err == nil {
				if strings.TrimSpace(turn.ContextSnapshot) == "" {
					err = errors.New("empty canonical Secretary context snapshot")
				} else {
					var canonical core.SecretaryContext
					if err = json.Unmarshal([]byte(turn.ContextSnapshot), &canonical); err == nil {
						err = canonical.Validate()
					}
					if err == nil {
						prompt, err = core.SecretaryContextPrompt(canonical, text)
					}
				}
			}
			if err != nil {
				if _, finishErr := store.FinishSecretaryTurn(context.Background(), turnID, core.SecretaryTurnFailed, "canonical Secretary context unavailable: "+err.Error()); finishErr != nil {
					r.reportError(finishErr)
				}
				owned := r.clearOwnedDurablePrompt(session, turnID)
				r.reportError(err)
				if owned {
					r.startNextDurable(context.Background())
				}
				return
			}
		}
		if store != nil && turnID != "" {
			if !r.ownsDurablePrompt(session, turnID) {
				r.abandonDurablePrompt(store, turnID, false)
				return
			}
			if err := store.BeginSecretaryPrompt(context.Background(), turnID); err != nil {
				if _, finishErr := store.FinishSecretaryTurn(context.Background(), turnID, core.SecretaryTurnFailed, "prompt could not start: "+err.Error()); finishErr != nil {
					r.reportError(finishErr)
				}
				owned := r.clearOwnedDurablePrompt(session, turnID)
				r.reportError(err)
				if owned {
					r.startNextDurable(context.Background())
				}
				return
			}
			if !r.ownsDurablePrompt(session, turnID) {
				r.abandonDurablePrompt(store, turnID, true)
				return
			}
		}
		if store != nil && turnID != "" && !r.ownsDurablePrompt(session, turnID) {
			return
		}
		if err := session.Prompt(ctx, prompt); err != nil {
			// Prompt may return after Stop or replacement. That error belongs to
			// the old session and must not mutate any current durable turn.
			if store != nil && turnID != "" && !r.ownsDurablePrompt(session, turnID) {
				return
			}
			if store != nil && turnID != "" {
				if releaseErr := store.ReleaseSecretaryTurnClaims(context.Background(), turnID); releaseErr != nil {
					r.reportError(releaseErr)
				}
				if _, finishErr := store.FinishSecretaryTurn(context.Background(), turnID, core.SecretaryTurnFailed, err.Error()); finishErr != nil {
					r.reportError(finishErr)
				}
			}
			owned := r.clearOwnedDurablePrompt(session, turnID)
			r.reportError(err)
			if owned {
				r.startNextDurable(context.Background())
			}
			return
		}
		if store != nil && turnID != "" && r.ownsDurablePrompt(session, turnID) {
			if err := store.AcceptSecretaryPrompt(context.Background(), turnID); err != nil {
				r.reportError(err)
			}
		}
	}()
}

func (r *Runtime) Stop(ctx context.Context) error {
	r.mu.Lock()
	session := r.session
	durable := r.store != nil && r.identity.ID != ""
	r.session = nil
	r.busy = false
	r.activeTurnID = ""
	if !durable {
		r.queued = nil
	}
	r.mu.Unlock()
	if session == nil {
		return nil
	}
	if err := session.Cancel(ctx); err != nil {
		return err
	}
	return r.node.Remove(workerRef)
}
