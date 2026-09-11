package node

import (
	"context"
	"errors"
	"os"
	"sync"
)

type ActivityKind string

const (
	ActivityText   ActivityKind = "text"
	ActivityTool   ActivityKind = "tool"
	ActivityStatus ActivityKind = "status"
)

type Activity struct {
	Kind ActivityKind `json:"kind"`
	Text string       `json:"text"`
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
	WorkerRef  string
	Task       string
	Workspace  string
	RawLogPath string
	MCPServers []MCPServer
	Profile    ManagedProfile
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
