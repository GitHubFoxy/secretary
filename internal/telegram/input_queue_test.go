package telegram

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type requestReplayServer struct {
	accepted  map[string]WorkerMessage
	calls     []WorkerMessage
	failFirst bool
}

func (s *requestReplayServer) SendMessage(context.Context, InboundMessage) error { return nil }
func (s *requestReplayServer) SendWorkerMessage(_ context.Context, m WorkerMessage) error {
	s.calls = append(s.calls, m)
	if s.accepted == nil {
		s.accepted = map[string]WorkerMessage{}
	}
	if prior, ok := s.accepted[m.ExternalMessageID]; ok && prior != m {
		return errors.New("409 idempotency fingerprint differs")
	}
	s.accepted[m.ExternalMessageID] = m
	if s.failFirst {
		s.failFirst = false
		return errors.New("connection lost after acceptance")
	}
	return nil
}
func (s *requestReplayServer) ResumeWorker(context.Context, string) error { return nil }
func workerText(id int64, text string) Update {
	return Update{ID: id, Message: &Message{ChatID: 100, FromID: 100, ThreadID: 7, Text: text}}
}
func TestWorkerTextReplayKeepsRequestAfterMappingClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	transport := &fakeTransport{}
	server := &requestReplayServer{failFirst: true}
	a := newTestAdapter(t, transport, server, path)
	a.state.Topics["w"] = TopicMapping{WorkerRef: "w", ChatID: 100, ThreadID: 7, PendingRequestID: "request-old"}
	if err := a.HandleUpdate(context.Background(), workerText(1, "answer")); err == nil {
		t.Fatal("simulated acceptance ambiguity not surfaced")
	}
	if len(a.state.PendingInputs) != 1 || a.state.PendingInputs[0].WorkerRequestID == nil || *a.state.PendingInputs[0].WorkerRequestID != "request-old" {
		t.Fatal("dispatch request ID not persisted")
	}
	// Reproduce crash window: the server accepted this input and the adapter
	// persisted clearing the mapping, but did not persist Processed/removal yet.
	a.mu.Lock()
	mapping := a.state.Topics["w"]
	mapping.PendingRequestID = ""
	a.state.Topics["w"] = mapping
	if err := a.saveLocked(); err != nil {
		t.Fatal(err)
	}
	a.mu.Unlock()
	a = newTestAdapter(t, transport, server, path)
	// A new question must not change the replay body or get cleared by replay.
	a.mu.Lock()
	mapping = a.state.Topics["w"]
	mapping.PendingRequestID = "request-new"
	a.state.Topics["w"] = mapping
	if err := a.saveLocked(); err != nil {
		t.Fatal(err)
	}
	a.mu.Unlock()
	if err := a.drainInputs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(server.accepted) != 1 || len(server.calls) != 2 || server.calls[1].RequestID != "request-old" {
		t.Fatalf("replay=%+v", server.calls)
	}
	if a.state.Topics["w"].PendingRequestID != "request-new" {
		t.Fatal("replay cleared newer pending request")
	}
	if err := a.HandleUpdate(context.Background(), workerText(2, "new answer")); err != nil {
		t.Fatal(err)
	}
	if server.calls[2].RequestID != "request-new" {
		t.Fatalf("new request misrouted=%+v", server.calls[2])
	}
}
func TestWorkerQueuedTextBindsRequestAtDispatch(t *testing.T) {
	server := &requestReplayServer{}
	a := newTestAdapter(t, &fakeTransport{}, server, filepath.Join(t.TempDir(), "state"))
	a.state.Topics["w"] = TopicMapping{WorkerRef: "w", ChatID: 100, ThreadID: 7, PendingRequestID: "request-one"}
	if err := a.enqueueInputs([]Update{workerText(1, "answer"), workerText(2, "steering")}, true); err != nil {
		t.Fatal(err)
	}
	if err := a.drainInputs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(server.calls) != 2 || server.calls[0].RequestID != "request-one" || server.calls[1].RequestID != "" {
		t.Fatalf("premature request snapshot=%+v", server.calls)
	}
}
