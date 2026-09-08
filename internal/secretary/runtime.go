package secretary

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/beruseruko/secretary/internal/node"
)

const workerRef = "secretary"

var ErrNotStarted = errors.New("secretary: runtime not started")

// Runtime owns the single long-lived Secretary session. It never receives a
// raw store handle. Task lifecycle authority is exposed only through ctl.
type Runtime struct {
	node       *node.LocalNode
	capability string

	mu      sync.Mutex
	session node.Session
	busy    bool
	queued  []string
	errors  chan error
}

func NewRuntime(local *node.LocalNode, capability string) *Runtime {
	return &Runtime{node: local, capability: capability, errors: make(chan error, 8)}
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
	prompt := fmt.Sprintf("You are the persistent personal Secretary. Use only the capability-scoped secretaryctl for Task lifecycle operations. Run secretaryctl with SECRETARY_CAPABILITY=%s. Never give this capability to a Worker or Channel adapter.", r.capability)
	session, err := r.node.Dispatch(ctx, node.StartRequest{WorkerRef: workerRef, Task: prompt})
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.session = session
	r.busy = true
	r.mu.Unlock()
	go r.consumeResults(session)
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
	for range session.Result() {
		r.mu.Lock()
		r.busy = false
		r.mu.Unlock()
		r.startNext(context.Background())
	}
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
	go func() {
		if err := session.Prompt(ctx, text); err != nil {
			r.mu.Lock()
			r.busy = false
			r.mu.Unlock()
			select {
			case r.errors <- err:
			default:
			}
		}
	}()
}

func (r *Runtime) Stop(ctx context.Context) error {
	r.mu.Lock()
	session := r.session
	r.session = nil
	r.busy = false
	r.queued = nil
	r.mu.Unlock()
	if session == nil {
		return nil
	}
	if err := session.Cancel(ctx); err != nil {
		return err
	}
	return r.node.Remove(workerRef)
}
