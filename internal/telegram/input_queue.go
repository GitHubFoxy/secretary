package telegram

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

func inputLane(u Update) string {
	if u.Message == nil {
		return "none"
	}
	thread := u.Message.ThreadID
	if thread == 1 {
		thread = 0
	}
	return fmt.Sprintf("%d:%d", u.Message.ChatID, thread)
}

func (a *Adapter) enqueueInputs(updates []Update, advance bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, u := range updates {
		if u.ID <= 0 {
			continue
		}
		exists := false
		if _, ok := a.state.Processed[fmt.Sprint(u.ID)]; ok {
			exists = true
		}
		for _, p := range a.state.PendingInputs {
			if p.ID == u.ID {
				exists = true
				break
			}
		}
		if !exists && u.Message != nil {
			a.state.PendingInputs = append(a.state.PendingInputs, u)
		}
		if advance && u.ID > a.state.Offset {
			a.state.Offset = u.ID
		}
	}
	sort.SliceStable(a.state.PendingInputs, func(i, j int) bool { return a.state.PendingInputs[i].ID < a.state.PendingInputs[j].ID })
	return a.saveLocked()
}

func (a *Adapter) laneLock(key string) *sync.Mutex {
	a.inputMu.Lock()
	defer a.inputMu.Unlock()
	if a.inputLanes == nil {
		a.inputLanes = map[string]*sync.Mutex{}
	}
	if a.inputLanes[key] == nil {
		a.inputLanes[key] = &sync.Mutex{}
	}
	return a.inputLanes[key]
}

func (a *Adapter) drainLane(ctx context.Context, key string) error {
	lock := a.laneLock(key)
	lock.Lock()
	defer lock.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		a.mu.Lock()
		var next Update
		for _, u := range a.state.PendingInputs {
			if inputLane(u) == key {
				next = u
				break
			}
		}
		a.mu.Unlock()
		if next.ID == 0 {
			return nil
		}
		// Keep the PendingInput as a durable dependency. The image worker
		// will resume this lane only after all earlier photos in its chat
		// have been delivered (or permanently rejected).
		a.mu.Lock()
		blocked := a.hasEarlierPendingImageLocked(next)
		a.mu.Unlock()
		if blocked {
			return nil
		}
		err := a.processUpdate(ctx, next)
		if err != nil && !errors.Is(err, ErrUnauthorized) && !errors.Is(err, ErrPairingUsed) {
			return err
		}
		a.mu.Lock()
		for i, u := range a.state.PendingInputs {
			if u.ID == next.ID {
				a.state.PendingInputs = append(a.state.PendingInputs[:i], a.state.PendingInputs[i+1:]...)
				break
			}
		}
		err = a.saveLocked()
		a.mu.Unlock()
		if err != nil {
			return err
		}
		if next.Message != nil && next.Message.Image != nil {
			a.startImageWorkers(ctx)
		}
	}
}

func (a *Adapter) inputKeys() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	seen := map[string]bool{}
	var keys []string
	for _, u := range a.state.PendingInputs {
		k := inputLane(u)
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}

func (a *Adapter) HandleUpdate(ctx context.Context, u Update) error {
	if err := a.enqueueInputs([]Update{u}, false); err != nil {
		return err
	}
	return a.drainLane(ctx, inputLane(u))
}

func (a *Adapter) pollInputs(ctx context.Context) error {
	a.mu.Lock()
	offset := a.state.Offset + 1
	a.mu.Unlock()
	updates, err := a.transport.GetUpdates(ctx, offset, a.config.PollInterval)
	if err != nil {
		return err
	}
	return a.enqueueInputs(updates, true)
}

func (a *Adapter) drainInputs(ctx context.Context) error {
	var wg sync.WaitGroup
	keys := a.inputKeys()
	errs := make(chan error, len(keys))
	for _, key := range keys {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			if err := a.drainLane(ctx, k); err != nil {
				errs <- err
			}
		}(key)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	return a.Flush(ctx)
}

func (a *Adapter) startInputWorkers(ctx context.Context) {
	for _, key := range a.inputKeys() {
		a.inputMu.Lock()
		if a.inputWorkers == nil {
			a.inputWorkers = map[string]bool{}
		}
		if a.inputWorkers[key] {
			a.inputMu.Unlock()
			continue
		}
		a.inputWorkers[key] = true
		a.inputMu.Unlock()
		a.inputWG.Add(1)
		go func(k string) {
			defer a.inputWG.Done()
			_ = a.drainLane(ctx, k)
			a.inputMu.Lock()
			delete(a.inputWorkers, k)
			a.inputMu.Unlock()
		}(key)
	}
}

func (a *Adapter) bindWorkerRequest(updateID int64, currentRequestID string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.state.PendingInputs {
		pending := &a.state.PendingInputs[i]
		if pending.ID != updateID {
			continue
		}
		if pending.WorkerRequestID == nil {
			requestID := currentRequestID
			pending.WorkerRequestID = &requestID
		}
		return *pending.WorkerRequestID, a.saveLocked()
	}
	return "", errors.New("telegram: dispatched Worker input is not durably queued")
}

func (a *Adapter) hasEarlierPendingImageLocked(u Update) bool {
	if u.Message == nil {
		return false
	}
	for _, image := range a.state.PendingImages {
		if image.Message != nil && image.Message.ChatID == u.Message.ChatID && image.ID < u.ID {
			return true
		}
	}
	// An earlier photo may still be in another input lane and not yet have
	// moved into PendingImages. Do not let later topic text overtake it.
	for _, pending := range a.state.PendingInputs {
		if pending.Message != nil && pending.Message.Image != nil &&
			pending.Message.ChatID == u.Message.ChatID && pending.ID < u.ID {
			return true
		}
	}
	return false
}

func (a *Adapter) inputLanesForChat(chatID int64) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	seen := make(map[string]bool)
	var lanes []string
	for _, pending := range a.state.PendingInputs {
		if pending.Message != nil && pending.Message.ChatID == chatID {
			key := inputLane(pending)
			if !seen[key] {
				seen[key] = true
				lanes = append(lanes, key)
			}
		}
	}
	return lanes
}
