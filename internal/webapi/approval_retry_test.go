package webapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/node"
)

func TestApprovalRetryAPIRestartConnectionLossUsesSavedIntentAndPreservesResults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "approval-retry-api.db")
	store, err := core.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = store.Close()
		}
	})

	api1, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	personID := api1.OwnerID()
	conversation, err := store.ConversationForPerson(ctx, personID)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, personID)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, core.ProjectSpec{ID: "approval-retry-project", Name: "Approval retry", Mappings: []core.ProjectPathMapping{{Node: "retry-node", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}

	manager1, err := node.NewServerManager(ctx, store, "pair-token", "admin-token")
	if err != nil {
		t.Fatal(err)
	}
	manager1.SetCommandOutcomeSink(node.NewStoreCommandOutcomeSink(store))
	nodeHTTP1 := approvalRetryNodeServer(manager1)
	identity, err := node.EnrollNode(ctx, nodeHTTP1.Client(), nodeHTTP1.URL, "pair-token", "retry-node")
	if err != nil {
		t.Fatal(err)
	}
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "approval-retry-worker", Intent: "wait for permission", ProjectID: project.ID,
		NodeID: "retry-node", HarnessInstanceID: "retry-node/fx",
	}, core.TurnSpec{Input: "wait for permission"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	const requestID = "approval-retry-api-request"
	const savedResponse = "denied"
	if _, err := store.RecordNodeActivityReplay(ctx, core.Activity{Metadata: core.ActivityMetadata{
		EventID: "approval-retry-api-event", Node: "retry-node", HarnessInstanceID: "retry-node/fx", WorkerRef: worker.WorkerRef,
		TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC(),
	}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: requestID, Summary: "synthetic permission"}}); err != nil {
		t.Fatal(err)
	}
	_, foreignConversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foreignWorker, foreignTurn, foreignAttempt, err := store.CreateWorker(ctx, foreignConversation.ID, core.WorkerSpec{
		WorkerRef: "foreign-approval-worker", Intent: "foreign approval", ProjectID: project.ID,
		NodeID: "retry-node", HarnessInstanceID: "retry-node/fx",
	}, core.TurnSpec{Input: "foreign approval"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, foreignAttempt.ID); err != nil {
		t.Fatal(err)
	}
	const foreignRequestID = "foreign-approval-request"
	if _, err := store.RecordNodeActivityReplay(ctx, core.Activity{Metadata: core.ActivityMetadata{
		EventID: "foreign-approval-event", Node: "retry-node", HarnessInstanceID: "retry-node/fx", WorkerRef: foreignWorker.WorkerRef,
		TurnID: foreignTurn.ID, AttemptID: foreignAttempt.ID, Sequence: 1, ObservedAt: time.Now().UTC(),
	}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: foreignRequestID, Summary: "foreign synthetic permission"}}); err != nil {
		t.Fatal(err)
	}
	foreignApproval, err := store.Approval(ctx, foreignRequestID)
	if err != nil {
		t.Fatal(err)
	}
	service1 := ctl.WorkerService{Store: store, PersonID: personID, Capability: capability, Runtime: ctl.NodeRuntime{Manager: manager1}}
	api1.AttachNodeService(manager1)
	api1.AttachWorkerResponder(service1)
	apiHTTP1 := httptest.NewServer(api1.Handler())
	ownerClient1 := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, ownerClient1, apiHTTP1.URL)

	nodeConnection1, err := dialApprovalRetryNode(ctx, identity, nodeHTTP1.URL)
	if err != nil {
		t.Fatal(err)
	}
	firstAttemptCtx, cancelFirstAttempt := context.WithTimeout(ctx, 180*time.Millisecond)
	firstRequest, err := http.NewRequestWithContext(firstAttemptCtx, http.MethodPost, apiHTTP1.URL+"/v1/approvals/"+requestID+"/deny", nil)
	if err != nil {
		t.Fatal(err)
	}
	firstRequest.Header.Set("Idempotency-Key", "initial-approval-answer")
	firstDone := make(chan error, 1)
	go func() {
		response, requestErr := ownerClient1.Do(firstRequest)
		if response != nil {
			response.Body.Close()
		}
		firstDone <- requestErr
	}()
	firstCommand, err := nodeConnection1.ReceiveCommand(ctx)
	if err != nil {
		cancelFirstAttempt()
		t.Fatal(err)
	}
	if firstCommand.Metadata().TurnID != turn.ID || firstCommand.Metadata().AttemptID != attempt.ID || firstCommand.RespondWorker == nil || firstCommand.RespondWorker.Response != savedResponse {
		cancelFirstAttempt()
		t.Fatal("initial Node handoff did not contain the original decision and exact lifecycle identity")
	}
	originalCommandID := firstCommand.Metadata().CommandID
	_ = nodeConnection1.Close()
	cancelFirstAttempt()
	<-firstDone
	if !waitForWebAPI(t, ctx, func() bool {
		approval, approvalErr := store.Approval(ctx, requestID)
		command, found, commandErr := store.FindWorkerCommand(ctx, "respond", "request:"+requestID, worker.ID, attempt.ID)
		return approvalErr == nil && commandErr == nil && found && approval.State == core.ApprovalResolving && command.State == core.WorkerCommandUncertain
	}) {
		t.Fatal("lost authenticated ACK did not leave a retryable saved Approval intent")
	}
	uncertainCommand, found, err := store.FindWorkerCommand(ctx, "respond", "request:"+requestID, worker.ID, attempt.ID)
	if err != nil || !found || uncertainCommand.State != core.WorkerCommandUncertain || !uncertainCommand.LeaseUntil.IsZero() {
		t.Fatalf("connection loss command state=%q lease_released=%v found=%v err=%v", uncertainCommand.State, uncertainCommand.LeaseUntil.IsZero(), found, err)
	}
	uncertainError := uncertainCommand.LastError

	_, canonical, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "canonical Result before retry receipt",
	})
	if err != nil || canonical == nil {
		t.Fatalf("record canonical Result before retry receipt: err=%v", err)
	}
	apiHTTP1.Close()
	nodeHTTP1.Close()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true

	store, err = core.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	closed = false
	if err := store.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecoverApprovalResolutionCommands(ctx); err != nil {
		t.Fatal(err)
	}
	approval, err := store.Approval(ctx, requestID)
	if err != nil || approval.State != core.ApprovalResolving || approval.ResolutionResponse != savedResponse || approval.ResolutionCommandID != originalCommandID {
		t.Fatalf("server restart changed the saved resolution intent: state=%q err=%v", approval.State, err)
	}
	independentTurn, _, err := store.CreateTurn(ctx, worker.ID, core.TurnSpec{Input: "a separate next direction"})
	if err != nil {
		t.Fatalf("create independent Turn after canonical Result: %v", err)
	}

	manager2, err := node.NewServerManager(ctx, store, "pair-token", "admin-token")
	if err != nil {
		t.Fatal(err)
	}
	manager2.SetCommandOutcomeSink(node.NewStoreCommandOutcomeSink(store))
	nodeHTTP2 := approvalRetryNodeServer(manager2)
	defer nodeHTTP2.Close()
	identity2, err := dialApprovalRetryNode(ctx, identity, nodeHTTP2.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer identity2.Close()
	api2, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	service2 := ctl.WorkerService{Store: store, PersonID: personID, Capability: capability, Runtime: ctl.NodeRuntime{Manager: manager2}}
	api2.AttachNodeService(manager2)
	api2.AttachWorkerResponder(service2)
	apiHTTP2 := httptest.NewServer(api2.Handler())
	defer apiHTTP2.Close()
	ownerClient2 := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, ownerClient2, apiHTTP2.URL)
	recoveredCommand, found, err := store.FindWorkerCommand(ctx, "respond", "request:"+requestID, worker.ID, attempt.ID)
	if err != nil || !found || recoveredCommand.ID != originalCommandID || recoveredCommand.State != core.WorkerCommandUncertain || !recoveredCommand.LeaseUntil.IsZero() || recoveredCommand.LastError != uncertainError {
		t.Fatal("server startup or Node reconnect automatically retried the saved Approval command")
	}

	unauthenticatedRetry, err := http.NewRequest(http.MethodPost, apiHTTP2.URL+"/v1/approvals/"+requestID+"/retry", nil)
	if err != nil {
		t.Fatal(err)
	}
	unauthenticatedResponse, err := http.DefaultClient.Do(unauthenticatedRetry)
	if err != nil {
		t.Fatal(err)
	}
	unauthenticatedResponse.Body.Close()
	if unauthenticatedResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated retry status=%d", unauthenticatedResponse.StatusCode)
	}
	foreignRetry, err := http.NewRequest(http.MethodPost, apiHTTP2.URL+"/v1/approvals/"+foreignApproval.ID+"/retry", nil)
	if err != nil {
		t.Fatal(err)
	}
	foreignRetry.Header.Set("Idempotency-Key", "cross-owner-retry")
	foreignResponse, err := ownerClient2.Do(foreignRetry)
	if err != nil {
		t.Fatal(err)
	}
	foreignResponse.Body.Close()
	if foreignResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-owner public Approval retry status=%d", foreignResponse.StatusCode)
	}

	approvalsResponse, err := ownerClient2.Get(apiHTTP2.URL + "/v1/approvals")
	if err != nil {
		t.Fatal(err)
	}
	approvalsBody, err := io.ReadAll(approvalsResponse.Body)
	approvalsResponse.Body.Close()
	if err != nil || approvalsResponse.StatusCode != http.StatusOK {
		t.Fatalf("read public Approval recovery state: status=%d err=%v", approvalsResponse.StatusCode, err)
	}
	var publicApprovals []publicApprovalDTO
	if err := json.Unmarshal(approvalsBody, &publicApprovals); err != nil {
		t.Fatal(err)
	}
	containsResponseField := strings.Contains(string(approvalsBody), `"response":`) || strings.Contains(string(approvalsBody), `"resolution_response":`)
	containsCommand := strings.Contains(string(approvalsBody), originalCommandID)
	if len(publicApprovals) != 1 || publicApprovals[0].ID != approval.ID || publicApprovals[0].State != core.ApprovalResolving || publicApprovals[0].ResolutionState != core.ApprovalDenied || containsResponseField || containsCommand {
		var state, decision core.ApprovalState
		if len(publicApprovals) > 0 {
			state, decision = publicApprovals[0].State, publicApprovals[0].ResolutionState
		}
		t.Fatalf("public Approval mismatch: count=%d state=%q decision=%q response_field=%v command_leak=%v", len(publicApprovals), state, decision, containsResponseField, containsCommand)
	}

	replacementRequest, err := http.NewRequest(http.MethodPost, apiHTTP2.URL+"/v1/approvals/"+approval.ID+"/retry", strings.NewReader(`{"response":"client replacement payload"}`))
	if err != nil {
		t.Fatal(err)
	}
	replacementRequest.Header.Set("Content-Type", "application/json")
	replacementRequest.Header.Set("Idempotency-Key", "attempt-to-replace-saved-response")
	replacementResponse, err := ownerClient2.Do(replacementRequest)
	if err != nil {
		t.Fatal(err)
	}
	replacementResponse.Body.Close()
	if replacementResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("client-supplied response was not rejected: status=%d", replacementResponse.StatusCode)
	}
	retryRequest, err := http.NewRequest(http.MethodPost, apiHTTP2.URL+"/v1/approvals/"+approval.ID+"/retry", nil)
	if err != nil {
		t.Fatal(err)
	}
	retryRequest.Header.Set("Idempotency-Key", "owner-retry-saved-approval")
	retryDone := make(chan struct {
		response *http.Response
		err      error
	}, 1)
	go func() {
		response, requestErr := ownerClient2.Do(retryRequest)
		retryDone <- struct {
			response *http.Response
			err      error
		}{response, requestErr}
	}()
	type receivedCommand struct {
		command node.Command
		err     error
	}
	commandResult := make(chan receivedCommand, 1)
	go func() {
		command, receiveErr := identity2.ReceiveCommand(ctx)
		commandResult <- receivedCommand{command: command, err: receiveErr}
	}()
	var retriedCommand node.Command
	select {
	case received := <-commandResult:
		if received.err != nil {
			t.Fatal(received.err)
		}
		retriedCommand = received.command
	case result := <-retryDone:
		if result.response != nil {
			body, _ := io.ReadAll(result.response.Body)
			result.response.Body.Close()
			message := string(body)
			message = strings.ReplaceAll(message, savedResponse, "[saved-response]")
			message = strings.ReplaceAll(message, originalCommandID, "[command-id]")
			t.Fatalf("retry API returned before Node handoff: status=%d error=%q", result.response.StatusCode, message)
		}
		t.Fatalf("retry API failed before Node handoff: err=%v", result.err)
	case <-ctx.Done():
		t.Fatal("owner retry did not produce a Node handoff or API response")
	}
	metadata := retriedCommand.Metadata()
	if metadata.CommandID != originalCommandID || metadata.Node != "retry-node" || metadata.TurnID != turn.ID || metadata.AttemptID != attempt.ID || retriedCommand.RespondWorker == nil || retriedCommand.RespondWorker.Response != savedResponse {
		t.Fatal("owner retry did not reuse the exact saved command and decision")
	}
	if err := identity2.SendCommandOutcome(ctx, node.CommandOutcome{
		CommandID: originalCommandID, Kind: node.CommandRespondWorker, State: node.CommandAccepted,
		TurnID: turn.ID, AttemptID: attempt.ID,
	}); err != nil {
		t.Fatal(err)
	}
	result := <-retryDone
	if result.err != nil {
		t.Fatal(result.err)
	}
	responseBody, err := io.ReadAll(result.response.Body)
	result.response.Body.Close()
	if err != nil || result.response.StatusCode != http.StatusOK {
		t.Fatalf("explicit retry response status=%d err=%v", result.response.StatusCode, err)
	}
	if strings.Contains(string(responseBody), `"response":`) || strings.Contains(string(responseBody), `"resolution_response":`) || strings.Contains(string(responseBody), originalCommandID) {
		t.Fatal("retry API exposed a response field or private command identity")
	}

	resolved, err := store.Approval(ctx, requestID)
	if err != nil || resolved.State != core.ApprovalDenied || resolved.Response != savedResponse {
		t.Fatalf("authenticated receipt did not finalize the original choice: state=%q err=%v", resolved.State, err)
	}
	oldTurn, err := store.Turn(ctx, turn.ID)
	if err != nil || oldTurn.State != core.TurnSucceeded || oldTurn.ResultID != canonical.ID {
		t.Fatalf("retry receipt replaced or revived canonical Result: state=%q result=%q err=%v", oldTurn.State, oldTurn.ResultID, err)
	}
	currentWorker, err := store.Worker(ctx, worker.ID)
	if err != nil || currentWorker.Status != core.WorkerQueued || currentWorker.CurrentTurnID != independentTurn.ID {
		t.Fatalf("retry receipt changed the independent Turn: status=%q current_turn=%q err=%v", currentWorker.Status, currentWorker.CurrentTurnID, err)
	}
	newTurn, err := store.Turn(ctx, independentTurn.ID)
	if err != nil || newTurn.State != core.TurnStarting {
		t.Fatalf("retry receipt changed independent Turn state: state=%q err=%v", newTurn.State, err)
	}
	staleRetry, err := http.NewRequest(http.MethodPost, apiHTTP2.URL+"/v1/approvals/"+approval.ID+"/retry", nil)
	if err != nil {
		t.Fatal(err)
	}
	staleRetry.Header.Set("Idempotency-Key", "stale-retry-after-receipt")
	staleResponse, err := ownerClient2.Do(staleRetry)
	if err != nil {
		t.Fatal(err)
	}
	staleResponse.Body.Close()
	if staleResponse.StatusCode != http.StatusOK {
		t.Fatalf("stale retry response status=%d", staleResponse.StatusCode)
	}
	noSecondCommandCtx, cancelNoSecondCommand := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelNoSecondCommand()
	if duplicateCommand, receiveErr := identity2.ReceiveCommand(noSecondCommandCtx); receiveErr == nil {
		_ = duplicateCommand
		t.Fatal("stale retry handed a second command to the Node")
	}
}

func approvalRetryNodeServer(manager *node.ServerManager) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	return httptest.NewServer(mux)
}

func dialApprovalRetryNode(ctx context.Context, identity node.NodeIdentity, serverURL string) (*node.ProtocolConnection, error) {
	base, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}
	base.Scheme = "ws"
	base.Path = "/v1/nodes/connect"
	base.RawQuery = "node=" + url.QueryEscape(string(identity.Node))
	identity.ConnectURL = base.String()
	auth, err := identity.Authenticator()
	if err != nil {
		return nil, err
	}
	nodeRef := identity.Node
	instance := core.HarnessInstance{
		ID: core.HarnessInstanceID(string(nodeRef) + "/fx"), Node: nodeRef, Kind: core.HarnessFX,
		Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true},
		Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityShell}, Activity: []core.ActivityCapability{core.ActivityPermissionRequest}},
	}
	inventory := core.HarnessInventorySnapshot{Node: nodeRef, Instances: []core.HarnessInstance{instance}, ObservedAt: time.Now().UTC()}
	return node.DialProtocol(ctx, identity.ConnectURL, nodeRef, auth, node.Handshake{
		Node: nodeRef, ProtocolVersion: node.ProtocolVersion, Inventory: inventory, Nonce: "approval-retry-reconnect",
	})
}
