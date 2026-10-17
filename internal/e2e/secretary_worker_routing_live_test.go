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
//   SECRETARY_T01_LIVE_E2E=1
//   SECRETARY_T01_LIVE_URL=https://test-secretary.example
//   SECRETARY_T01_LIVE_BOOTSTRAP=<the test instance's bootstrap token>
//   go test ./internal/e2e -run '^TestT01LiveSecretaryChoosesWorkerAndClarifies$' -v
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
	if err != nil { t.Fatal(err) }
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	encoded, _ := json.Marshal(map[string]string{"bootstrap_token": token})
	response, err := client.Post(base+"/v1/web/session", "application/json", bytes.NewReader(encoded))
	if err != nil { t.Fatal(err) }
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("live test instance authentication failed: status=%d", response.StatusCode)
	}

	var after int64
	getEntries := func() []core.ConversationEntry {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/conversation?after_seq="+strconv.FormatInt(after, 10), nil)
		if err != nil { t.Fatal(err) }
		res, err := client.Do(req)
		if err != nil { t.Fatal(err) }
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("conversation read failed: %d", res.StatusCode)
		}
		var page struct { Entries []core.ConversationEntry `json:"entries"` }
		if err := json.NewDecoder(res.Body).Decode(&page); err != nil { t.Fatal(err) }
		return page.Entries
	}
	// This test needs an isolated instance with no existing tasks or entries.
	if existing := getEntries(); len(existing) != 0 {
		t.Fatal("live acceptance requires a new, empty, disposable Secretary instance")
	}
	post := func(id, input string) {
		t.Helper()
		encoded, _ := json.Marshal(map[string]string{"external_message_id": id, "body": input})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(encoded))
		if err != nil { t.Fatal(err) }
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", id)
		res, err := client.Do(req)
		if err != nil { t.Fatal(err) }
		defer res.Body.Close()
		if res.StatusCode != http.StatusAccepted {
			t.Fatalf("live input %q was not accepted: %d", id, res.StatusCode)
		}
	}
	await := func(kind core.EntryKind) core.ConversationEntry {
		t.Helper()
		tick := time.NewTicker(350 * time.Millisecond)
		defer tick.Stop()
		for {
			for _, entry := range getEntries() {
				if entry.Seq > after { after = entry.Seq }
				if entry.Kind == kind {
					return entry
				}
			}
			select {
			case <-ctx.Done():
				t.Fatalf("live Secretary did not produce %s: %v", kind, ctx.Err())
			case <-tick.C:
			}
		}
	}

	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)
	post("t01-live-website-"+stamp,
		"I want a fictional website. Do not create or edit any files or run shell commands. Delegate this as a new Task. Ask me what the website should be about before planning it.")
	first := await(core.EntryWorkerResult)
	if first.WorkerRef == "" || !strings.Contains(first.Body, "?") {
		t.Fatalf("first Worker did not ask a question: %#v", first)
	}
	post("t01-live-vacation-"+stamp,
		"New, unrelated Task: help me plan a fictional vacation. Do not create or edit files or run commands. Delegate this to a different Worker and ask where I want to go.")
	second := await(core.EntryWorkerResult)
	if second.WorkerRef == "" || second.WorkerRef == first.WorkerRef || !strings.Contains(second.Body, "?") {
		t.Fatalf("Secretary did not start an independent Worker asking a question: %#v", second)
	}

	// Both Workers have asked questions. This reply deliberately omits which
	// task is intended, and explicitly requires Secretary to clarify.
	post("t01-live-ambiguous-"+stamp,
		"Yes. I have not said whether that is for the website or vacation. Ask me which Worker I mean. Do not guess.")
	clarify := await(core.EntrySecretary)
	if !strings.Contains(clarify.Body, "?") {
		t.Fatalf("Secretary did not ask for Worker clarification: %#v", clarify)
	}
	post("t01-live-website-answer-"+stamp,
		"For the website Worker: it should be an online bookshop. Please ask any remaining questions. Do not create or edit files.")
	continued := await(core.EntryWorkerResult)
	if continued.WorkerRef != first.WorkerRef {
		t.Fatalf("Secretary sent website answer to Worker %q; want %q", continued.WorkerRef, first.WorkerRef)
	}
	t.Logf("live route passed: first=%s unrelated=%s followup=%s", first.WorkerRef, second.WorkerRef, continued.WorkerRef)
}
