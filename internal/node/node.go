package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"

	"github.com/beruseruko/secretary/internal/core"
)

type ActivityKind string

const (
	ActivityText            ActivityKind = "text"
	ActivityTool            ActivityKind = "tool"
	ActivityStatus          ActivityKind = "status"
	ActivityPermission      ActivityKind = "permission_request"
	ActivityUserInput       ActivityKind = "user_input_request"
	ActivityThinkingSummary ActivityKind = "thinking_summary"
	ActivityToolCall        ActivityKind = "tool_call"
	ActivityToolResult      ActivityKind = "tool_result"
)

type PendingRequest struct {
	RequestID string       `json:"request_id"`
	Kind      ActivityKind `json:"kind"`
}

type Activity struct {
	Kind          ActivityKind              `json:"kind"`
	Text          string                    `json:"text,omitempty"`
	RequestID     string                    `json:"request_id,omitempty"`
	Summary       string                    `json:"summary,omitempty"`
	RequestSchema json.RawMessage           `json:"request_schema,omitempty"`
	Tool          string                    `json:"tool,omitempty"`
	CallID        string                    `json:"call_id,omitempty"`
	OutputPresent bool                      `json:"output_present,omitempty"`
	Arguments     json.RawMessage           `json:"arguments,omitempty"`
	Result        string                    `json:"result,omitempty"`
	Error         string                    `json:"-"`
	Status        string                    `json:"status,omitempty"`
	Failure       *core.ToolFailureMetadata `json:"failure,omitempty"`
}

type TerminalCompletionContract string

type TerminalStopReason string

const (
	TerminalCompletionOpenCodeV2 TerminalCompletionContract = "opencode-v2-acp-terminal-v1"

	TerminalStopReasonUnspecified TerminalStopReason = ""
	TerminalStopReasonEndTurn     TerminalStopReason = "end_turn"
	TerminalStopReasonMaxTokens   TerminalStopReason = "max_tokens"
	TerminalStopReasonRefusal     TerminalStopReason = "refusal"
	TerminalStopReasonCanceled    TerminalStopReason = "canceled"
	TerminalStopReasonOther       TerminalStopReason = "other"
)

type TerminalCompletionEvidence struct {
	Contract        TerminalCompletionContract
	StopReason      TerminalStopReason
	RPCSucceeded    bool
	DrainCompleted  bool
	AssistantChunks uint64
}

func (e TerminalCompletionEvidence) AllowsAssistantFinalText() bool {
	return e.Contract == TerminalCompletionOpenCodeV2 &&
		e.StopReason == TerminalStopReasonEndTurn &&
		e.RPCSucceeded && e.DrainCompleted && e.AssistantChunks > 0
}

func (e TerminalCompletionEvidence) AllowsAddressedReplyOnly() bool {
	return e.Contract == TerminalCompletionOpenCodeV2 &&
		e.StopReason == TerminalStopReasonEndTurn &&
		e.RPCSucceeded && e.DrainCompleted && e.AssistantChunks == 0
}

type Result struct {
	Status             string                      `json:"status"`
	Summary            string                      `json:"summary"`
	FailureCode        string                      `json:"-"`
	FailureCategory    core.FailureCategory        `json:"-"`
	Technical          string                      `json:"-"`
	CompletionEvidence *TerminalCompletionEvidence `json:"-"`
}

type MCPEnv struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type MCPServer struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Env     []MCPEnv `json:"env"`
}

type StartRequest struct {
	AttemptID               string
	WorkerRef               string
	Task                    string
	Images                  []ImageInput
	Workspace               string
	RawLogPath              string
	MCPServers              []MCPServer
	Profile                 ManagedProfile
	HarnessInstance         core.HarnessInstance
	Model                   string
	Reasoning               string
	ApprovalPolicy          string
	DrainOutputBeforeResult bool
	DeferInitialPrompt      bool
	PendingRequests         []PendingRequest
	PendingRequestKinds     map[string]ActivityKind
	PendingRequestIDs       []string
}

func (r StartRequest) validateBinding() error {
	if r.HarnessInstance.Kind == "" && r.HarnessInstance.ID == "" && r.HarnessInstance.Node == "" {
		return nil
	}
	if err := r.HarnessInstance.Validate(); err != nil {
		return err
	}
	if r.Profile.Runtime != "" && r.Profile.Runtime != string(r.HarnessInstance.Kind) {
		return errors.New("node: managed Profile runtime conflicts with immutable HarnessInstance binding")
	}
	if r.Profile.Model != "" && r.Profile.Model != r.Model {
		return errors.New("node: managed Profile model conflicts with immutable execution model")
	}
	if r.Profile.Reasoning != "" && r.Profile.Reasoning != r.Reasoning {
		return errors.New("node: managed Profile reasoning conflicts with immutable execution reasoning")
	}
	return nil
}

func (r StartRequest) effectiveProfile() (ManagedProfile, error) {
	if err := r.validateBinding(); err != nil {
		return ManagedProfile{}, err
	}
	profile := r.Profile
	if r.HarnessInstance.Kind != "" {
		profile.Runtime = string(r.HarnessInstance.Kind)
		profile.Model = r.Model
		profile.Reasoning = r.Reasoning
	}
	return profile, nil
}

type Session interface {
	ID() string
	Prompt(context.Context, string) error
	Steer(context.Context, string) (injected bool, err error)
	Cancel(context.Context) error
	Activity() <-chan Activity
	Result() <-chan Result
	Close() error
}

type Runtime interface {
	Start(context.Context, StartRequest) (Session, error)
}

type Resumer interface {
	Resume(context.Context, StartRequest, string) (Session, error)
}

type Queueer interface {
	Queue(context.Context, string) error
}

type LocalNode struct {
	runtime Runtime
	mu      sync.Mutex
	workers map[string]Session
	// activeAttempts binds the currently registered native Session to its
	// owning Attempt; old durable checkpoints are not proof of ownership.
	activeAttempts map[string]string
	// Direct native Resume inherits an Attempt-scoped cancellable Context.
	// Keep its cancel function until the Session is removed or Node closes.
	resumeCancels            map[string]context.CancelFunc
	checkpoints              map[string]localCheckpoint
	artifactFetcher          ArtifactFetcher
	imageResponseStore       *LocalStore
	publishBroker            *workerPublishBroker
	artifactPublisherCommand string
	prestartStore            *LocalStore
	prestarts                map[string]*localPending
	workerGates              map[string]chan struct{}
}

type localCheckpoint struct {
	request  StartRequest
	nativeID string
}

func NewLocal(runtime Runtime) *LocalNode {
	return &LocalNode{runtime: runtime, workers: make(map[string]Session), activeAttempts: make(map[string]string), resumeCancels: make(map[string]context.CancelFunc), checkpoints: make(map[string]localCheckpoint), prestarts: make(map[string]*localPending), workerGates: make(map[string]chan struct{})}
}

// SetPrestartStore binds local pre-native cancellation to protected Node-owned
// durable state. Production binds the same Node state store used for image ACKs.
func (n *LocalNode) SetPrestartStore(store *LocalStore) {
	n.mu.Lock()
	n.prestartStore = store
	n.mu.Unlock()
}

// SetImageResponseStore binds a durable Node-owned checkpoint file. It must
// reside in protected Node state, never in a Worker-controlled workspace.
func (n *LocalNode) SetImageResponseStore(store *LocalStore) {
	n.mu.Lock()
	n.imageResponseStore = store
	n.mu.Unlock()
}

func (n *LocalNode) ImageResponseStore() *LocalStore {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.imageResponseStore
}

// SetLocalArtifactPublisher wires a private Server-side Attempt-validated
// publication saver into the same path-only broker used by remote Nodes.
func (n *LocalNode) SetLocalArtifactPublisher(save LocalArtifactSaver, command string) error {
	if command == "" {
		return errors.New("node artifact: local MCP executable required")
	}
	broker, err := newLocalWorkerPublishBroker(save)
	if err != nil {
		return err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.publishBroker != nil {
		_ = broker.Close()
		return errors.New("node artifact: local publisher already set")
	}
	n.publishBroker = broker
	n.artifactPublisherCommand = command
	return nil
}

// Called with LocalNode.mu held; each Attempt gets a private, unguessable
// registration that is revoked on terminal Result, Close or launch failure.
func (n *LocalNode) bindLocalPublisher(request *StartRequest) (func(), error) {
	if n.publishBroker == nil {
		return func() {}, nil
	}
	if request.WorkerRef == "" || request.AttemptID == "" || request.Workspace == "" {
		return nil, errors.New("node artifact: local Attempt binding incomplete")
	}
	endpoint, err := n.publishBroker.Register(request.WorkerRef, request.AttemptID, request.Workspace)
	if err != nil {
		return nil, err
	}
	request.MCPServers = append(request.MCPServers, MCPServer{
		Name: "secretary_artifacts", Command: n.artifactPublisherCommand,
		Args: []string{"--artifact-mcp"},
		Env:  []MCPEnv{{Name: "SECRETARY_ARTIFACT_BROKER_URL", Value: endpoint}},
	})
	return func() { n.publishBroker.Unregister(request.AttemptID) }, nil
}

type localPublishedSession struct {
	Session
	result chan Result
	once   sync.Once
	revoke func()
}

func (s *localPublishedSession) Result() <-chan Result { return s.result }

// Preserve native session extension behavior (ACP response, image steering,
// queued input and image prompts) across the terminal-publication wrapper.
// An unsupported extension fails explicitly rather than silently dropping it.
func (s *localPublishedSession) Respond(ctx context.Context, requestID, response string) error {
	if underlying, ok := s.Session.(Responder); ok {
		return underlying.Respond(ctx, requestID, response)
	}
	return errors.New("node: local native session cannot respond to input")
}
func (s *localPublishedSession) PromptImages(ctx context.Context, text string, images []ImageInput) error {
	if underlying, ok := s.Session.(ImagePromptSession); ok {
		return underlying.PromptImages(ctx, text, images)
	}
	return errors.New("image_input_unsupported: local session cannot prompt with images")
}
func (s *localPublishedSession) SteerImages(ctx context.Context, text string, images []ImageInput) (bool, error) {
	if underlying, ok := s.Session.(ImageSteeringSession); ok {
		return underlying.SteerImages(ctx, text, images)
	}
	return false, errors.New("image_input_unsupported: local session cannot steer images")
}
func (s *localPublishedSession) QueueImages(ctx context.Context, text string, images []ImageInput) error {
	if underlying, ok := s.Session.(ImageQueueSession); ok {
		return underlying.QueueImages(ctx, text, images)
	}
	return errors.New("image_input_unsupported: local session cannot queue images")
}
func (s *localPublishedSession) Queue(ctx context.Context, text string) error {
	if underlying, ok := s.Session.(Queueer); ok {
		return underlying.Queue(ctx, text)
	}
	return errors.New("node: local native session cannot queue")
}
func (s *localPublishedSession) Close() error {
	s.once.Do(s.revoke)
	return s.Session.Close()
}
func wrapLocalPublishedSession(session Session, revoke func()) Session {
	out := &localPublishedSession{Session: session, result: make(chan Result, 1), revoke: revoke}
	go func() {
		defer close(out.result)
		defer out.once.Do(out.revoke)
		for result := range session.Result() {
			out.result <- result
			// A native terminal Result ends this Attempt's publication authority,
			// even if the provider leaves its result channel open until Close.
			out.once.Do(out.revoke)
			return
		}
	}()
	return out
}

func (n *LocalNode) SetArtifactFetcher(fetcher ArtifactFetcher) {
	n.mu.Lock()
	n.artifactFetcher = fetcher
	n.mu.Unlock()
}

func (n *LocalNode) prepareInput(ctx context.Context, request *StartRequest) error {
	n.mu.Lock()
	fetch := n.artifactFetcher
	n.mu.Unlock()
	return prepareLocalInput(ctx, request, fetch)
}

func prepareLocalInput(ctx context.Context, request *StartRequest, fetch ArtifactFetcher) error {
	text, images, err := PrepareArtifactInput(ctx, request.Workspace, request.Task, fetch)
	if err != nil {
		return err
	}
	if len(images) > 0 && !supportsImageHarness(request.HarnessInstance) {
		return errors.New("image_input_unsupported: selected local Worker harness does not support images")
	}
	request.Task, request.Images = text, images
	return nil
}

func (n *LocalNode) PrepareWorkerSteering(ctx context.Context, workerRef, text string) (string, []ImageInput, error) {
	if len(ArtifactIDs(text)) == 0 {
		return text, nil, nil
	}
	n.mu.Lock()
	fetch := n.artifactFetcher
	workspace := ""
	allowed := false
	for _, mapping := range n.checkpoints {
		if mapping.request.WorkerRef == workerRef {
			workspace = mapping.request.Workspace
			allowed = supportsImageHarness(mapping.request.HarnessInstance)
		}
	}
	n.mu.Unlock()
	if workspace == "" {
		return "", nil, fmt.Errorf("artifact: local Worker workspace not found")
	}
	preparedText, images, err := PrepareArtifactInput(ctx, workspace, text, fetch)
	if err != nil {
		return "", nil, err
	}
	if len(images) > 0 && !allowed {
		return "", nil, errors.New("image_input_unsupported: local Worker harness does not advertise image input")
	}
	return preparedText, images, nil
}

func (n *LocalNode) Dispatch(ctx context.Context, request StartRequest) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	// Same-Worker commands are serialized, but media download/polling and
	// runtime Start never hold LocalNode's global mutex.
	unlock, err := n.lockWorker(ctx, request.WorkerRef)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if request.Workspace == "" {
		workspace, e := os.MkdirTemp("", "secretary-worker-")
		if e != nil {
			return nil, e
		}
		request.Workspace = workspace
	}
	n.mu.Lock()
	_, exists := n.workers[request.WorkerRef]
	hasCheckpoint := false
	if request.AttemptID != "" {
		for _, c := range n.checkpoints {
			if c.request.WorkerRef == request.WorkerRef {
				hasCheckpoint = true
				break
			}
		}
	}
	n.mu.Unlock()
	if exists {
		return nil, errors.New("node: worker already exists")
	}
	if hasCheckpoint {
		return nil, ErrRuntimeSessionUnavailable
	}
	prepCtx, finish, err := n.beginLocalPrestart(ctx, request.AttemptID, request.WorkerRef)
	if err != nil {
		return nil, err
	}
	defer finish()
	if err := n.prepareInput(prepCtx, &request); err != nil {
		return nil, err
	}
	if request.Profile.Name != "" && !nativeProfileDelivery(n.runtime, request.Profile) {
		if _, err := request.Profile.MaterializeInstructions(request.Workspace); err != nil {
			return nil, err
		}
	}
	if err := n.commitLocalHandoff(prepCtx, request.AttemptID, request.WorkerRef); err != nil {
		return nil, err
	}
	n.mu.Lock()
	revoke, err := n.bindLocalPublisher(&request)
	n.mu.Unlock()
	if err != nil {
		return nil, err
	}
	session, err := n.runtime.Start(ctx, request)
	if err != nil {
		revoke()
		return nil, err
	}
	n.mu.Lock()
	hasPublisher := n.publishBroker != nil
	n.mu.Unlock()
	if hasPublisher {
		session = wrapLocalPublishedSession(session, revoke)
	}
	n.mu.Lock()
	n.workers[request.WorkerRef] = session
	n.activeAttempts[request.WorkerRef] = request.AttemptID
	if request.AttemptID != "" {
		n.checkpoints[request.AttemptID] = localCheckpoint{request: request, nativeID: session.ID()}
	}
	n.mu.Unlock()
	return session, nil
}

func (n *LocalNode) Continue(ctx context.Context, request StartRequest, previousAttemptID string) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	if request.AttemptID == "" || request.AttemptID == previousAttemptID {
		return nil, ErrRuntimeSessionUnavailable
	}
	unlock, err := n.lockWorker(ctx, request.WorkerRef)
	if err != nil {
		return nil, err
	}
	defer unlock()
	n.mu.Lock()
	checkpoint, ok := n.checkpoints[previousAttemptID]
	fetch := n.artifactFetcher
	n.mu.Unlock()
	prior := checkpoint.request
	if request.Workspace == "" && ok {
		request.Workspace = prior.Workspace
	}
	if !ok || checkpoint.nativeID == "" || prior.WorkerRef != request.WorkerRef || prior.Workspace != request.Workspace ||
		!reflect.DeepEqual(prior.Profile, request.Profile) || !reflect.DeepEqual(prior.HarnessInstance, request.HarnessInstance) ||
		prior.Model != request.Model || prior.Reasoning != request.Reasoning || prior.ApprovalPolicy != request.ApprovalPolicy {
		return nil, ErrRuntimeSessionUnavailable
	}
	resumer, ok := n.runtime.(Resumer)
	if !ok {
		return nil, ErrRuntimeSessionUnavailable
	}
	prepCtx, finish, err := n.beginLocalPrestart(ctx, request.AttemptID, request.WorkerRef)
	if err != nil {
		return nil, err
	}
	defer finish()
	// Do not hold n.mu while waiting in awaitVideoProcessing: other Workers'
	// Dispatch, Session, Steer and Cancel remain independent.
	if err := prepareLocalInput(prepCtx, &request, fetch); err != nil {
		return nil, err
	}
	if err := n.commitLocalHandoff(prepCtx, request.AttemptID, request.WorkerRef); err != nil {
		return nil, err
	}
	n.mu.Lock()
	previous, hadPrevious := n.workers[request.WorkerRef]
	n.mu.Unlock()
	if hadPrevious {
		if err := previous.Close(); err != nil {
			return nil, err
		}
		n.mu.Lock()
		if current, ok := n.workers[request.WorkerRef]; ok && current == previous {
			delete(n.workers, request.WorkerRef)
			delete(n.activeAttempts, request.WorkerRef)
			if cancel := n.resumeCancels[request.WorkerRef]; cancel != nil {
				cancel()
				delete(n.resumeCancels, request.WorkerRef)
			}
		}
		n.mu.Unlock()
	}
	request.DeferInitialPrompt = true
	n.mu.Lock()
	revoke, err := n.bindLocalPublisher(&request)
	n.mu.Unlock()
	if err != nil {
		return nil, err
	}
	session, err := resumeSameIdentity(ctx, resumer, request, checkpoint.nativeID)
	if err != nil {
		revoke()
		return nil, ErrRuntimeSessionUnavailable
	}
	n.mu.Lock()
	hasPublisher := n.publishBroker != nil
	n.mu.Unlock()
	if hasPublisher {
		session = wrapLocalPublishedSession(session, revoke)
	}
	n.mu.Lock()
	n.workers[request.WorkerRef] = session
	n.activeAttempts[request.WorkerRef] = request.AttemptID
	n.checkpoints[request.AttemptID] = localCheckpoint{request: request, nativeID: session.ID()}
	n.mu.Unlock()
	go func() { _ = promptWithImages(context.Background(), session, request.Task, request.Images) }()
	return session, nil
}

func (n *LocalNode) Resume(ctx context.Context, request StartRequest, runtimeSessionID string) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	resumer, ok := n.runtime.(Resumer)
	if !ok {
		return nil, errors.New("node: runtime does not support session resume")
	}
	if request.Workspace == "" {
		return nil, errors.New("node: workspace is required for session resume")
	}
	if request.Profile.Name != "" && !nativeProfileDelivery(n.runtime, request.Profile) {
		if _, err := request.Profile.MaterializeInstructions(request.Workspace); err != nil {
			return nil, err
		}
	}
	// Serialize only operations for this Worker. A native Resume can block on
	// provider I/O indefinitely, so never hold the global Node mutex across it.
	unlock, err := n.lockWorker(ctx, request.WorkerRef)
	if err != nil {
		return nil, err
	}
	defer unlock()
	n.mu.Lock()
	_, exists := n.workers[request.WorkerRef]
	n.mu.Unlock()
	if exists {
		return nil, errors.New("node: worker already exists")
	}
	prepCtx, finish, err := n.beginLocalPrestart(ctx, request.AttemptID, request.WorkerRef)
	if err != nil {
		return nil, err
	}
	defer finish()
	if err := n.markLocalResuming(prepCtx, request.AttemptID, request.WorkerRef); err != nil {
		return nil, err
	}
	n.mu.Lock()
	revoke, err := n.bindLocalPublisher(&request)
	n.mu.Unlock()
	if err != nil {
		return nil, err
	}
	session, err := resumer.Resume(prepCtx, request, runtimeSessionID)
	if err != nil {
		revoke()
		return nil, err
	}
	if err := n.commitLocalHandoff(prepCtx, request.AttemptID, request.WorkerRef); err != nil {
		_ = session.Close()
		revoke()
		return nil, err
	}
	n.mu.Lock()
	hasPublisher := n.publishBroker != nil
	n.mu.Unlock()
	if hasPublisher {
		session = wrapLocalPublishedSession(session, revoke)
	}
	n.mu.Lock()
	n.workers[request.WorkerRef] = session
	n.activeAttempts[request.WorkerRef] = request.AttemptID
	key := request.AttemptID
	if key == "" {
		key = "worker:" + request.WorkerRef
	}
	// The native provider may retain prepCtx after Resume returns.
	// Do not auto-cancel an accepted Session from finish().
	if pending := n.prestarts[key]; pending != nil && pending.workerRef == request.WorkerRef && pending.stage == "handoff" {
		pending.keepContext = true
		n.resumeCancels[request.WorkerRef] = pending.cancel
	}
	n.mu.Unlock()
	return session, nil
}

func nativeProfileDelivery(runtime Runtime, profile ManagedProfile) bool {
	if profile.Delivery == "native" || profile.Runtime == "opencode" || profile.Runtime == "claude_code" {
		return true
	}
	switch selected := runtime.(type) {
	case ClaudeCodeRuntime:
		return true
	case *ClaudeCodeRuntime:
		return selected != nil
	case OpenCodeRuntime:
		return true
	case *OpenCodeRuntime:
		return selected != nil
	case RuntimeRouter:
		return profile.Runtime == "" && (selected.DefaultHarness == "opencode" || selected.DefaultHarness == "claude_code")
	case *RuntimeRouter:
		return selected != nil && profile.Runtime == "" && (selected.DefaultHarness == "opencode" || selected.DefaultHarness == "claude_code")
	default:
		return false
	}
}

func (n *LocalNode) Session(workerRef string) (Session, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	session, ok := n.workers[workerRef]
	return session, ok
}

func (n *LocalNode) Close() error {
	n.mu.Lock()
	workers := n.workers
	resumeCancels := n.resumeCancels
	n.workers = make(map[string]Session)
	broker := n.publishBroker
	n.publishBroker = nil
	n.activeAttempts = make(map[string]string)
	n.resumeCancels = make(map[string]context.CancelFunc)
	n.mu.Unlock()
	for _, cancel := range resumeCancels {
		cancel()
	}
	var first error
	if broker != nil {
		first = broker.Close()
	}
	for _, session := range workers {
		if err := session.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (n *LocalNode) Remove(workerRef string) error {
	n.mu.Lock()
	session, ok := n.workers[workerRef]
	var cancel context.CancelFunc
	if ok {
		delete(n.workers, workerRef)
		delete(n.activeAttempts, workerRef)
		cancel = n.resumeCancels[workerRef]
		delete(n.resumeCancels, workerRef)
	}
	n.mu.Unlock()
	if !ok {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	return session.Close()
}
