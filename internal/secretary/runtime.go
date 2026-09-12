package secretary

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"

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
	dataDir        string
	profile        func() node.ManagedProfile
	store          *core.Store
	conversationID string
	identity       core.SecretaryIdentity

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
	r.mu.Lock()
	r.mcpCommand = command
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
	mcpCommand, dataDir, profileFn, store, identity := r.mcpCommand, r.dataDir, r.profile, r.store, r.identity
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
	prompt := "You are the persistent personal Secretary. Use the server-owned Secretary tools for Task lifecycle operations. Never give Secretary capabilities to a Worker or Channel adapter."
	request := node.StartRequest{WorkerRef: workerRef, Task: prompt}
	if profileFn != nil {
		request.Profile = profileFn()
		if store != nil && identity.ID != "" {
			if err := r.persistPolicySnapshot(ctx, request.Profile); err != nil {
				return err
			}
		}
		if request.Profile.Content != "" {
			request.Task = "Start the Secretary session and follow the managed Profile."
			if request.Profile.Delivery != "native" {
				request.Task = "Read and follow the managed AGENTS.md before starting the Secretary session. Do not replace or weaken its instructions."
			}
		}
	}
	if mcpCommand != "" {
		request.MCPServers = []node.MCPServer{node.SecretaryMCPServer(mcpCommand, dataDir, r.capability)}
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
	r.busy = true
	r.mu.Unlock()
	go r.consumeResults(session)
	if store != nil && identity.ID != "" {
		go r.consumeActivity(session)
	}
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
	if session == nil {
		r.mu.Unlock()
		return ErrNotStarted
	}
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
			r.startNextDurable(ctx)
		}
		return err
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
		store, turnID := r.store, r.activeTurnID
		r.mu.Unlock()
		if store == nil || turnID == "" {
			continue
		}
		var err error
		switch activity.Kind {
		case node.ActivityText:
			err = func() error {
				_, e := store.RecordSecretaryTextDelta(context.Background(), turnID, activity.Text)
				return e
			}()
		case node.ActivityTool:
			err = func() error {
				_, e := store.RecordSecretaryToolCall(context.Background(), turnID, activity.Text, "")
				return e
			}()
		}
		if err != nil {
			r.reportError(err)
		}
	}
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
		store, turnID := r.store, r.activeTurnID
		r.mu.Unlock()
		prompt := text
		if store != nil && turnID != "" {
			turn, err := store.SecretaryTurn(ctx, turnID)
			if err == nil && turn.ContextSnapshot != "" {
				var canonical core.SecretaryContext
				if err = json.Unmarshal([]byte(turn.ContextSnapshot), &canonical); err == nil {
					prompt, err = core.SecretaryContextPrompt(canonical, text)
				}
			}
			if err != nil {
				if _, finishErr := store.FinishSecretaryTurn(context.Background(), turnID, core.SecretaryTurnFailed, "canonical Secretary context unavailable: "+err.Error()); finishErr != nil {
					r.reportError(finishErr)
				}
				r.mu.Lock()
				r.busy = false
				r.activeTurnID = ""
				r.mu.Unlock()
				r.reportError(err)
				return
			}
		}
		if err := session.Prompt(ctx, prompt); err != nil {
			r.mu.Lock()
			store, turnID := r.store, r.activeTurnID
			r.busy = false
			r.activeTurnID = ""
			r.mu.Unlock()
			if store != nil && turnID != "" {
				if _, finishErr := store.FinishSecretaryTurn(context.Background(), turnID, core.SecretaryTurnFailed, err.Error()); finishErr != nil {
					r.reportError(finishErr)
				}
			}
			r.reportError(err)
		}
	}()
}

func (r *Runtime) Stop(ctx context.Context) error {
	r.mu.Lock()
	session := r.session
	durable := r.store != nil && r.identity.ID != ""
	r.session = nil
	r.busy = false
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
