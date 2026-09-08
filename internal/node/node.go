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

type StartRequest struct {
	WorkerRef string
	Task      string
	Workspace string
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

func (n *LocalNode) Session(workerRef string) (Session, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	session, ok := n.workers[workerRef]
	return session, ok
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
