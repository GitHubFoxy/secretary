package webapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
	"github.com/coder/websocket"
)

type observerRuntime struct{ session *observerSession }
type observerSession struct {
	activity chan node.Activity
	result   chan node.Result
	queued   chan string
	steered  string
}

func (r *observerRuntime) Start(context.Context, node.StartRequest) (node.Session, error) {
	r.session = &observerSession{activity: make(chan node.Activity, 2), result: make(chan node.Result, 1), queued: make(chan string, 2)}
	return r.session, nil
}
func (s *observerSession) ID() string                                 { return "observer-session" }
func (s *observerSession) Prompt(context.Context, string) error       { return nil }
func (s *observerSession) Queue(_ context.Context, text string) error { s.queued <- text; return nil }
func (s *observerSession) Steer(_ context.Context, text string) (bool, error) {
	s.steered = text
	return true, nil
}
func (s *observerSession) Cancel(context.Context) error {
	s.result <- node.Result{Status: "canceled", Summary: "stopped"}
	return nil
}
func (s *observerSession) Activity() <-chan node.Activity { return s.activity }
func (s *observerSession) Result() <-chan node.Result     { return s.result }
func (s *observerSession) Close() error                   { return nil }

func observerTestServer(t *testing.T, local *node.LocalNode) (*httptest.Server, *http.Client) {
	t.Helper()
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "observer.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachNode(local)
	server := httptest.NewServer(api.Handler())
	jar, _ := cookiejar.New(nil)
	return server, &http.Client{Jar: jar}
}

func TestPublicClientResponsesRedactNativeRuntimeSessionID(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "public-redaction.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, conversation.ID, "inspect")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.AcceptDispatch(ctx, task.ID, "public-worker", "local", "native-secret-session", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEntry(ctx, conversation.ID, core.EntryUser, "Plain Markdown contains xoxb_ABC"); err != nil {
		t.Fatal(err)
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, client, server.URL)
	for _, path := range []string{"/v1/bootstrap", "/v1/workers", "/v1/workers/public-worker/thread", "/v1/conversation"} {
		response, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("path=%s status=%d body=%s", path, response.StatusCode, body)
		}
		if strings.Contains(string(body), "runtime_session_id") || strings.Contains(string(body), "native-secret-session") || strings.Contains(string(body), "xoxb_ABC") {
			t.Fatalf("path=%s leaked native runtime ID: %s", path, body)
		}
	}
}

func TestWorkerDiagnosticViewIncludesOutcomesAndRedactedHarnessDetails(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "diagnostics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "diagnostic-worker", Intent: "inspect", ProjectID: "project", NodeID: "local", HarnessInstanceID: "local/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, "diagnostic-worker")
	if err != nil || len(details.Attempts) != 1 {
		t.Fatalf("details=%#v err=%v", details, err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, details.Attempts[0].ID, core.AttemptOutcomeInput{Status: core.OutcomeFailed, Classification: core.OutcomeFinal, ErrorCode: "harness_failed", ErrorMessage: "failed", Diagnostics: "diagnostic detail", Summary: "failed"}); err != nil {
		t.Fatal(err)
	}
	logDir := t.TempDir()
	log := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"native-runtime-session","update":{"sessionUpdate":"tool_call","title":"shell","rawInput":{"command":"printf safe","token":"top-secret-token"}}}}` + "\n" +
		`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"raw chain-of-thought must not escape"}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(logDir, "diagnostic-worker.jsonl"), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.diagnosticLogDir = logDir
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, client, server.URL)
	response, err := client.Get(server.URL + "/v1/workers/diagnostic-worker/diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	text := string(body)
	for _, want := range []string{"attempt_outcomes", "raw_harness_details", "harness_failed", "shell"} {
		if !strings.Contains(text, want) {
			t.Fatalf("diagnostics missing %q: %s", want, text)
		}
	}
	for _, forbidden := range []string{"runtime_session_id", "native-runtime-session", "top-secret-token", "raw chain-of-thought"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("diagnostics leaked %q: %s", forbidden, text)
		}
	}
	conversationResponse, err := client.Get(server.URL + "/v1/workers/diagnostic-worker")
	if err != nil {
		t.Fatal(err)
	}
	conversationBody, _ := io.ReadAll(conversationResponse.Body)
	conversationResponse.Body.Close()
	if strings.Contains(string(conversationBody), "shell") || strings.Contains(string(conversationBody), "harness_details") {
		t.Fatalf("normal Worker contract contains diagnostic details: %s", conversationBody)
	}
}

func TestWorkerObserverReplaysBurstActivityFromDurableStore(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "burst-observer.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "burst-observer", Intent: "inspect", ProjectID: "project", NodeID: "local", HarnessInstanceID: "local/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 128; index++ {
		activity := core.Activity{
			Metadata: core.ActivityMetadata{EventID: "burst-event-" + strconv.Itoa(index), Node: "local", HarnessInstanceID: "local/fx", WorkerRef: "burst-observer", TurnID: turn.ID, AttemptID: attempt.ID, Sequence: uint64(index + 1), ObservedAt: time.Now().UTC()},
			Kind:     core.ActivityKindAssistantTextDelta,
			Text:     "activity-" + strconv.Itoa(index),
		}
		if _, err := store.RecordNodeActivityReplay(ctx, activity); err != nil {
			t.Fatalf("activity %d: %v", index, err)
		}
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, client, server.URL)
	response, err := client.Get(server.URL + "/v1/workers/burst-observer/activity?after_seq=0")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var events []core.Event
	if err := json.NewDecoder(response.Body).Decode(&events); err != nil {
		t.Fatal(err)
	}
	activityCount := 0
	for _, event := range events {
		if event.Kind == "attempt.activity" {
			activityCount++
		}
	}
	if response.StatusCode != http.StatusOK || activityCount != 128 {
		t.Fatalf("status=%d events=%d activity=%d", response.StatusCode, len(events), activityCount)
	}
}

func TestWorkerObserverExposesSafeToolFailureMetadataWithoutToolPayloads(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "failure-observer.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "failure-observer", Intent: "inspect weather", ProjectID: "project", NodeID: "local", HarnessInstanceID: "local/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "inspect weather"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	activities := []core.Activity{
		{Metadata: core.ActivityMetadata{EventID: "ticket30-call", Node: "local", HarnessInstanceID: "local/fx", WorkerRef: "failure-observer", TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: core.ActivityKindToolCall, ToolCall: &core.ToolCall{Name: "web_fetch", Arguments: json.RawMessage(`{"url":"https://example.invalid/?token=raw-private-argument"}`), Preview: "example.invalid"}},
		{Metadata: core.ActivityMetadata{EventID: "ticket30-result", Node: "local", HarnessInstanceID: "local/fx", WorkerRef: "failure-observer", TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 2, ObservedAt: time.Now().UTC()}, Kind: core.ActivityKindToolResult, ToolResult: &core.ToolResult{Name: "web_fetch", Output: "raw private tool output", Status: "failed", Preview: "example.invalid", Failure: &core.ToolFailureMetadata{Category: core.ToolFailureCategoryHTTP, Code: core.ToolFailureCodeHTTPError, HTTPStatus: 503}}},
	}
	for _, activity := range activities {
		if _, err := store.RecordNodeActivityReplay(ctx, activity); err != nil {
			t.Fatal(err)
		}
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, client, server.URL)
	response, err := client.Get(server.URL + "/v1/workers/failure-observer/activity?after_seq=0")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s err=%v", response.StatusCode, body, err)
	}
	text := string(body)
	for _, want := range []string{`"category":"http"`, `"code":"http_error"`, `"http_status":503`, `"name":"web_fetch"`, `"status":"failed"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("public observer omitted %q: %s", want, text)
		}
	}
	for _, forbidden := range []string{"raw-private-argument", "raw private tool output", `"arguments"`, `"output"`, `"error"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public observer leaked %q: %s", forbidden, text)
		}
	}
}

func TestLegacyWorkerFailureReadModelsSanitizeOnReopen(t *testing.T) {
	for _, malformedStatus := range []bool{false, true} {
		t.Run(fmt.Sprint(malformedStatus), func(t *testing.T) {
			var legacyStatus any = "failed"
			if malformedStatus {
				legacyStatus = map[string]any{"message": "Digest username=owner, response=privatevalue https://unknown.invalid/private"}
			}
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "legacy-worker-failure.db")
			store, err := core.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			_, conversation, err := store.CreatePersonWithConversation(ctx)
			if err != nil {
				t.Fatal(err)
			}
			worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
				WorkerRef: "legacy-failure-reader", Intent: "inspect", ProjectID: "project", NodeID: "local", HarnessInstanceID: "local/fx",
			}, core.TurnSpec{Input: "inspect"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
				t.Fatal(err)
			}
			_, result, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{
				Status: core.OutcomeFailed, Classification: core.OutcomeFinal, Summary: "safe failure",
				ErrorCode: "internal_error", ErrorMessage: "safe error", Diagnostics: "safe diagnostic",
				FailureCode: "usage_limit_exceeded", FailureCategory: core.FailureCategoryUsageLimit,
			})
			if err != nil || result == nil {
				t.Fatalf("record terminal failure: result=%#v err=%v", result, err)
			}
			successWorker, _, successAttempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
				WorkerRef: "legacy-success-reader", Intent: "report", ProjectID: "project", NodeID: "local", HarnessInstanceID: "local/fx",
			}, core.TurnSpec{Input: "report"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.SetPhase4AttemptActive(ctx, successAttempt.ID); err != nil {
				t.Fatal(err)
			}
			successSummary := "Successful output keeps its literal Bearer demo wording"
			if _, successResult, _, err := store.RecordAttemptOutcome(ctx, successAttempt.ID, core.AttemptOutcomeInput{
				Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: successSummary,
			}); err != nil || successResult == nil {
				t.Fatalf("record successful output: result=%#v err=%v", successResult, err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}

			raw, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			rawSummary := "Provider failed: Bearer legacy-bearer-secret; Digest username=owner, response=legacy-digest-secret"
			rawDetails := `{"category":"usage_limit","provider_code":"usage_limit_exceeded","message":"Usage limit exceeded; try again at 4:37 PM; Bearer details-bearer-secret","technical":"acp rpc -32603; Digest username=owner, response=details-digest-secret"}`
			if _, err := raw.ExecContext(ctx, `UPDATE phase4_results SET summary = ?, failure_code = ?, failure_details = ? WHERE id = ?`, rawSummary, "Bearer result-code-secret", rawDetails, result.ID); err != nil {
				raw.Close()
				t.Fatal(err)
			}
			if _, err := raw.ExecContext(ctx, `UPDATE phase4_attempt_outcomes SET error_code = ?, error_message = ?, diagnostics = ? WHERE attempt_id = ?`,
				"Digest username=owner, response=outcome-code-secret", "Bearer outcome-message-secret", "diagnostic "+strings.Repeat("Bearer sk-outcome-diagnostic-secret ", 60), attempt.ID); err != nil {
				raw.Close()
				t.Fatal(err)
			}
			if _, err := raw.ExecContext(ctx, `UPDATE workers SET last_result_summary = ? WHERE id = ?`, rawSummary, worker.ID); err != nil {
				raw.Close()
				t.Fatal(err)
			}
			if _, err := raw.ExecContext(ctx, `UPDATE conversation_entries SET body = ? WHERE result_id = ?`, rawSummary, result.ID); err != nil {
				raw.Close()
				t.Fatal(err)
			}
			var legacyEventID string
			var legacyEventSeq int64
			if err := raw.QueryRowContext(ctx, `SELECT id, seq FROM events WHERE kind = 'result.accepted' AND aggregate_id = ?`, result.ID).Scan(&legacyEventID, &legacyEventSeq); err != nil {
				t.Fatal(err)
			}
			legacyPayload, _ := json.Marshal(map[string]any{"id": result.ID, "status": legacyStatus, "extra": map[string]any{"message": "Digest username=owner, response=privatevalue https://unknown.invalid/private"}, "text": "Digest username=owner, response=privatevalue https://unknown.invalid/private", "summary": rawSummary + " https://unknown.invalid/private " + strings.Repeat("я", 1200), "attempt_id": attempt.ID, "failure": map[string]any{"category": 123, "message": "Digest username=owner, response=privatevalue https://unknown.invalid/private " + strings.Repeat("я", 1200)}, "failure_category": []any{"Digest username=owner, response=privatevalue https://unknown.invalid/private "}, "diagnostics": map[string]any{"message": "Digest username=owner, response=privatevalue https://unknown.invalid/private "}, "error_code": []any{"Digest username=owner, response=privatevalue https://unknown.invalid/private "}, "error_message": map[string]any{"message": "Digest username=owner, response=privatevalue https://unknown.invalid/private "}, "failure_code": []any{"Digest username=owner, response=privatevalue https://unknown.invalid/private "}})
			if _, err := raw.ExecContext(ctx, `UPDATE events SET payload_json = ? WHERE kind = 'result.accepted' AND aggregate_id = ?`, string(legacyPayload), result.ID); err != nil {
				t.Fatal(err)
			}
			if err := raw.Close(); err != nil {
				t.Fatal(err)
			}

			reopened, err := core.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			details, err := reopened.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
			if err != nil {
				t.Fatal(err)
			}
			if len(details.Results) != 1 || len(details.Outcomes) != 1 {
				t.Fatalf("read failure model: results=%d outcomes=%d", len(details.Results), len(details.Outcomes))
			}
			successDetails, err := reopened.WorkerDetailsForConversation(ctx, conversation.ID, successWorker.WorkerRef)
			if err != nil || len(successDetails.Results) != 1 || successDetails.Results[0].Summary != successSummary || successDetails.Worker.LastResultSummary != successSummary {
				t.Fatalf("successful output changed on read: details=%#v err=%v", successDetails, err)
			}
			encodedDetails, err := json.Marshal(details)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"legacy-bearer-secret", "legacy-digest-secret", "result-code-secret", "details-bearer-secret", "details-digest-secret", "outcome-code-secret", "outcome-message-secret", "sk-outcome-diagnostic-secret"} {
				if strings.Contains(string(encodedDetails), secret) {
					t.Errorf("WorkerDetails leaked legacy failure text %q: %s", secret, encodedDetails)
				}
			}
			if details.Results[0].FailureCode != "" || details.Results[0].Summary == rawSummary || details.Outcomes[0].Diagnostics == "" {
				t.Fatalf("legacy read fields were not sanitized: result=%#v outcome=%#v", details.Results[0], details.Outcomes[0])
			}
			entries, err := reopened.EntriesAfter(ctx, conversation.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			foundFailureEntry := false
			for _, entry := range entries {
				if entry.ResultID == result.ID {
					foundFailureEntry = true
					if entry.Body == rawSummary {
						t.Fatalf("legacy Conversation result was not sanitized: %#v", entry)
					}
				}
			}
			if !foundFailureEntry {
				t.Fatalf("legacy Conversation result missing from entries: %#v", entries)
			}
			replay, err := reopened.ReplayConversation(ctx, conversation.ID, 0)
			foundFailureEntry = false
			for _, entry := range replay.Entries {
				if entry.ResultID == result.ID {
					foundFailureEntry = entry.Body != rawSummary
				}
			}
			if err != nil || !foundFailureEntry {
				t.Fatalf("legacy Conversation replay was not sanitized: replay=%#v err=%v", replay, err)
			}

			api, err := New(ctx, reopened, "bootstrap")
			if err != nil {
				t.Fatal(err)
			}
			events, err := api.workerEventsWithContext(ctx, worker.WorkerRef, 0)
			if err != nil {
				t.Fatal(err)
			}
			foundResultEvent := false
			for _, event := range events {
				if event.Kind != "result.accepted" {
					continue
				}
				foundResultEvent = true
				public := sanitizePublicEvent(event)
				var payload core.Result
				if err := json.Unmarshal(public.Payload, &payload); err != nil {
					t.Fatal(err)
				}
				if payload.ID != result.ID || payload.AttemptID != attempt.ID || event.ID != legacyEventID || event.Seq != legacyEventSeq || len([]rune(payload.Summary)) > 1000 {
					t.Fatalf("replay identity/bound: %#v %#v", event, payload)
				}
				for _, secret := range []string{"legacy-bearer-secret", "legacy-digest-secret", "privatevalue", "username=owner", "unknown.invalid"} {
					if strings.Contains(string(public.Payload), secret) {
						t.Fatalf("Web replay leaked %s", secret)
					}
				}
			}
			if !foundResultEvent {
				t.Fatal("result event missing")
			}
			diagnostics, err := api.buildWorkerDiagnostics(details)
			if err != nil || len(diagnostics.AttemptOutcomes) != 1 || diagnostics.AttemptOutcomes[0].Failure == nil || diagnostics.AttemptOutcomes[0].Summary == "" {
				t.Fatalf("terminal diagnostic metadata missing: %#v %v", diagnostics, err)
			}
			server := httptest.NewServer(api.Handler())
			defer server.Close()
			client := &http.Client{Jar: mustWebCookieJar(t)}
			login(t, client, server.URL)
			for _, route := range []string{"/v1/workers/" + worker.WorkerRef, "/v1/workers/" + worker.WorkerRef + "/diagnostics", "/v1/conversation"} {
				response, err := client.Get(server.URL + route)
				if err != nil {
					t.Fatal(err)
				}
				body, readErr := io.ReadAll(response.Body)
				response.Body.Close()
				if readErr != nil || response.StatusCode != http.StatusOK {
					t.Fatalf("GET %s status=%d err=%v body=%s", route, response.StatusCode, readErr, body)
				}
				if strings.HasSuffix(route, "/diagnostics") {
					var dto workerDiagnosticResponse
					if err := json.Unmarshal(body, &dto); err != nil {
						t.Fatal(err)
					}
					if len(dto.AttemptOutcomes) != 1 {
						t.Fatalf("diagnostic outcomes: %#v", dto)
					}
					outcome := dto.AttemptOutcomes[0]
					if outcome.Failure == nil || outcome.Failure.Category != core.FailureCategoryUsageLimit || outcome.Failure.ProviderCode != "usage_limit_exceeded" || !strings.Contains(outcome.Failure.Message, "4:37 PM") || !strings.Contains(outcome.Failure.Technical, "acp rpc -32603") || len([]rune(outcome.Diagnostics)) > 1000 {
						t.Fatalf("terminal diagnostics lost reason/reset/technical: %#v", outcome)
					}
				}

				for _, secret := range []string{"legacy-bearer-secret", "legacy-digest-secret", "result-code-secret", "details-bearer-secret", "details-digest-secret", "outcome-code-secret", "outcome-message-secret", "sk-outcome-diagnostic-secret"} {
					if strings.Contains(string(body), secret) {
						t.Errorf("owner API %s leaked legacy failure text %q: %s", route, secret, body)
					}
				}
			}
		})
	}
}

func TestWorkerObserverStatusSteerStopAndActivity(t *testing.T) {
	runtime := &observerRuntime{}
	local := node.NewLocal(runtime)
	storeServer, client := observerTestServer(t, local)
	defer storeServer.Close()
	login(t, client, storeServer.URL)
	if _, err := local.Dispatch(context.Background(), node.StartRequest{WorkerRef: "worker-1"}); err != nil {
		t.Fatal(err)
	}

	response, err := client.Get(storeServer.URL + "/v1/workers/worker-1")
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]any
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	if _, leaked := status["session_id"]; leaked {
		t.Fatalf("public Worker response leaked native session ID: %#v", status)
	}
	request, _ := http.NewRequest(http.MethodPost, storeServer.URL+"/v1/workers/worker-1/steer", bytes.NewBufferString(`{"text":"change"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "observer-steer")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if runtime.session.steered != "change" {
		t.Fatalf("steered=%q", runtime.session.steered)
	}

	parsed, _ := url.Parse(storeServer.URL)
	header := http.Header{}
	for _, cookie := range client.Jar.Cookies(parsed) {
		header.Add("Cookie", cookie.String())
	}
	parsed.Scheme = "ws"
	parsed.Path = "/v1/workers/worker-1/activity"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, parsed.String(), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	runtime.session.activity <- node.Activity{Kind: node.ActivityTool, Text: "shell"}
	_, payload, err := connection.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var activity node.Activity
	if err := json.Unmarshal(payload, &activity); err != nil || activity.Text != "shell" {
		t.Fatalf("activity=%#v err=%v", activity, err)
	}
	request, _ = http.NewRequest(http.MethodPost, storeServer.URL+"/v1/workers/worker-1/queue", bytes.NewBufferString(`{"text":"after idle"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "observer-queue")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("queue status=%d", response.StatusCode)
	}
	if got := <-runtime.session.queued; got != "after idle" {
		t.Fatalf("queued=%q", got)
	}

	request, _ = http.NewRequest(http.MethodPost, storeServer.URL+"/v1/workers/worker-1/stop", nil)
	request.Header.Set("Idempotency-Key", "observer-stop")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("stop status=%d", response.StatusCode)
	}
}

type errorSteerSession struct {
	observerSession
	steerErr error
}

func (s *errorSteerSession) Steer(context.Context, string) (bool, error) {
	return false, s.steerErr
}

type errorSteerRuntime struct {
	session *errorSteerSession
}

func (r *errorSteerRuntime) Start(context.Context, node.StartRequest) (node.Session, error) {
	r.session = &errorSteerSession{
		observerSession: observerSession{activity: make(chan node.Activity, 2), result: make(chan node.Result, 1), queued: make(chan string, 2)},
		steerErr: &node.SteeringError{
			Failure: core.WorkerFailure{
				Category:     core.FailureCategoryUsageLimit,
				ProviderCode: "usage_limit_exceeded",
				Message:      "Usage limit reached",
				Technical:    "acp rpc -32603: Internal error",
			},
			Err: errors.New("usage_limit_exceeded"),
		},
	}
	return r.session, nil
}

func TestWorkerSteerError_ReturnsStructuredJSON(t *testing.T) {
	runtime := &errorSteerRuntime{}
	local := node.NewLocal(runtime)
	storeServer, client := observerTestServer(t, local)
	defer storeServer.Close()
	login(t, client, storeServer.URL)
	if _, err := local.Dispatch(context.Background(), node.StartRequest{WorkerRef: "worker-steer-err"}); err != nil {
		t.Fatal(err)
	}

	request, _ := http.NewRequest(http.MethodPost, storeServer.URL+"/v1/workers/worker-steer-err/steer", bytes.NewBufferString(`{"text":"continue"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "steer-err-key")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got: %d", response.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if body["category"] != "usage_limit" {
		t.Fatalf("expected category 'usage_limit', got: %#v", body["category"])
	}
	if body["provider_code"] != "usage_limit_exceeded" {
		t.Fatalf("expected provider_code 'usage_limit_exceeded', got: %#v", body["provider_code"])
	}
	if body["technical"] != "acp rpc -32603: Internal error" {
		t.Fatalf("expected technical 'acp rpc -32603: Internal error', got: %#v", body["technical"])
	}
	if !strings.Contains(fmt.Sprint(body["error"]), "Usage limit reached") {
		t.Fatalf("expected error to contain 'Usage limit reached', got: %#v", body["error"])
	}

	repeatRequest, _ := http.NewRequest(http.MethodPost, storeServer.URL+"/v1/workers/worker-steer-err/steer", bytes.NewBufferString(`{"text":"continue"}`))
	repeatRequest.Header.Set("Content-Type", "application/json")
	repeatRequest.Header.Set("Idempotency-Key", "steer-err-key")
	repeatResponse, err := client.Do(repeatRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer repeatResponse.Body.Close()

	if repeatResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request on repeat, got: %d", repeatResponse.StatusCode)
	}

	var repeatBody map[string]any
	if err := json.NewDecoder(repeatResponse.Body).Decode(&repeatBody); err != nil {
		t.Fatalf("decode repeat response body: %v", err)
	}

	if repeatBody["category"] != "usage_limit" {
		t.Fatalf("expected repeat category 'usage_limit', got: %#v", repeatBody["category"])
	}
	if repeatBody["provider_code"] != "usage_limit_exceeded" {
		t.Fatalf("expected repeat provider_code 'usage_limit_exceeded', got: %#v", repeatBody["provider_code"])
	}
	if repeatBody["technical"] != "acp rpc -32603: Internal error" {
		t.Fatalf("expected repeat technical 'acp rpc -32603: Internal error', got: %#v", repeatBody["technical"])
	}
	if !strings.Contains(fmt.Sprint(repeatBody["error"]), "Usage limit reached") {
		t.Fatalf("expected repeat error to contain 'Usage limit reached', got: %#v", repeatBody["error"])
	}
}

func TestWorkerSteerError_UnknownMetadataSanitized(t *testing.T) {
	runtime := &errorSteerRuntime{}
	local := node.NewLocal(runtime)
	storeServer, client := observerTestServer(t, local)
	defer storeServer.Close()
	login(t, client, storeServer.URL)
	if _, err := local.Dispatch(context.Background(), node.StartRequest{WorkerRef: "worker-steer-unknown"}); err != nil {
		t.Fatal(err)
	}
	runtime.session.steerErr = &node.SteeringError{
		Failure: core.WorkerFailure{
			Category:     "sk-private-secret",
			ProviderCode: "sk-private-secret-key",
			Message:      "Bearer private-message-secret",
			Technical:    "sk-technical-secret",
		},
		Err: errors.New("upstream unavailable"),
	}

	request, _ := http.NewRequest(http.MethodPost, storeServer.URL+"/v1/workers/worker-steer-unknown/steer", bytes.NewBufferString(`{"text":"continue"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "steer-unknown-key")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got: %d", response.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body["category"] != string(core.FailureCategoryInternalError) {
		t.Fatalf("expected unknown failure category collapsed to internal_error, got: %#v", body["category"])
	}
	if body["provider_code"] != "" {
		t.Fatalf("expected secret provider_code excluded, got: %#v", body["provider_code"])
	}
	if strings.Contains(fmt.Sprint(body), "private-message-secret") || strings.Contains(fmt.Sprint(body), "technical-secret") {
		t.Fatalf("public error leaked Node metadata: %#v", body)
	}
}

func TestProductionLegacyWorkerHistoryIsReadOnlyWithoutEmbeddedNode(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "legacy-observer.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil { t.Fatal(err) }
	task, err := store.CreateTask(ctx, conversation.ID, "legacy task")
	if err != nil { t.Fatal(err) }
	if _, _, _, err := store.AcceptDispatch(ctx, task.ID, "legacy-worker", "local", "historical-native-id", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil { t.Fatal(err) }
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, client, server.URL)
	response, err := client.Get(server.URL + "/v1/workers/legacy-worker/thread")
	if err != nil { t.Fatal(err) }
	response.Body.Close()
	if response.StatusCode != http.StatusOK { t.Fatalf("legacy history hidden: %d", response.StatusCode) }
	for _, action := range []string{"steer", "queue", "stop"} {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/workers/legacy-worker/"+action, strings.NewReader(`{"text":"continue"}`))
		if err != nil { t.Fatal(err) }
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil { t.Fatal(err) }
		response.Body.Close()
		if response.StatusCode != http.StatusConflict {
			t.Fatalf("legacy %s unexpectedly reached an embedded Session: %d", action, response.StatusCode)
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/workers/does-not-exist/stop", strings.NewReader("{}"))
	if err != nil { t.Fatal(err) }
	response, err = client.Do(request)
	if err != nil { t.Fatal(err) }
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound { t.Fatalf("unknown Worker status=%d", response.StatusCode) }
}
