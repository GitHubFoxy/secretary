package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

// Real native Secretary model acceptance for T01. The normal CI test uses a
// scripted harness and cannot measure semantic Worker selection. Run this
// against a DEDICATED, throwaway Secretary instance with a configured, logged-in
// Codex or Claude Code harness. Never point it at your personal production data.
//
//	SECRETARY_T01_LIVE_E2E=1
//	SECRETARY_T01_LIVE_URL=https://test-secretary.example
//	SECRETARY_T01_LIVE_BOOTSTRAP=<the test instance's bootstrap token>
//	go test ./internal/e2e -run '^TestT01LiveSecretaryChoosesWorkerAndClarifies$' -v
func TestT01LiveSecretaryChoosesWorkerAndClarifies(t *testing.T) {
	if os.Getenv("SECRETARY_T01_LIVE_E2E") != "1" {
		t.Skip("opt in to real native Secretary routing acceptance on a disposable instance")
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("SECRETARY_T01_LIVE_URL")), "/")
	token := os.Getenv("SECRETARY_T01_LIVE_BOOTSTRAP")
	if base == "" || token == "" {
		t.Fatal("a dedicated live Secretary URL and bootstrap token are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	encoded, _ := json.Marshal(map[string]string{"bootstrap_token": token})
	response, err := client.Post(base+"/v1/web/session", "application/json", bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("live test instance authentication failed: status=%d", response.StatusCode)
	}

	getEntries := func() []core.ConversationEntry {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/conversation?after_seq=0", nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("conversation read failed: %d", res.StatusCode)
		}
		var page struct {
			Entries []core.ConversationEntry `json:"entries"`
		}
		if err := json.NewDecoder(res.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page.Entries
	}
	// This test needs an isolated instance with no existing tasks or entries.
	if existing := getEntries(); len(existing) != 0 {
		t.Fatal("live acceptance requires a new, empty, disposable Secretary instance")
	}
	post := func(id, input string) int64 {
		t.Helper()
		encoded, _ := json.Marshal(map[string]string{"external_message_id": id, "body": input})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", id)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusAccepted {
			t.Fatalf("live input %q was not accepted: %d", id, res.StatusCode)
		}
		var accepted struct {
			EntrySeq int64 `json:"entry_seq"`
		}
		if err := json.NewDecoder(res.Body).Decode(&accepted); err != nil {
			t.Fatal(err)
		}
		if accepted.EntrySeq <= 0 {
			t.Fatal("input was accepted without a canonical Conversation sequence")
		}
		return accepted.EntrySeq
	}
	// Read from a fixed sequence boundary. A single HTTP page can contain
	// several entries, so do not discard entries after the first match.
	await := func(kind core.EntryKind, afterSeq int64, match func(core.ConversationEntry) bool) core.ConversationEntry {
		t.Helper()
		tick := time.NewTicker(350 * time.Millisecond)
		defer tick.Stop()
		for {
			for _, entry := range getEntries() {
				if entry.Seq > afterSeq && entry.Kind == kind && (match == nil || match(entry)) {
					return entry
				}
			}
			select {
			case <-ctx.Done():
				t.Fatalf("live Secretary did not produce %s after seq %d: %v", kind, afterSeq, ctx.Err())
			case <-tick.C:
			}
		}
	}

	// Public Worker observer endpoints let the test verify that a clarification
	// did not secretly create a new Task, Turn or Attempt.
	getWorkers := func() []core.Worker {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/workers", nil)
		if err != nil { t.Fatal(err) }
		res, err := client.Do(req)
		if err != nil { t.Fatal(err) }
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK { t.Fatalf("list Workers failed: %d", res.StatusCode) }
		var workers []core.Worker
		if err := json.NewDecoder(res.Body).Decode(&workers); err != nil { t.Fatal(err) }
		return workers
	}
	getWorker := func(ref string) core.WorkerDetails {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/workers/"+ref, nil)
		if err != nil { t.Fatal(err) }
		res, err := client.Do(req)
		if err != nil { t.Fatal(err) }
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK { t.Fatalf("read Worker %q failed: %d", ref, res.StatusCode) }
		var details core.WorkerDetails
		if err := json.NewDecoder(res.Body).Decode(&details); err != nil { t.Fatal(err) }
		return details
	}

	// Wait for the addressed Secretary Turn to finish before asserting that
	// no Worker action was sent. A visible clarification alone is not sufficient.
	waitTurnFinished := func(turnID string) {
		t.Helper()
		if turnID == "" {
			t.Fatal("clarification is missing its Secretary Turn ID")
		}
		tick := time.NewTicker(350 * time.Millisecond)
		defer tick.Stop()
		for {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/secretary/turns/"+turnID+"/stream?after_seq=0", nil)
			if err != nil { t.Fatal(err) }
			res, err := client.Do(req)
			if err != nil { t.Fatal(err) }
			if res.StatusCode != http.StatusOK {
				res.Body.Close()
				t.Fatalf("read Secretary Turn %s failed: %d", turnID, res.StatusCode)
			}
			var replay struct {
				Events []core.Event `json:"events"`
			}
			err = json.NewDecoder(res.Body).Decode(&replay)
			res.Body.Close()
			if err != nil { t.Fatal(err) }
			for _, event := range replay.Events {
				if event.Kind == core.SecretaryTurnFinishedEvent { return }
			}
			select {
			case <-ctx.Done():
				t.Fatalf("Secretary Turn %s did not finish: %v", turnID, ctx.Err())
			case <-tick.C:
			}
		}
	}

	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)
	post("t01-live-website-"+stamp,
		"I want a fictional website. Do not create or edit any files or run shell commands. Delegate this as a new Task. Ask me what the website should be about before planning it.")
	first := await(core.EntryWorkerResult, 0, nil)
	if first.WorkerRef == "" || !strings.Contains(first.Body, "?") {
		t.Fatalf("first Worker did not ask a question: %#v", first)
	}
	post("t01-live-vacation-"+stamp,
		"New, unrelated Task: help me plan a fictional vacation. Do not create or edit files or run commands. Delegate this to a different Worker and ask where I want to go.")
	second := await(core.EntryWorkerResult, first.Seq, nil)
	if second.WorkerRef == "" || second.WorkerRef == first.WorkerRef || !strings.Contains(second.Body, "?") {
		t.Fatalf("Secretary did not start an independent Worker asking a question: %#v", second)
	}

	// Both Workers asked questions and remain available for Follow-ups.
	// The next input is deliberately neutral. The model must discover the
	// ambiguity itself; do not tell it to ask for clarification in the input.
	if workers := getWorkers(); len(workers) != 2 {
		t.Fatalf("expected two available Workers before ambiguity, got %d", len(workers))
	}
	firstBefore, secondBefore := getWorker(first.WorkerRef), getWorker(second.WorkerRef)
	if len(firstBefore.Results) != 1 || len(secondBefore.Results) != 1 {
		t.Fatalf("both Workers must have terminal question Results before Yes")
	}
	yesSeq := post("t01-live-ambiguous-"+stamp, "Yes")
	clarify := await(core.EntrySecretary, yesSeq, func(entry core.ConversationEntry) bool {
		body := strings.ToLower(strings.TrimSpace(entry.Body))
		// The question must identify BOTH candidate Tasks, not merely ask
		// a generic unrelated question.
		return strings.Contains(body, "?") &&
			((strings.Contains(body, "website") && strings.Contains(body, "vacation")) ||
				(strings.Contains(body, strings.ToLower(first.WorkerRef)) &&
					strings.Contains(body, strings.ToLower(second.WorkerRef))))
	})
	// The model must identify the ambiguity itself, with both candidates.
	// Check after the Secretary Turn finishes, not while it can still use tools.
	waitTurnFinished(clarify.TurnID)
	if workers := getWorkers(); len(workers) != 2 {
		t.Fatalf("neutral Yes created a new Worker: count=%d", len(workers))
	}
	for _, before := range []core.WorkerDetails{firstBefore, secondBefore} {
		after := getWorker(before.Worker.WorkerRef)
		if len(after.Turns) != len(before.Turns) ||
			len(after.Attempts) != len(before.Attempts) ||
			len(after.Results) != len(before.Results) {
			t.Fatalf("neutral Yes was sent to Worker %s: turns %d→%d, attempts %d→%d, results %d→%d",
				before.Worker.WorkerRef,
				len(before.Turns), len(after.Turns),
				len(before.Attempts), len(after.Attempts),
				len(before.Results), len(after.Results))
		}
	}
	for _, entry := range getEntries() {
		if entry.Seq > yesSeq && entry.Kind == core.EntryWorkerResult {
			t.Fatalf("neutral Yes caused a Worker Result before clarification: %#v", entry)
		}
	}
	post("t01-live-website-answer-"+stamp,
		"For the website Worker: it should be an online bookshop. Please ask any remaining questions. Do not create or edit files.")
	continued := await(core.EntryWorkerResult, clarify.Seq, nil)
	if continued.WorkerRef != first.WorkerRef {
		t.Fatalf("Secretary sent website answer to Worker %q; want %q", continued.WorkerRef, first.WorkerRef)
	}
	t.Logf("live route passed: first=%s unrelated=%s followup=%s", first.WorkerRef, second.WorkerRef, continued.WorkerRef)
}
