package node

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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
	Kind          ActivityKind    `json:"kind"`
	Text          string          `json:"text,omitempty"`
	RequestID     string          `json:"request_id,omitempty"`
	Summary       string          `json:"summary,omitempty"`
	RequestSchema json.RawMessage `json:"request_schema,omitempty"`
	Tool          string          `json:"tool,omitempty"`
	Arguments     json.RawMessage `json:"arguments,omitempty"`
	Result        string          `json:"result,omitempty"`
	Error         string          `json:"error,omitempty"`
	Status        string          `json:"status,omitempty"`
}

type Result struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
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
	WorkerRef       string
	Task            string
	Workspace       string
	RawLogPath      string
	MCPServers      []MCPServer
	Profile         ManagedProfile
	HarnessInstance core.HarnessInstance
	Model           string
	Reasoning       string
	ApprovalPolicy  string
	// PendingRequests is the durable request metadata used for reconnect.
	PendingRequests []PendingRequest
	// PendingRequestKinds preserves metadata when an older caller can only
	// provide PendingRequestIDs.
	PendingRequestKinds map[string]ActivityKind
	// PendingRequestIDs is retained for older runtime adapters. New code must
	// use PendingRequests so permission and input requests cannot cross-bind.
	PendingRequestIDs []string
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
}

func NewLocal(runtime Runtime) *LocalNode {
	return &LocalNode{runtime: runtime, workers: make(map[string]Session)}
}

func (n *LocalNode) Dispatch(ctx context.Context, request StartRequest) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	if request.Workspace == "" {
		workspace, err := os.MkdirTemp("", "secretary-worker-")
		if err != nil {
			return nil, err
		}
		request.Workspace = workspace
	}
	if request.Profile.Name != "" && !nativeProfileDelivery(n.runtime, request.Profile) {
		if _, err := request.Profile.MaterializeInstructions(request.Workspace); err != nil {
			return nil, err
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, exists := n.workers[request.WorkerRef]; exists {
		return nil, errors.New("node: worker already exists")
	}
	session, err := n.runtime.Start(ctx, request)
	if err != nil {
		return nil, err
	}
	n.workers[request.WorkerRef] = session
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
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, exists := n.workers[request.WorkerRef]; exists {
		return nil, errors.New("node: worker already exists")
	}
	session, err := resumer.Resume(ctx, request, runtimeSessionID)
	if err != nil {
		return nil, err
	}
	n.workers[request.WorkerRef] = session
	return session, nil
}

func nativeProfileDelivery(runtime Runtime, profile ManagedProfile) bool {
	if profile.Delivery == "native" || profile.Runtime == "opencode" {
		return true
	}
	switch selected := runtime.(type) {
	case OpenCodeRuntime:
		return true
	case *OpenCodeRuntime:
		return selected != nil
	case RuntimeRouter:
		return profile.Runtime == "" && selected.DefaultHarness == "opencode"
	case *RuntimeRouter:
		return selected != nil && profile.Runtime == "" && selected.DefaultHarness == "opencode"
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
	n.workers = make(map[string]Session)
	n.mu.Unlock()
	var first error
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
	if ok {
		delete(n.workers, workerRef)
	}
	n.mu.Unlock()
	if !ok {
		return nil
	}
	return session.Close()
}
