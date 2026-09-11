package node

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// FXRuntime adapts fx's ACP implementation. fx does not implement
// _session/steering, so steering is expressed as cancel current turn, wait
// for the terminal response, then start a replacement prompt.
type FXRuntime struct {
	ACPRuntime
}

func (r FXRuntime) Start(ctx context.Context, request StartRequest) (Session, error) {
	base, err := r.ACPRuntime.Start(ctx, request)
	if err != nil {
		return nil, err
	}
	return wrapFXSession(base)
}

func (r FXRuntime) Resume(ctx context.Context, request StartRequest, runtimeSessionID string) (Session, error) {
	base, err := r.ACPRuntime.Resume(ctx, request, runtimeSessionID)
	if err != nil {
		return nil, err
	}
	return wrapFXSession(base)
}

func wrapFXSession(session Session) (Session, error) {
	base, ok := session.(*acpSession)
	if !ok {
		return nil, fmt.Errorf("fx: ACP runtime returned unsupported session %T", session)
	}
	return &fxSession{acpSession: base}, nil
}

type fxSession struct {
	*acpSession
	steerMu sync.Mutex
}

// InterruptAndContinueSteerer lets the server create a durable follow-up
// Attempt after fx has completed cancellation and before the replacement
// prompt is sent.
type InterruptAndContinueSteerer interface {
	InterruptAndContinue(context.Context, string, func() error) (bool, error)
}

func (s *fxSession) Steer(ctx context.Context, text string) (bool, error) {
	return s.InterruptAndContinue(ctx, text, nil)
}

func (s *fxSession) InterruptAndContinue(ctx context.Context, text string, prepare func() error) (bool, error) {
	if text == "" {
		return false, errors.New("fx: steering text is empty")
	}
	s.steerMu.Lock()
	defer s.steerMu.Unlock()
	if s.busyNow() {
		if err := s.Cancel(ctx); err != nil {
			return false, err
		}
		if err := s.waitIdle(ctx); err != nil {
			return false, err
		}
	}
	if prepare != nil {
		if err := prepare(); err != nil {
			return false, err
		}
	}
	if !s.beginTurn() {
		return false, errors.New("fx: session became busy while steering")
	}
	go func() { _ = s.promptTurn(context.Background(), text) }()
	return true, nil
}

func (s *acpSession) busyNow() bool {
	s.turnMu.Lock()
	busy := s.busy
	s.turnMu.Unlock()
	return busy
}

func (s *acpSession) waitIdle(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !s.busyNow() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

var _ Runtime = FXRuntime{}
var _ Resumer = FXRuntime{}
var _ InterruptAndContinueSteerer = (*fxSession)(nil)
