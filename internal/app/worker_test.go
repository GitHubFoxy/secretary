package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type resumeRuntime struct {
	runtime
	resumed bool
}

type interruptRuntime struct {
	store   *core.Store
	session *interruptSession
}
type interruptSession struct {
	session
	store     *core.Store
	attemptID string
}

func (r *interruptRuntime) Start(_ context.Context, _ node.StartRequest) (node.Session, error) {
	r.session = &interruptSession{session: session{result: make(chan node.Result, 2), queued: make(chan string, 2)}, store: r.store}
	return r.session, nil
}

func (s *interruptSession) InterruptAndContinue(ctx context.Context, text string, prepare func() error) (bool, error) {
	if text == "" {
		return false, nil
	}
	if _, err := s.store.MarkAttemptInterrupted(ctx, s.attemptID); err != nil {
		return false, err
	}
	if err := prepare(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *resumeRuntime) Resume(_ context.Context, _ node.StartRequest, sessionID string) (node.Session, error) {
	r.resumed = true
	return &session{result: make(chan node.Result, 2), queued: make(chan string, 2)}, nil
}

func TestWorkerControllerPersistsFxInterruptFollowUp(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "fx-followup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, conversation.ID, "initial")
	if err != nil {
		t.Fatal(err)
	}
	runtime := &interruptRuntime{store: store}
	dispatcher := &Dispatcher{Store: store, Node: node.NewLocal(runtime)}
	_, firstAttempt, err := dispatcher.Dispatch(ctx, task, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	runtime.session.attemptID = firstAttempt.ID
	controller := WorkerController{Store: store, Node: dispatcher.Node}
	injected, err := controller.Steer(ctx, task.ID, "continue with this correction")
	if err != nil || !injected {
		t.Fatalf("injected=%v err=%v", injected, err)
	}
	details, err := store.TaskDetails(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(details.Attempts) != 2 || details.Attempts[0].State != core.AttemptInterrupted || details.Attempts[1].Number != 2 || details.Attempts[1].State != core.AttemptStarting {
		t.Fatalf("attempts=%#v", details.Attempts)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil || len(entries) != 1 || entries[0].Kind != core.EntryWorkerInput || entries[0].Body != "continue with this correction" {
		t.Fatalf("entries=%#v err=%v", entries, err)
	}
}

func TestWorkerControllerResumesInterruptedWorkerBeforeFollowUp(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "resume.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, conversation.ID, "initial")
	if err != nil {
		t.Fatal(err)
	}
	runtime := &resumeRuntime{}
	local := node.NewLocal(runtime)
	dispatcher := &Dispatcher{Store: store, Node: local}
	_, attempt, err := dispatcher.Dispatch(ctx, task, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkAttemptInterrupted(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if err := local.Remove(task.ID); err != nil {
		t.Fatal(err)
	}
	controller := WorkerController{Store: store, Node: local}
	followUp, err := controller.Queue(ctx, task.ID, "continue")
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.resumed || followUp.Number != 2 {
		t.Fatalf("resumed=%v followUp=%#v", runtime.resumed, followUp)
	}
	resumedSession, ok := local.Session(task.ID)
	if !ok {
		t.Fatal("resumed session was not registered")
	}
	if got := <-resumedSession.(*session).queued; got != "continue" {
		t.Fatalf("queued=%q", got)
	}
}

func TestWorkerControllerPersistsFollowUpBeforeQueueing(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "worker.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, conversation.ID, "initial")
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &Dispatcher{Store: store, Node: node.NewLocal(runtime{})}
	_, _, err = dispatcher.Dispatch(ctx, task, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	workerSession, _ := dispatcher.Node.Session(task.ID)
	fake := workerSession.(*session)
	fake.result <- node.Result{Status: "succeeded", Summary: "first result"}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		details, readErr := store.TaskDetails(ctx, task.ID)
		if readErr == nil && len(details.Results) == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	controller := WorkerController{Store: store, Node: dispatcher.Node}
	attempt, err := controller.Queue(ctx, task.ID, "follow-up input")
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Number != 2 || attempt.State != core.AttemptStarting {
		t.Fatalf("attempt=%#v", attempt)
	}
	if got := <-fake.queued; got != "follow-up input" {
		t.Fatalf("queued=%q", got)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[1].Kind != core.EntryWorkerInput {
		t.Fatalf("entries=%#v", entries)
	}
}
