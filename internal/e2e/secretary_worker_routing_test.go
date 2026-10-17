package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/mcp"
	"github.com/beruseruko/secretary/internal/node"
	secretaryruntime "github.com/beruseruko/secretary/internal/secretary"
	"github.com/beruseruko/secretary/internal/webapi"
)

// This test drives the public Web API and the real Secretary runtime + MCP
// boundary. A scripted Secretary harness reads the ACTUAL canonical prompt and
// chooses its tool from the Workers/Results in that prompt. It is not a test of
// a language model's semantic accuracy: that still needs a live harness check.
type t01SecretaryHarness struct {
	session *t01SecretarySession
}

type t01SecretarySession struct {
	onPrompt func(string) error
	results  chan node.Result
	activity chan node.Activity
}

func (r *t01SecretaryHarness) Start(_ context.Context, _ node.StartRequest) (node.Session, error) {
	r.session = &t01SecretarySession{
		results: make(chan node.Result, 8), activity: make(chan node.Activity, 8),
	}
	return r.session, nil
}

func (*t01SecretarySession) ID() string { return "t01-secretary-session" }

func (s *t01SecretarySession) Prompt(_ context.Context, prompt string) error {
	if s.onPrompt == nil {
		return errors.New("t01: scripted Secretary harness is not attached")
	}
	if err := s.onPrompt(prompt); err != nil {
		return err
	}
	// The real addressed-reply contract allows a native MCP-only completion
	// with zero assistant text chunks after a durable reply_to_user call.
	s.results <- node.Result{Status: "failed", CompletionEvidence: &node.TerminalCompletionEvidence{
		Contract: node.TerminalCompletionOpenCodeV2, StopReason: node.TerminalStopReasonEndTurn,
		RPCSucceeded: true, DrainCompleted: true,
	}}
	return nil
}
func (*t01SecretarySession) Steer(context.Context, string) (bool, error) { return false, nil }
func (*t01SecretarySession) Cancel(context.Context) error              { return nil }
func (s *t01SecretarySession) Activity() <-chan node.Activity          { return s.activity }
func (s *t01SecretarySession) Result() <-chan node.Result              { return s.results }
func (*t01SecretarySession) Close() error                              { return nil }

type t01WorkerHarness struct {
	store *core.Store
	mu    sync.Mutex
	calls map[string]int
}

func (r *t01WorkerHarness) complete(ctx context.Context, worker core.Worker, attempt core.Phase4Attempt) error {
	if _, err := r.store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		return err
	}
	r.mu.Lock()
	if r.calls == nil {
		r.calls = make(map[string]int)
	}
	r.calls[worker.ID]++
	count := r.calls[worker.ID]
	r.mu.Unlock()

	summary := "Task completed"
	switch {
	case strings.Contains(worker.Intent, "website") && count == 1:
		summary = "What should the website be about?"
	case strings.Contains(worker.Intent, "website"):
		summary = "Website requirements recorded."
	case strings.Contains(worker.Intent, "vacation") && count == 1:
		summary = "Where should we go on vacation?"
	case strings.Contains(worker.Intent, "vacation"):
		summary = "Vacation requirements recorded."
	}
	_, _, _, err := r.store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: summary,
	})
	return err
}
func (r *t01WorkerHarness) Dispatch(ctx context.Context, _ string, w core.Worker, _ core.Turn, a core.Phase4Attempt, _ core.DispatchResolution) error {
	return r.complete(ctx, w, a)
}
func (r *t01WorkerHarness) Resume(ctx context.Context, _ string, w core.Worker, _ core.Turn, a core.Phase4Attempt, _ string) error {
	return r.complete(ctx, w, a)
}
func (*t01WorkerHarness) Steer(context.Context, string, core.Worker, core.Phase4Attempt, string) error {
	return errors.New("unexpected steering: completed Worker must use Follow-up")
}
func (*t01WorkerHarness) Respond(context.Context, string, core.Worker, core.Phase4Attempt, string, string) error {
	return errors.New("unexpected response")
}
func (*t01WorkerHarness) Cancel(context.Context, string, core.Worker, core.Phase4Attempt) error {
	return errors.New("unexpected cancel")
}

// A native Secretary sees this prompt, not a preselected worker_ref supplied
// by the test. This fixture selects tools ONLY from the conversation-derived
// Worker identities; a failure to provide those records makes the test fail.
func t01DecodePrompt(prompt string) (core.SecretaryContext, string, string, string, error) {
	const contextHead = "Canonical server-owned Secretary context:\n"
	const routingHead = "\n\nWorker continuation contract:"
	const userHead = "\n\nUser turn:\n"
	const identityHead = "\n\nActive reply identity (server-issued; use these exact values):\n"
	contextStart := strings.Index(prompt, contextHead)
	if contextStart < 0 {
		return core.SecretaryContext{}, "", "", "", errors.New("missing canonical context")
	}
	after := prompt[contextStart+len(contextHead):]
	raw, _, ok := strings.Cut(after, routingHead)
	if !ok {
		return core.SecretaryContext{}, "", "", "", errors.New("missing Worker continuation contract")
	}
	var snapshot core.SecretaryContext
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return core.SecretaryContext{}, "", "", "", err
	}
	if err := snapshot.Validate(); err != nil {
		return core.SecretaryContext{}, "", "", "", err
	}
	userStart := strings.Index(prompt, userHead)
	identityStart := strings.Index(prompt, identityHead)
	if userStart < 0 || identityStart < userStart {
		return core.SecretaryContext{}, "", "", "", errors.New("missing user input or server-issued identity")
	}
	input := strings.TrimSpace(prompt[userStart+len(userHead) : identityStart])
	values := strings.Split(prompt[identityStart+len(identityHead):], "\n")
	if len(values) < 2 || !strings.HasPrefix(values[0], "secretary_turn_id=") || !strings.HasPrefix(values[1], "input_id=") {
		return core.SecretaryContext{}, "", "", "", errors.New("invalid server-issued origin")
	}
	return snapshot, input, strings.TrimPrefix(values[0], "secretary_turn_id="), strings.TrimPrefix(values[1], "input_id="), nil
}

type t01RoutingFixture struct {
	ctx       context.Context
	store     *core.Store
	api       *webapi.Server
	server    *httptest.Server
	client    *http.Client
	identity  core.SecretaryIdentity
	secretary *secretaryruntime.Runtime
	harness   *t01SecretaryHarness
	mcp       mcp.Secretary
	projectID string
	errs      chan error
}

func newT01RoutingFixture(t *testing.T) *t01RoutingFixture {
	t.Helper()
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "t01.db"))
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = store.Close() })

	api, err := webapi.New(ctx, store, "bootstrap")
	if err != nil { t.Fatal(err) }
	owner, conversation, err := store.EnsureOwner(ctx)
	if err != nil { t.Fatal(err) }
	identity, err := store.EnsureSecretaryIdentity(ctx, owner.ID, conversation.ID)
	if err != nil { t.Fatal(err) }
	capability, err := store.RotateSecretaryCapability(ctx, owner.ID)
	if err != nil { t.Fatal(err) }
	dataDir := t.TempDir()
	if _, err := store.SaveUserDocument(ctx, filepath.Join(dataDir, "user.md"), "one owner"); err != nil { t.Fatal(err) }
	policy := core.SecretaryPolicySnapshot{
		Version: "test", Harness: "opencode", Model: "fixture", Reasoning: "high",
		ProfileVersion: "test", ProfileName: "secretary", ProfileHash: "fixture",
		ProfileContent: "Always use canonical context to route a user answer; clarify if two Workers match.",
		ReplyContractVersion: core.SecretaryReplyContractAddressedV1,
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, policy); err != nil { t.Fatal(err) }
	project, err := store.CreateProject(ctx, core.ProjectSpec{
		ID: "test-workspace", Name: "Test workspace",
		Mappings: []core.ProjectPathMapping{{Node: "test-node", Path: t.TempDir()}},
	})
	if err != nil { t.Fatal(err) }
	if _, err := store.EnrollNode(ctx, "test-node"); err != nil { t.Fatal(err) }
	inventory := core.HarnessInventorySnapshot{
		Node: "test-node", ObservedAt: time.Now().UTC(),
		Instances: []core.HarnessInstance{{
			ID: "test-node/fx", Node: "test-node", Kind: core.HarnessFX,
			Version: "fixture", Status: core.HarnessReady,
			Authentication: core.HarnessAuthentication{Authenticated: true},
			Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{
				core.CapabilityShell, core.CapabilityEdit,
			}},
		}},
	}
	if err := store.UpdateNodeHeartbeat(ctx, "test-node", inventory, core.NodeHeartbeat{Capacity: 4}); err != nil { t.Fatal(err) }

	workerRuntime := &t01WorkerHarness{store: store}
	handler := mcp.Secretary{
		Workers: ctl.WorkerService{
			Store: store, PersonID: owner.ID, Capability: capability, Runtime: workerRuntime,
			WorkerProfileSource: func() (node.ManagedProfile, error) {
				return node.ManagedProfile{
					Version: "fixture", Name: "worker", Content: "Synthetic Worker",
					AllowTools: []string{"read"}, Hash: "fixture", Runtime: "fx",
				}, nil
			},
		},
		ReplyContractVersion: core.SecretaryReplyContractAddressedV1,
	}
	fake := &t01SecretaryHarness{}
	runtime := secretaryruntime.NewRuntime(node.NewLocal(fake), capability)
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachIdentity(identity)
	secretaryProfile := node.ManagedProfile{
		Version: "fixture", Name: "secretary", Content: policy.ProfileContent,
		Runtime: "opencode", Model: "fixture", Reasoning: "high",
		ReplyContractVersion: core.SecretaryReplyContractAddressedV1,
	}
	secretaryProfile.Hash = node.HashProfile(secretaryProfile.Content, nil, secretaryProfile.Model, secretaryProfile.Reasoning)
	runtime.AttachProfile(func() node.ManagedProfile { return secretaryProfile })
	runtime.AttachMCP("fake-mcp", dataDir)
	if err := runtime.Start(ctx); err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	api.AttachSecretary(runtime)
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	loginE2E(t, client, server.URL)
	fixture := &t01RoutingFixture{
		ctx: ctx, store: store, api: api, server: server, client: client,
		identity: identity, secretary: runtime, harness: fake,
		mcp: handler, projectID: project.ID, errs: make(chan error, 8),
	}
	fake.session.onPrompt = func(prompt string) error { return fixture.handleSecretaryPrompt(prompt) }
	return fixture
}

func (f *t01RoutingFixture) handleSecretaryPrompt(prompt string) error {
	snapshot, input, turnID, inputID, err := t01DecodePrompt(prompt)
	if err != nil { f.errs <- err; return err }
	if !strings.Contains(prompt, "ask the owner which Worker") {
		err := errors.New("Secretary prompt lost ambiguous-Worker safeguard")
		f.errs <- err
		return err
	}
	call := func(name string, args map[string]any) (any, error) {
		args["secretary_turn_id"], args["input_id"] = turnID, inputID
		data, err := json.Marshal(args)
		if err != nil { return nil, err }
		return f.mcp.Call(f.ctx, name, data)
	}
	if input == "Create a website" || input == "Plan a vacation" {
		// An optional acknowledgement precedes delegation; it is not a
		// second reply after the Worker's terminal Result.
		_, err = call("reply_to_user", map[string]any{"text": "Request received."})
		if err != nil { f.errs <- err; return err }
		_, err = call("spawn_worker", map[string]any{
			"intent": strings.ToLower(strings.TrimPrefix(input, "Create a ")),
			"preferences": map[string]string{
				"project_id": f.projectID, "node_id": "test-node",
				"harness_instance_id": "test-node/fx", "harness_kind": "fx",
			},
			"idempotency_key": inputID,
		})
	} else {
		pending := make([]core.Worker, 0)
		for _, w := range snapshot.OpenWorkers {
			if strings.HasSuffix(strings.TrimSpace(w.LastResultSummary), "?") {
				pending = append(pending, w)
			}
		}
		var matches []core.Worker
		for _, w := range pending {
			if strings.Contains(strings.ToLower(input), "website") && strings.Contains(w.Intent, "website") ||
				strings.Contains(strings.ToLower(input), "vacation") && strings.Contains(w.Intent, "vacation") {
				matches = append(matches, w)
			}
		}
		if len(matches) == 0 && len(pending) == 1 {
			matches = pending
		}
		if len(matches) != 1 {
			// Do not infer the recipient when multiple Workers can match.
			refs := make([]string, 0, len(pending))
			for _, worker := range pending {
				refs = append(refs, worker.WorkerRef)
			}
			_, err = call("reply_to_user", map[string]any{
				"text": "Which Worker should I continue: " + strings.Join(refs, ", ") + "?",
			})
		} else {
			_, err = call("reply_to_user", map[string]any{"text": "Request received."})
			if err != nil { f.errs <- err; return err }
			_, err = call("message_worker", map[string]any{
				"worker_ref": matches[0].WorkerRef, "text": input,
				"idempotency_key": inputID,
			})
		}
	}
	if err != nil { f.errs <- err }
	return err
}

func (f *t01RoutingFixture) post(t *testing.T, id, message string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"external_message_id": id, "body": message})
	if err != nil { t.Fatal(err) }
	req, err := http.NewRequest(http.MethodPost, f.server.URL+"/v1/messages", bytes.NewReader(body))
	if err != nil { t.Fatal(err) }
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "t01-"+id)
	resp, err := f.client.Do(req)
	if err != nil { t.Fatal(err) }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /v1/messages %q: status=%d", id, resp.StatusCode)
	}
}

func (f *t01RoutingFixture) wait(t *testing.T, wantTurns int) []core.Worker {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-f.errs:
			t.Fatal(err)
		default:
		}
		turns, err := f.store.SecretaryTurns(f.ctx, f.identity.ID)
		if err != nil { t.Fatal(err) }
		if len(turns) == wantTurns && turns[len(turns)-1].State == core.SecretaryTurnSucceeded {
			workers, err := f.mcp.Workers.ListWorkers(f.ctx)
			if err != nil { t.Fatal(err) }
			return workers
		}
		time.Sleep(5 * time.Millisecond)
	}
	turns, _ := f.store.SecretaryTurns(f.ctx, f.identity.ID)
	t.Fatalf("Secretary did not finish %d turns: %#v", wantTurns, turns)
	return nil
}

func TestT01WebToSecretaryHarnessRoutesWorkerFollowUpAndSeparateTask(t *testing.T) {
	f := newT01RoutingFixture(t)
	f.post(t, "create-site", "Create a website")
	workers := f.wait(t, 1)
	if len(workers) != 1 || workers[0].LastResultSummary != "What should the website be about?" {
		t.Fatalf("first Worker/Result=%#v", workers)
	}
	website := workers[0].ID
	websiteRef := workers[0].WorkerRef
	f.post(t, "answer-site", "A bookshop")
	workers = f.wait(t, 2)
	if len(workers) != 1 || workers[0].ID != website {
		t.Fatalf("Secretary created a new Worker for an answer: %#v", workers)
	}
	details, err := f.mcp.Workers.GetWorker(f.ctx, websiteRef)
	if err != nil || len(details.Turns) != 2 || len(details.Results) != 2 {
		t.Fatalf("Follow-up did not continue same Worker: %#v err=%v", details, err)
	}
	// Replay the same public input. It must not cause another Task/Attempt.
	f.post(t, "answer-site", "A bookshop")
	_ = f.wait(t, 2)
	details, err = f.mcp.Workers.GetWorker(f.ctx, websiteRef)
	if err != nil || len(details.Turns) != 2 {
		t.Fatalf("replay created another Worker Turn: %#v err=%v", details, err)
	}
	f.post(t, "new-topic", "Plan a vacation")
	workers = f.wait(t, 3)
	if len(workers) != 2 {
		t.Fatalf("new topic did not create an independent Worker: %#v", workers)
	}
	details, err = f.mcp.Workers.GetWorker(f.ctx, websiteRef)
	if err != nil || details.Worker.Archived || len(details.Results) != 2 {
		t.Fatalf("switching topics closed or changed website Worker: %#v err=%v", details, err)
	}
}

func TestT01WebToSecretaryHarnessClarifiesAmbiguousWorker(t *testing.T) {
	f := newT01RoutingFixture(t)
	f.post(t, "site", "Create a website")
	_ = f.wait(t, 1)
	f.post(t, "trip", "Plan a vacation")
	workers := f.wait(t, 2)
	if len(workers) != 2 { t.Fatalf("need two candidate Workers: %#v", workers) }

	before := make(map[string]int)
	for _, w := range workers {
		d, err := f.mcp.Workers.GetWorker(f.ctx, w.WorkerRef)
		if err != nil { t.Fatal(err) }
		before[w.ID] = len(d.Turns)
	}
	f.post(t, "ambiguous", "Yes")
	_ = f.wait(t, 3)
	entries, err := f.store.EntriesAfter(f.ctx, f.identity.ConversationID, 0)
	if err != nil { t.Fatal(err) }
	clarifications := 0
	for _, entry := range entries {
		if entry.Kind == core.EntrySecretary && strings.Contains(entry.Body, "Which Worker") {
			clarifications++
		}
	}
	if clarifications != 1 {
		t.Fatalf("ambiguous input did not produce exactly one clarification, got=%d", clarifications)
	}
	for _, w := range workers {
		d, err := f.mcp.Workers.GetWorker(f.ctx, w.WorkerRef)
		if err != nil { t.Fatal(err) }
		if len(d.Turns) != before[w.ID] {
			t.Fatalf("ambiguous input was sent to Worker %s", w.WorkerRef)
		}
	}
	f.post(t, "resolved", "For the website: sell books")
	workers = f.wait(t, 4)
	for _, w := range workers {
		d, err := f.mcp.Workers.GetWorker(f.ctx, w.WorkerRef)
		if err != nil { t.Fatal(err) }
		want := before[w.ID]
		if strings.Contains(w.Intent, "website") { want++ }
		if len(d.Turns) != want {
			t.Fatalf("clarified answer routed to wrong Worker: %s turns=%d want=%d", w.WorkerRef, len(d.Turns), want)
		}
	}
}


func TestT01WebToSecretaryHarnessClarifiesTwoWorkersWithSameTopic(t *testing.T) {
	f := newT01RoutingFixture(t)
	f.post(t, "website-first", "Create a website")
	_ = f.wait(t, 1)
	f.post(t, "website-second", "Create a website")
	workers := f.wait(t, 2)
	if len(workers) != 2 {
		t.Fatalf("two distinct website Tasks did not create two Workers: %#v", workers)
	}
	before := make(map[string]int)
	for _, w := range workers {
		details, err := f.mcp.Workers.GetWorker(f.ctx, w.WorkerRef)
		if err != nil { t.Fatal(err) }
		before[w.WorkerRef] = len(details.Turns)
	}
	// Mentioning the shared topic is insufficient to identify one Worker.
	f.post(t, "website-ambiguous", "For the website, use blue")
	_ = f.wait(t, 3)
	entries, err := f.store.EntriesAfter(f.ctx, f.identity.ConversationID, 0)
	if err != nil { t.Fatal(err) }
	found := false
	for _, entry := range entries {
		if entry.Kind != core.EntrySecretary || !strings.Contains(entry.Body, "Which Worker") {
			continue
		}
		found = strings.Contains(entry.Body, workers[0].WorkerRef) &&
			strings.Contains(entry.Body, workers[1].WorkerRef)
	}
	if !found {
		t.Fatal("ambiguous routing must ask for a Worker identity, not choose one")
	}
	for _, w := range workers {
		details, err := f.mcp.Workers.GetWorker(f.ctx, w.WorkerRef)
		if err != nil { t.Fatal(err) }
		if len(details.Turns) != before[w.WorkerRef] {
			t.Fatalf("ambiguous website answer was dispatched to Worker %s", w.WorkerRef)
		}
	}
}
