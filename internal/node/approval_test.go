package node

import (
	"context"
	"errors"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestRespondWorkerCommandUsesTypedResponseAndDeduplicatesRuntimeAction(t *testing.T) {
	store, err := OpenLocalStore(t.TempDir() + "/node.json")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	responder := &approvalResponderSession{id: "native", responses: make(chan string, 2)}
	execution := NewExecutionNode("macbook", &approvalRuntime{session: responder}, store)
	dispatch := dispatchFixture("dispatch-approval")
	if _, err := execution.HandleCommand(context.Background(), dispatch); err != nil {
		t.Fatal(err)
	}
	command := Command{Kind: CommandRespondWorker, RespondWorker: &RespondWorkerCommand{
		Metadata:  core.CommandMetadata{CommandID: "respond-1", Node: "macbook", WorkerRef: "worker-1", TurnID: "turn-1", AttemptID: "attempt-1"},
		RequestID: "request-1", Response: `{"approved":true}`,
	}}
	first, err := execution.HandleCommand(context.Background(), command)
	if err != nil || first.State != CommandAccepted {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	duplicateCommand := command
	duplicateCommand.RespondWorker = &RespondWorkerCommand{Metadata: command.RespondWorker.Metadata, RequestID: command.RespondWorker.RequestID, Response: command.RespondWorker.Response}
	duplicateCommand.RespondWorker.Metadata.CommandID = "respond-2"
	second, err := execution.HandleCommand(context.Background(), duplicateCommand)
	if err != nil || second.State != CommandAccepted || second.CommandID != "respond-2" {
		t.Fatalf("duplicate=%#v first=%#v err=%v", second, first, err)
	}
	if got := len(responder.responses); got != 1 {
		t.Fatalf("runtime response count=%d", got)
	}
	if response := <-responder.responses; response != `{"request_id":"request-1","response":"{"approved":true}"}` {
		t.Fatalf("typed response=%q", response)
	}
}

type approvalRuntime struct{ session Session }

func (r *approvalRuntime) Start(context.Context, StartRequest) (Session, error) {
	return r.session, nil
}

type approvalResponderSession struct {
	id        string
	responses chan string
}

func (s *approvalResponderSession) ID() string                                { return s.id }
func (*approvalResponderSession) Prompt(context.Context, string) error        { return nil }
func (*approvalResponderSession) Steer(context.Context, string) (bool, error) { return true, nil }
func (*approvalResponderSession) Cancel(context.Context) error                { return nil }
func (*approvalResponderSession) Activity() <-chan Activity                   { return nil }
func (*approvalResponderSession) Result() <-chan Result                       { return nil }
func (*approvalResponderSession) Close() error                                { return nil }
func (s *approvalResponderSession) Respond(_ context.Context, requestID, response string) error {
	s.responses <- `{"request_id":"` + requestID + `","response":"` + response + `"}`
	return nil
}

func TestFailedRespondWorkerCanRetryAfterNodeReconnect(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/node.json"
	store, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	firstSession := &retryResponderSession{id: "native-first", fail: true}
	firstRuntime := &retryResponderRuntime{session: firstSession}
	execution := NewExecutionNode("macbook", firstRuntime, store)
	dispatch := dispatchFixture("dispatch-retry")
	if outcome, err := execution.HandleCommand(ctx, dispatch); err != nil || outcome.State != CommandAccepted {
		t.Fatalf("dispatch=%#v err=%v", outcome, err)
	}
	command := Command{Kind: CommandRespondWorker, RespondWorker: &RespondWorkerCommand{Metadata: core.CommandMetadata{CommandID: "respond-retry", Node: "macbook", WorkerRef: "worker-1", TurnID: "turn-1", AttemptID: "attempt-1"}, RequestID: "retry-request", Response: "denied"}}
	if outcome, err := execution.HandleCommand(ctx, command); err != nil || outcome.State != CommandFailed {
		t.Fatalf("first response=%#v err=%v", outcome, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	secondSession := &retryResponderSession{id: "native-reconnected"}
	reconnected := NewExecutionNode("macbook", &retryResponderRuntime{session: secondSession}, reopened)
	outcome, err := reconnected.HandleCommand(ctx, command)
	if err != nil || outcome.State != CommandAccepted {
		t.Fatalf("retry response=%#v err=%v", outcome, err)
	}
	if firstSession.responds != 1 || secondSession.responds != 1 || firstSession.successes+secondSession.successes != 1 {
		t.Fatalf("responds first=%d second=%d successes=%d", firstSession.responds, secondSession.responds, firstSession.successes+secondSession.successes)
	}
	concurrent := command
	concurrent.RespondWorker = &RespondWorkerCommand{Metadata: command.RespondWorker.Metadata, RequestID: command.RespondWorker.RequestID, Response: command.RespondWorker.Response}
	concurrent.RespondWorker.Metadata.CommandID = "respond-retry-concurrent"
	if outcome, err := reconnected.HandleCommand(ctx, concurrent); err != nil || outcome.State != CommandAccepted || secondSession.responds != 1 {
		t.Fatalf("duplicate retry=%#v err=%v responds=%d", outcome, err, secondSession.responds)
	}
}

type retryResponderRuntime struct{ session *retryResponderSession }

func (r *retryResponderRuntime) Start(context.Context, StartRequest) (Session, error) {
	return r.session, nil
}
func (r *retryResponderRuntime) Resume(context.Context, StartRequest, string) (Session, error) {
	return r.session, nil
}

type retryResponderSession struct {
	id        string
	fail      bool
	responds  int
	successes int
}

func (s *retryResponderSession) ID() string                                { return s.id }
func (*retryResponderSession) Prompt(context.Context, string) error        { return nil }
func (*retryResponderSession) Steer(context.Context, string) (bool, error) { return true, nil }
func (*retryResponderSession) Cancel(context.Context) error                { return nil }
func (*retryResponderSession) Activity() <-chan Activity                   { return nil }
func (*retryResponderSession) Result() <-chan Result                       { return nil }
func (*retryResponderSession) Close() error                                { return nil }
func (s *retryResponderSession) Respond(context.Context, string, string) error {
	s.responds++
	if s.fail {
		s.fail = false
		return errors.New("native transport disconnected")
	}
	s.successes++
	return nil
}

func TestRespondWorkerRequestRebindsToNewSessionAfterNodeReconnect(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/node.json"
	store, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	firstRuntime := &reconnectRuntime{session: newReconnectSession("native-1")}
	execution := NewExecutionNode("macbook", firstRuntime, store)
	dispatch := dispatchFixture("dispatch-reconnect")
	dispatch.Dispatch.Envelope.HarnessInstance.Capabilities.Activity = []core.ActivityCapability{core.ActivitySessionStarted, core.ActivityAssistantTextDelta, core.ActivityPermissionRequest}
	if _, err := execution.HandleCommand(ctx, dispatch); err != nil {
		t.Fatal(err)
	}
	execution.publishRuntimeActivity(dispatch.Dispatch.Envelope, Activity{Kind: ActivityPermission, RequestID: "durable-request", Summary: "write"})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	resumed := newReconnectSession("native-1-reconnected")
	secondRuntime := &reconnectRuntime{session: resumed}
	reconnected := NewExecutionNode("macbook", secondRuntime, reopened)
	respond := Command{Kind: CommandRespondWorker, RespondWorker: &RespondWorkerCommand{Metadata: core.CommandMetadata{CommandID: "respond-reconnect", Node: "macbook", WorkerRef: "worker-1", TurnID: "turn-1", AttemptID: "attempt-1"}, RequestID: "durable-request", Response: "denied"}}
	outcome, err := reconnected.HandleCommand(ctx, respond)
	if err != nil || outcome.State != CommandAccepted {
		t.Fatalf("reconnected response=%#v err=%v", outcome, err)
	}
	if secondRuntime.resumes != 1 || resumed.rebinds != 1 || resumed.responds != 1 {
		t.Fatalf("resume=%d rebinds=%d responds=%d", secondRuntime.resumes, resumed.rebinds, resumed.responds)
	}
	duplicate := respond
	duplicate.RespondWorker = &RespondWorkerCommand{Metadata: respond.RespondWorker.Metadata, RequestID: respond.RespondWorker.RequestID, Response: respond.RespondWorker.Response}
	duplicate.RespondWorker.Metadata.CommandID = "respond-reconnect-duplicate"
	if outcome, err := reconnected.HandleCommand(ctx, duplicate); err != nil || outcome.State != CommandAccepted || resumed.responds != 1 {
		t.Fatalf("duplicate=%#v err=%v responds=%d", outcome, err, resumed.responds)
	}
}

type reconnectRuntime struct {
	session *reconnectSession
	resumes int
}

func (r *reconnectRuntime) Start(context.Context, StartRequest) (Session, error) {
	return r.session, nil
}
func (r *reconnectRuntime) Resume(context.Context, StartRequest, string) (Session, error) {
	r.resumes++
	return r.session, nil
}

type reconnectSession struct {
	id       string
	pending  map[string]bool
	rebinds  int
	responds int
}

func newReconnectSession(id string) *reconnectSession {
	return &reconnectSession{id: id, pending: map[string]bool{}}
}
func (s *reconnectSession) ID() string                                { return s.id }
func (*reconnectSession) Prompt(context.Context, string) error        { return nil }
func (*reconnectSession) Steer(context.Context, string) (bool, error) { return true, nil }
func (*reconnectSession) Cancel(context.Context) error                { return nil }
func (*reconnectSession) Activity() <-chan Activity                   { return nil }
func (*reconnectSession) Result() <-chan Result                       { return nil }
func (*reconnectSession) Close() error                                { return nil }
func (s *reconnectSession) RebindRequests(requests []string) {
	s.rebinds++
	for _, requestID := range requests {
		s.pending[requestID] = true
	}
}
func (s *reconnectSession) Respond(_ context.Context, requestID, _ string) error {
	if !s.pending[requestID] {
		return errors.New("request was not rebound")
	}
	s.responds++
	delete(s.pending, requestID)
	return nil
}
