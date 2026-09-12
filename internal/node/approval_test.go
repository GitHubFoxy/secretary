package node

import (
	"context"
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
