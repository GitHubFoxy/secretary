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

	lifecycleMu          sync.Mutex
	recoveryMu           sync.Mutex
	startMu              sync.Mutex
	mu                   sync.Mutex
	session              node.Session
	stopSession          node.Session
	stopPending          bool
	busy                 bool
	activeTurnID         string
	activeInputID        string
	nextPromptID         uint64
	activePromptID       uint64
	promptStartedID      uint64
	cancelRequestedID    uint64
	cancelInFlightID     uint64
	cancelDone           chan struct{}
	cancelTimeout        time.Duration
	sessionUncertain     bool
	replyContractVersion string
	launch               core.SecretaryRuntimeLaunch
	deliveredProfileKey  string
	queued               []string
	errors               chan error
}

func NewRuntime(local *node.LocalNode, capability string) *Runtime {
	return &Runtime{node: local, capability: capability, errors: make(chan error, 8), cancelTimeout: 5 * time.Second}
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
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if r.node == nil {
		return errors.New("secretary: local node is required")
	}
	r.mu.Lock()
	if r.stopPending {
		r.mu.Unlock()
		return errors.New("secretary: runtime cleanup pending; retry Stop before restart")
	}
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
	recoveryRequired := false
	if store != nil && identity.ID != "" {
		var err error
		recoveryRequired, err = store.SecretaryRecoveryRequired(ctx, identity.ID)
		if err != nil {
			return err
		}
	}
	prompt := "Persistent Secretary session; the first prompt is supplied by the user."
	request := node.StartRequest{WorkerRef: workerRef, Task: prompt, DeferInitialPrompt: true}
	if profileFn != nil {
		request.Profile = profileFn()
		if request.Profile.ReplyContractVersion != "" && request.Profile.ReplyContractVersion != core.SecretaryReplyContractAddressedV1 {
			return fmt.Errorf("secretary: unsupported reply contract version %q", request.Profile.ReplyContractVersion)
		}
		request.DrainOutputBeforeResult = request.Profile.ReplyContractVersion == core.SecretaryReplyContractAddressedV1
	}
	var launch core.SecretaryRuntimeLaunch
	registered := false
	if store != nil && identity.ID != "" && (request.Profile.Runtime == "opencode" || request.Profile.ReplyContractVersion == core.SecretaryReplyContractAddressedV1) {
		var err error
		launch, err = store.BeginSecretaryRuntime(ctx, identity.ID, r.capability, request.Profile.Runtime, request.Profile.Model, request.Profile.Reasoning)
		if err != nil {
			return err
		}
		defer func() {
			if !registered {
				_ = store.EndSecretaryRuntime(context.Background(), launch)
			}
		}()
		updated, err := store.SecretaryIdentity(ctx, identity.PersonID)
		if err != nil {
			return err
		}
		r.AttachIdentity(updated)
	}
	if mcpCommand != "" {
		server := node.SecretaryMCPServerAtWithReplyContract(mcpCommand, dataDir, r.capability, mcpServerURL, request.Profile.ReplyContractVersion)
		if launch.ObserverCapability != "" && mcpServerURL != "" {
			server.Env = append(server.Env, node.MCPEnv{Name: "SECRETARY_MCP_OBSERVER_CAPABILITY", Value: launch.ObserverCapability})
		}
		request.MCPServers = []node.MCPServer{server}
	}
	session, err := r.node.Dispatch(ctx, request)
	if err != nil {
		return err
	}
	if profileFn != nil && store != nil && identity.ID != "" {
		if err := r.persistPolicySnapshot(ctx, request.Profile); err != nil {
			_ = r.node.Remove(workerRef)
			return err
		}
	}
	if request.Profile.Name != "" && store != nil {
		_, _ = store.RecordEvent(ctx, "secretary.profile_delivery", workerRef, "", "", map[string]string{
			"profile": request.Profile.Name, "profile_version": request.Profile.Version,
			"profile_hash": request.Profile.Hash, "delivery": request.Profile.Delivery, "runtime": request.Profile.Runtime,
		})
	}
	r.mu.Lock()
	r.session = session
	r.busy = !request.DeferInitialPrompt
	r.sessionUncertain = recoveryRequired
	r.replyContractVersion = request.Profile.ReplyContractVersion
	r.launch = launch
	r.deliveredProfileKey = secretaryProfileDeliveryKey(request.Profile)
	registered = true
	r.mu.Unlock()
	if request.Profile.ReplyContractVersion == core.SecretaryReplyContractAddressedV1 {
		go r.consumeAddressedSession(session)
	} else {
		go r.consumeResults(session)
		go r.consumeActivity(session)
	}
	if recoveryRequired && store != nil && identity.ID != "" {
		if _, err := store.AppendEntry(ctx, identity.ConversationID, core.EntrySecretary, "После перезапуска я не могу подтвердить, что прежняя ACP-сессия остановилась. Сохранённые сообщения остаются в очереди. Убедитесь, что прежний процесс завершён, затем отправьте /recover, чтобы продолжить."); err != nil {
			r.reportError(err)
		}
	}
	r.startNextDurable(context.Background())
	return nil
}

func (r *Runtime) Errors() <-chan error { return r.errors }

func (r *Runtime) HandleMessage(ctx context.Context, text string) error {
	return r.handleMessage(ctx, text, nil)
}

func (r *Runtime) HandleInbound(ctx context.Context, entry core.ConversationEntry) error {
	return r.handleMessage(ctx, entry.Body, &entry)
}

func (r *Runtime) handleMessage(ctx context.Context, text string, entry *core.ConversationEntry) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("secretary: message is empty")
	}
	if text == "/recover" {
		return r.confirmRecovery(ctx)
	}
	queuedOnly := strings.HasPrefix(text, "/q")
	if queuedOnly {
		text = strings.TrimSpace(strings.TrimPrefix(text, "/q"))
		if text == "" {
			return errors.New("secretary: queued message is empty")
		}
	}
	r.mu.Lock()
	session := r.session
	durable := r.store != nil && r.identity.ID != ""
	if durable {
		store, identityID := r.store, r.identity.ID
		promptID := r.activePromptID
		var interruption *core.SecretaryInterruptionTarget
		if !queuedOnly && session != nil && r.busy && r.activeTurnID != "" && r.activePromptID != 0 && !r.sessionUncertain {
			interruption = &core.SecretaryInterruptionTarget{TurnID: r.activeTurnID, SessionID: session.ID(), RuntimeGeneration: r.launch.Generation}
		}
		r.mu.Unlock()
		var err error
		duplicate := false
		if entry != nil {
			_, duplicate, err = store.EnqueueSecretaryTurnForEntry(ctx, identityID, text, *entry, interruption)
		} else {
			_, err = r.QueueMessage(ctx, text)
		}
		if err != nil {
			return err
		}
		if !duplicate {
			if !queuedOnly {
				go r.cancelActivePrompt(context.Background(), session, promptID)
			}
			r.startNextDurable(context.Background())
		}
		return nil
	}
	if session == nil {
		r.mu.Unlock()
		return ErrNotStarted
	}
	if r.sessionUncertain {
		r.queued = append(r.queued, text)
		r.mu.Unlock()
		return nil
	}
	if queuedOnly {
		r.queued = append(r.queued, text)
		r.mu.Unlock()
		return nil
	}
	if !r.busy {
		r.busy = true
		r.beginPromptLocked()
		r.mu.Unlock()
		r.runPrompt(ctx, session, text)
		return nil
	}
	r.queued = append(r.queued, text)
	promptID := r.activePromptID
	r.mu.Unlock()
	r.cancelActivePrompt(ctx, session, promptID)
	return nil
}

func (r *Runtime) confirmRecovery(ctx context.Context) error {
	r.recoveryMu.Lock()
	defer r.recoveryMu.Unlock()
	r.mu.Lock()
	store, identity, session := r.store, r.identity, r.session
	activeTurnID := r.activeTurnID
	r.mu.Unlock()
	if store == nil || identity.ID == "" {
		return errors.New("secretary: recovery requires a durable identity")
	}
	required, err := store.SecretaryRecoveryRequired(ctx, identity.ID)
	if err != nil {
		return err
	}
	if !required {
		if session == nil {
			r.mu.Lock()
			stopPending := r.stopPending
			r.mu.Unlock()
			if stopPending {
				if err := r.Stop(ctx); err != nil {
					return err
				}
			}
			return r.Start(ctx)
		}
		_, err := store.AppendEntry(ctx, identity.ConversationID, core.EntrySecretary, "Нет незавершённого восстановления ACP-сессии.")
		return err
	}
	r.mu.Lock()
	if r.session == session {
		r.sessionUncertain = true
	}
	r.mu.Unlock()
	if err := r.Stop(ctx); err != nil {
		return err
	}
	if activeTurnID == "" {
		if activeTurn, err := store.ActiveSecretaryTurn(ctx, identity.ID); err == nil {
			activeTurnID = activeTurn.ID
		} else if !errors.Is(err, core.ErrNotFound) {
			return err
		}
	}
	if activeTurnID != "" {
		if _, err := store.FinishSecretaryTurn(ctx, activeTurnID, core.SecretaryTurnInterrupted, "ACP session closed during explicit recovery"); err != nil {
			turn, readErr := store.SecretaryTurn(ctx, activeTurnID)
			if readErr != nil || !turn.State.Terminal() {
				return errors.Join(err, readErr)
			}
		}
	}
	resolved, err := store.ConfirmSecretaryRecovery(ctx, identity.ID)
	if err != nil {
		return err
	}
	if !resolved {
		if restartErr := r.Start(ctx); restartErr != nil {
			return errors.Join(err, restartErr)
		}
		_, err := store.AppendEntry(ctx, identity.ConversationID, core.EntrySecretary, "Восстановление уже было подтверждено.")
		return err
	}
	_, err = store.AppendEntry(ctx, identity.ConversationID, core.EntrySecretary, "Восстановление подтверждено. Продолжаю сохранённые сообщения по порядку.")
	if err != nil {
		return err
	}
	return r.Start(ctx)
}

func (r *Runtime) beginPromptLocked() uint64 {
	r.nextPromptID++
	r.activePromptID = r.nextPromptID
	r.promptStartedID = 0
	r.cancelRequestedID = 0
	r.cancelInFlightID = 0
	r.cancelDone = nil
	return r.activePromptID
}

func (r *Runtime) cancelActivePrompt(ctx context.Context, session node.Session, promptID uint64) {
	r.mu.Lock()
	if session == nil || r.session != session || !r.busy || promptID == 0 || r.activePromptID != promptID || r.cancelRequestedID == promptID {
		r.mu.Unlock()
		return
	}
	r.cancelRequestedID = promptID
	if r.promptStartedID != promptID {
		r.mu.Unlock()
		return
	}
	done := make(chan struct{})
	r.cancelInFlightID, r.cancelDone = promptID, done
	store, turnID := r.store, r.activeTurnID
	timeout := r.cancelTimeout
	r.mu.Unlock()

	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	result := make(chan error, 1)
	go func() { result <- session.Cancel(cancelCtx) }()
	var err error
	select {
	case err = <-result:
		cancel()
		r.finishCancelAttempt(promptID, done)
	case <-cancelCtx.Done():
		err = cancelCtx.Err()
		cancel()
		r.mu.Lock()
		if r.session == session && r.activePromptID == promptID {
			r.sessionUncertain = true
		}
		r.mu.Unlock()
		go func() {
			<-result
			r.finishCancelAttempt(promptID, done)
		}()
	}
	if err != nil {
		r.reportCancelFailure(store, turnID, err)
	}
}

func (r *Runtime) finishCancelAttempt(promptID uint64, done chan struct{}) {
	r.mu.Lock()
	if r.cancelInFlightID == promptID && r.cancelDone == done {
		r.cancelInFlightID = 0
		r.cancelDone = nil
		close(done)
	}
	r.mu.Unlock()
}

func (r *Runtime) reportCancelFailure(store *core.Store, turnID string, err error) {
	r.reportError(fmt.Errorf("secretary: cancel active prompt: %w", err))
	if store != nil && turnID != "" {
		_, eventErr := store.RecordEventWithMetadata(context.Background(), core.EventInput{
			Kind: "secretary.interruption_failed", AggregateType: "secretary_turn", AggregateID: turnID,
			Source: "server", CorrelationID: turnID, Payload: map[string]string{"error": core.SanitizeFailureMessage(err.Error())},
		})
		if eventErr != nil {
			r.reportError(eventErr)
		}
	}
}

func (r *Runtime) consumeResults(session node.Session) {
	initial := true
	for result := range session.Result() {
		initial = r.consumeRuntimeResult(session, result, initial)
	}
}

func (r *Runtime) consumeAddressedSession(session node.Session) {
	activity, results := session.Activity(), session.Result()
	initial := true
	for activity != nil || results != nil {
		select {
		case item, ok := <-activity:
			if !ok {
				activity = nil
				continue
			}
			r.consumeActivityEvent(session, item)
		case result, ok := <-results:
			if !ok {
				results = nil
				continue
			}
			for activity != nil {
				select {
				case item, ok := <-activity:
					if !ok {
						activity = nil
						continue
					}
					r.consumeActivityEvent(session, item)
				default:
					goto activityDrained
				}
			}
		activityDrained:
			initial = r.consumeRuntimeResult(session, result, initial)
		}
	}
}

func (r *Runtime) consumeRuntimeResult(session node.Session, result node.Result, initial bool) bool {
	for {
		r.mu.Lock()
		if r.session != session {
			r.mu.Unlock()
			return initial
		}
		promptID := r.activePromptID
		if promptID != 0 && r.cancelInFlightID == promptID && r.cancelDone != nil {
			done := r.cancelDone
			r.mu.Unlock()
			<-done
			continue
		}
		break
	}
	store, conversationID, activeTurnID := r.store, r.conversationID, r.activeTurnID
	sessionUncertain := r.sessionUncertain
	activeInputID, replyContractVersion := r.activeInputID, r.replyContractVersion
	if store != nil && r.identity.ID != "" && activeTurnID == "" {
		r.mu.Unlock()
		return initial
	}
	r.busy = false
	r.activeTurnID = ""
	r.activeInputID = ""
	r.activePromptID = 0
	r.promptStartedID = 0
	r.cancelRequestedID = 0
	r.cancelInFlightID = 0
	r.cancelDone = nil
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
		if sessionUncertain && state != core.SecretaryTurnSucceeded {
			errorMessage = "ACP cancellation timed out; session outcome is uncertain; restart required"
		} else if state == core.SecretaryTurnSucceeded {
			response = result.Summary
		} else {
			errorMessage = result.Summary
		}
		var finishErr error
		if replyContractVersion == core.SecretaryReplyContractAddressedV1 {
			evidence := core.SecretaryCompletionEvidence{Branch: "invalid"}
			state = core.SecretaryTurnFailed
			if result.Status == "canceled" || result.Status == "cancelled" {
				state = core.SecretaryTurnCanceled
			}
			if sessionUncertain {
				state = core.SecretaryTurnFailed
			}
			if native := result.CompletionEvidence; native != nil {
				evidence.RPCSucceeded, evidence.DrainCompleted = native.RPCSucceeded, native.DrainCompleted
				evidence.AssistantChunks = native.AssistantChunks
				evidence.TerminalClass = string(native.StopReason)
				switch {
				case result.Status == "succeeded" && native.AllowsAssistantFinalText() && strings.TrimSpace(result.Summary) != "":
					evidence.Branch, evidence.TerminalValid, evidence.ResponsePresent = "assistant_final", true, true
					state = core.SecretaryTurnSucceeded
				case result.Status == "failed" && native.AllowsAddressedReplyOnly():
					evidence.Branch, evidence.TerminalValid = "addressed_reply_only", true
					state = core.SecretaryTurnSucceeded
				}
			}
			_, finishErr = store.FinishSecretaryTurnWithRequiredReply(context.Background(), activeTurnID, activeInputID, state, evidence)
		} else {
			_, _, finishErr = store.FinishSecretaryTurnWithResponse(context.Background(), activeTurnID, state, errorMessage, response)
		}
		if finishErr != nil {
			r.reportError(finishErr)
		}
	}
	if activeTurnID == "" && !initial && store != nil && conversationID != "" && result.Summary != "" {
		if _, err := store.AppendEntry(context.Background(), conversationID, core.EntrySecretary, result.Summary); err != nil {
			r.reportError(err)
		}
	}
	if !sessionUncertain {
		r.startNextDurable(context.Background())
		r.startNext(context.Background())
	}
	return false
}

func (r *Runtime) consumeActivity(session node.Session) {
	for activity := range session.Activity() {
		r.consumeActivityEvent(session, activity)
	}
}

func (r *Runtime) consumeActivityEvent(session node.Session, activity node.Activity) {
	r.mu.Lock()
	current := r.session == session
	store, turnID, inputID, replyContractVersion := r.store, r.activeTurnID, r.activeInputID, r.replyContractVersion
	r.mu.Unlock()
	if !current {
		return
	}
	if activity.Kind == node.ActivityPermission || activity.Kind == node.ActivityUserInput {
		responder, ok := session.(node.Responder)
		if !ok || strings.TrimSpace(activity.RequestID) == "" {
			r.reportError(errors.New("secretary: ACP interaction cannot be answered"))
			return
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
		return
	}
	if store == nil || turnID == "" {
		return
	}
	var err error
	switch activity.Kind {
	case node.ActivityText:
		if replyContractVersion == core.SecretaryReplyContractAddressedV1 {
			_, _, err = store.RecordAddressedSecretaryTextDelta(context.Background(), turnID, inputID, activity.Text)
		} else {
			_, err = store.RecordSecretaryTextDelta(context.Background(), turnID, activity.Text)
		}
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
				return
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
				return
			}
			errorText, safe := node.SanitizeToolResult(activity.Error)
			if !safe {
				return
			}
			_, err = store.RecordSecretaryToolResult(context.Background(), turnID, tool, result, status, errorText)
		}
	}
	if err != nil {
		r.reportError(err)
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

func (r *Runtime) clearOwnedDurablePrompt(session node.Session, turnID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session != session || r.activeTurnID != turnID {
		return false
	}
	r.busy = false
	r.activeTurnID = ""
	r.activeInputID = ""
	r.clearPromptLocked(r.activePromptID)
	return true
}

func (r *Runtime) clearPromptLocked(promptID uint64) {
	if promptID == 0 || r.activePromptID != promptID {
		return
	}
	r.activePromptID = 0
	r.promptStartedID = 0
	r.cancelRequestedID = 0
	r.cancelInFlightID = 0
	r.cancelDone = nil
}

func (r *Runtime) finishCancelledBeforePrompt(store *core.Store, session node.Session, turnID string, promptID uint64) {
	if store != nil && turnID != "" {
		if err := store.ReleaseSecretaryTurnClaims(context.Background(), turnID); err != nil {
			r.reportError(err)
		}
		if _, err := store.FinishSecretaryTurn(context.Background(), turnID, core.SecretaryTurnCanceled, "interrupted before prompt started"); err != nil {
			r.reportError(err)
		}
	}
	r.mu.Lock()
	if r.session == session && r.activePromptID == promptID {
		r.busy = false
		r.activeTurnID = ""
		r.activeInputID = ""
		r.clearPromptLocked(promptID)
	}
	durable := r.store != nil && r.identity.ID != ""
	r.mu.Unlock()
	if durable {
		r.startNextDurable(context.Background())
	} else {
		r.startNext(context.Background())
	}
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
		r.activeInputID = ""
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
	if profile.Runtime != "opencode" && profile.ReplyContractVersion == "" && harness != "" && model != "" && reasoning != "" && (identity.RuntimeHarness != harness || identity.RuntimeModel != model || identity.RuntimeReasoning != reasoning) {
		updated, err := store.ReplaceSecretaryRuntime(ctx, identity.ID, harness, model, reasoning)
		if err != nil {
			return err
		}
		r.AttachIdentity(updated)
	}
	return store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{
		Version: profile.Version, Harness: harness, Model: model, Reasoning: reasoning,
		ProfileVersion: profile.Version, ProfileName: profile.Name, ProfileHash: profile.Hash,
		ProfileContent: profile.Content, ProfileRuntime: profile.Runtime, ProfileModel: profile.Model,
		ProfileReasoning: profile.Reasoning, ProfileDelivery: profile.Delivery, ReplyContractVersion: profile.ReplyContractVersion, AllowedTools: profile.AllowTools,
	})
}

func secretaryProfileDeliveryKey(profile node.ManagedProfile) string {
	return node.HashProfile(profile.EffectivePrompt(), profile.Skills, profile.Name, profile.Runtime, profile.Model, profile.Reasoning, profile.Delivery, profile.ReplyContractVersion, strings.Join(profile.AllowTools, "\n"))
}

func (r *Runtime) startNextDurable(ctx context.Context) {
	r.startMu.Lock()
	defer r.startMu.Unlock()
	r.mu.Lock()
	if r.busy || r.session == nil || r.sessionUncertain || r.store == nil || r.identity.ID == "" {
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
		profile := profileFn()
		r.mu.Lock()
		replyContractVersion, deliveredKey := r.replyContractVersion, r.deliveredProfileKey
		r.mu.Unlock()
		if secretaryProfileDeliveryKey(profile) != deliveredKey {
			r.reportError(errors.New("secretary: Profile changed; restart the runtime before processing queued turns"))
			return
		}
		if profile.ReplyContractVersion != replyContractVersion {
			r.reportError(fmt.Errorf("secretary: reply contract changed from %q to %q; restart the runtime before processing queued turns", replyContractVersion, profile.ReplyContractVersion))
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
	launch := r.launch
	r.busy, r.activeTurnID, r.activeInputID = true, turn.ID, turn.InputID
	r.beginPromptLocked()
	r.mu.Unlock()
	if launch.ObserverCapability != "" {
		if err := store.BindSecretaryRuntimeTurn(ctx, launch, turn.ID, turn.InputID); err != nil {
			r.abandonDurablePrompt(store, turn.ID, false)
			r.reportError(err)
			return
		}
	}
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
	r.beginPromptLocked()
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
		if r.activePromptID == 0 {
			r.busy = true
			r.beginPromptLocked()
		}
		promptID := r.activePromptID
		store, turnID, activeInputID, replyContractVersion := r.store, r.activeTurnID, r.activeInputID, r.replyContractVersion
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
					if replyContractVersion == core.SecretaryReplyContractAddressedV1 && (turn.ID != turnID || turn.InputID == "" || turn.InputID != activeInputID) {
						err = errors.New("addressed reply v1 requires a server-issued active input identity")
					}
					var canonical core.SecretaryContext
					if err == nil {
						err = json.Unmarshal([]byte(turn.ContextSnapshot), &canonical)
					}
					if err == nil {
						err = canonical.Validate()
					}
					if err == nil {
						prompt, err = core.SecretaryContextPrompt(canonical, text)
					}
					if err == nil {
						prompt += fmt.Sprintf("\n\nActive reply identity (server-issued; use these exact values):\nsecretary_turn_id=%s\ninput_id=%s", turn.ID, turn.InputID)
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
		r.mu.Lock()
		if r.session != session || r.activePromptID != promptID {
			r.mu.Unlock()
			return
		}
		if r.cancelRequestedID == promptID {
			r.mu.Unlock()
			r.finishCancelledBeforePrompt(store, session, turnID, promptID)
			return
		}
		r.promptStartedID = promptID
		r.mu.Unlock()
		if err := session.Prompt(ctx, prompt); err != nil {
			r.awaitPromptCancel(promptID)
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

func (r *Runtime) awaitPromptCancel(promptID uint64) {
	r.mu.Lock()
	if promptID == 0 || r.cancelInFlightID != promptID || r.cancelDone == nil {
		r.mu.Unlock()
		return
	}
	done := r.cancelDone
	r.mu.Unlock()
	<-done
}

func (r *Runtime) Stop(ctx context.Context) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	r.mu.Lock()
	if r.session != nil {
		r.stopSession = r.session
		r.stopPending = true
	}
	if !r.stopPending {
		r.mu.Unlock()
		return nil
	}
	session := r.stopSession
	store, launch := r.store, r.launch
	durable := r.store != nil && r.identity.ID != ""
	sessionUncertain := r.sessionUncertain
	cancelDone := r.cancelDone
	r.session = nil
	r.busy = false
	r.activeTurnID = ""
	r.activeInputID = ""
	r.clearPromptLocked(r.activePromptID)
	if !durable {
		r.queued = nil
	}
	if cancelDone != nil {
		close(cancelDone)
	}
	r.mu.Unlock()

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var revokeErr, cancelErr, closeErr error
	if launch.ObserverCapability != "" {
		if store == nil {
			revokeErr = errors.New("secretary: runtime store is required for revoke")
		} else {
			revokeErr = store.EndSecretaryRuntime(cleanupCtx, launch)
		}
	}
	if session != nil {
		if !sessionUncertain {
			cancelErr = session.Cancel(cleanupCtx)
		}
		if _, exists := r.node.Session(workerRef); exists {
			closeErr = r.node.Remove(workerRef)
		} else {
			closeErr = session.Close()
		}
	}
	r.mu.Lock()
	if closeErr == nil {
		r.stopSession = nil
	}
	r.stopPending = revokeErr != nil || closeErr != nil
	if !r.stopPending {
		r.launch = core.SecretaryRuntimeLaunch{}
		r.sessionUncertain = false
	}
	r.mu.Unlock()
	return errors.Join(ctx.Err(), revokeErr, cancelErr, closeErr)
}
