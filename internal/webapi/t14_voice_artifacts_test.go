package webapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
	"github.com/beruseruko/secretary/internal/telegram"
)

type t14TelegramTransport struct {
	audio []byte
	downloads int
	notices []telegram.OutgoingMessage
	failNotices int
}

func (t *t14TelegramTransport) GetUpdates(context.Context, int64, time.Duration) ([]telegram.Update, error) { return nil, nil }
func (t *t14TelegramTransport) SendMessage(_ context.Context, message telegram.OutgoingMessage) error {
	if t.failNotices > 0 {
		t.failNotices--
		return errors.New("Telegram notification temporarily unavailable")
	}
	t.notices = append(t.notices, message)
	return nil
}
func (*t14TelegramTransport) SendChatAction(context.Context, int64, int64, string) error { return nil }
func (*t14TelegramTransport) CreateForumTopic(context.Context, int64, string) (telegram.ForumTopic, error) {
	return telegram.ForumTopic{}, nil
}
func (*t14TelegramTransport) GetFile(context.Context, string) (telegram.TelegramFile, error) {
	return telegram.TelegramFile{FilePath: "voice.oga", FileSize: 5}, nil
}
func (t *t14TelegramTransport) DownloadFile(context.Context, string, int64) ([]byte, error) {
	t.downloads++
	return append([]byte(nil), t.audio...), nil
}

type t14ASR struct { calls *int; fail bool }
func (s t14ASR) Transcribe(context.Context, []byte) (string, error) {
	*s.calls++
	if s.fail {
		return "", errors.New("ASR unavailable")
	}
	return "Привет, мир. Don't delete three files.", nil
}

type t14InventorySource struct { nodeRef core.NodeReference }

func (s t14InventorySource) Discover(context.Context) (core.HarnessInventorySnapshot, error) {
	return core.HarnessInventorySnapshot{
		Node: s.nodeRef,
		ObservedAt: time.Now().UTC(),
		Instances: []core.HarnessInstance{{
			ID: core.HarnessInstanceID(string(s.nodeRef)+"/fx"),
			Node: s.nodeRef, Kind: core.HarnessFX, Version: "test",
			Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true},
			Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityCancel}},
		}},
	}, nil
}

type t14VoiceWorker struct {
	original []byte
	transcript []byte
	seen chan string
}

func (v *t14VoiceWorker) Start(_ context.Context, req node.StartRequest) (node.Session, error) {
	paths, err := filepath.Glob(filepath.Join(req.Workspace, ".secretary-artifacts", "art_*"))
	if err != nil || len(paths) != 2 {
		return nil, fmt.Errorf("want two staged artifacts, got %v: %w", paths, err)
	}
	matchedOriginal, matchedTranscript := false, false
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil { return nil, err }
		if bytes.Equal(data, v.original) { matchedOriginal = true }
		if bytes.Equal(data, v.transcript) { matchedTranscript = true }
	}
	if !matchedOriginal || !matchedTranscript {
		return nil, errors.New("Worker did not receive both original and transcript")
	}
	v.seen <- req.Task
	result := make(chan node.Result, 1)
	result <- node.Result{Status: "succeeded", Summary: "Read original and Markdown transcript"}
	return &artifactWorkerSession{result: result}, nil
}

func TestT14TelegramVoiceArtifactToRemoteWorkerAndReconnect(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "server.sqlite")
	store, err := core.Open(ctx, dbPath)
	if err != nil { t.Fatal(err) }
	api, err := New(ctx, store, "bootstrap")
	if err != nil { t.Fatal(err) }
	api.AttachInternalCredential("t14-internal-credential")
	manager, err := node.NewServerManagerWithConfig(ctx, store, node.ServerConfig{
		PairingTokens: []string{"t14-enroll"}, AdminToken: "t14-admin",
	})
	if err != nil { t.Fatal(err) }
	api.AttachNodeService(manager)
	server := httptest.NewServer(api.Handler())
	identity, err := node.EnrollNode(ctx, server.Client(), server.URL, "t14-enroll", "voice-remote")
	if err != nil { t.Fatal(err) }
	tr := &t14TelegramTransport{audio: []byte("audio")}
	asrCalls := 0
	statePath := filepath.Join(t.TempDir(), "telegram-state.json")
	client := &telegram.HTTPServerClient{BaseURL: server.URL, Credential: "t14-internal-credential"}
	adapter, err := telegram.New(telegram.Config{StatePath: statePath, OwnerChatID: 100,
		ASRTranscriber: t14ASR{calls: &asrCalls}}, tr, client)
	if err != nil { t.Fatal(err) }
	voiceUpdate := telegram.Update{ID: 34, Message: &telegram.Message{
		ChatID: 100, FromID: 100, MessageID: 90, ChatType: "private",
		Voice: &telegram.Voice{FileID: "voice-34", FileUniqueID: "source-34", Duration: 2, FileSize: 5},
	}}
	if err := adapter.HandleUpdate(ctx, voiceUpdate); err != nil { t.Fatal(err) }
	record, ok := adapter.Transcript("34")
	if !ok || record.SourceArtifactID == "" || record.TranscriptArtifactID == "" {
		t.Fatalf("transcript not persisted: %+v", record)
	}
	if tr.downloads != 1 || asrCalls != 1 { t.Fatalf("downloads=%d ASR=%d", tr.downloads, asrCalls) }
	person, conversation, err := store.EnsureOwner(ctx)
	if err != nil { t.Fatal(err) }
	original, originalBytes, err := store.ArtifactForPerson(ctx, person.ID, record.SourceArtifactID)
	if err != nil { t.Fatal(err) }
	if original.ProcessingStatus != "ready" || original.TranscriptArtifactID != record.TranscriptArtifactID ||
		original.SourceIdentity != "telegram:34:100:90" || !bytes.Equal(originalBytes, tr.audio) {
		t.Fatalf("original invalid: %+v %q", original, originalBytes)
	}
	transcript, transcriptBytes, err := store.ArtifactForPerson(ctx, person.ID, record.TranscriptArtifactID)
	if err != nil { t.Fatal(err) }
	if transcript.SourceArtifactID != original.ID || transcript.MediaType != "text/markdown" ||
		!strings.Contains(string(transcriptBytes), "Привет, мир.") ||
		!strings.Contains(string(transcriptBytes), "artifact://"+original.ID) {
		t.Fatalf("transcript invalid: %+v %q", transcript, transcriptBytes)
	}
	remoteOriginal, err := identity.FetchArtifact(ctx, original.ID)
	if err != nil || !bytes.Equal(remoteOriginal.Data, originalBytes) { t.Fatalf("remote audio: %v", err) }
	remoteTranscript, err := identity.FetchArtifact(ctx, transcript.ID)
	if err != nil || !bytes.Equal(remoteTranscript.Data, transcriptBytes) { t.Fatalf("remote transcript: %v", err) }
	if err := adapter.HandleUpdate(ctx, voiceUpdate); err != nil { t.Fatal(err) }
	adapter, err = telegram.New(telegram.Config{StatePath: statePath, OwnerChatID: 100, ASRTranscriber: t14ASR{calls: &asrCalls}}, tr, client)
	if err != nil { t.Fatal(err) }
	if err := adapter.HandleUpdate(ctx, voiceUpdate); err != nil { t.Fatal(err) }
	if asrCalls != 1 || tr.downloads != 1 { t.Fatal("Telegram replay repeated ASR or download") }
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil { t.Fatal(err) }
	nOriginal, nTranscript := 0, 0
	inbound := ""
	for _, entry := range entries {
		if entry.Kind == core.EntryUser {
			inbound = entry.Body
		}
		if entry.Kind == core.EntrySystem && strings.Contains(entry.Body, "artifact://"+original.ID) { nOriginal++ }
		if entry.Kind == core.EntrySystem && strings.Contains(entry.Body, "artifact://"+transcript.ID) { nTranscript++ }
	}
	if nOriginal != 1 || nTranscript != 1 { t.Fatalf("duplicate Artifact announcements: %d %d", nOriginal, nTranscript) }
	if !strings.Contains(inbound, "artifact://"+original.ID) || !strings.Contains(inbound, "artifact://"+transcript.ID) || !strings.Contains(inbound, "Привет, мир.") {
		t.Fatalf("canonical Telegram inbound lost transcript or references: %q", inbound)
	}
	server.Close()
	if err := store.Close(); err != nil { t.Fatal(err) }
	restarted, err := core.Open(ctx, dbPath)
	if err != nil { t.Fatal(err) }
	defer restarted.Close()
	nextAPI, err := New(ctx, restarted, "bootstrap")
	if err != nil { t.Fatal(err) }
	nextAPI.AttachInternalCredential("t14-internal-credential")
	nextManager, err := node.NewServerManagerWithConfig(ctx, restarted, node.ServerConfig{
		PairingTokens: []string{"t14-enroll"}, AdminToken: "t14-admin",
	})
	if err != nil { t.Fatal(err) }
	nextAPI.AttachNodeService(nextManager)
	nextServer := httptest.NewServer(nextAPI.Handler())
	defer nextServer.Close()
	identity.ConnectURL = strings.Replace(identity.ConnectURL, strings.TrimPrefix(server.URL, "http://"), strings.TrimPrefix(nextServer.URL, "http://"), 1)
	reconnectedOriginal, err := identity.FetchArtifact(ctx, original.ID)
	if err != nil || !bytes.Equal(reconnectedOriginal.Data, originalBytes) { t.Fatalf("reconnected original: %v", err) }
	reconnectedTranscript, err := identity.FetchArtifact(ctx, transcript.ID)
	if err != nil || !bytes.Equal(reconnectedTranscript.Data, transcriptBytes) { t.Fatalf("reconnected transcript: %v", err) }
	runtime := &t14VoiceWorker{original: originalBytes, transcript: transcriptBytes, seen: make(chan string, 1)}
	nodeStore, err := node.OpenLocalStore(filepath.Join(t.TempDir(), "remote-state.json"))
	if err != nil { t.Fatal(err) }
	defer nodeStore.Close()
	daemon := &node.Daemon{
		Identity: identity, Store: nodeStore, Runtime: runtime,
		Inventory: t14InventorySource{nodeRef: identity.Node}, Capacity: 1,
		HeartbeatInterval: 20*time.Millisecond, InventoryInterval: time.Hour,
		OutboxPollInterval: 10*time.Millisecond, ReconnectMin: 10*time.Millisecond,
		ReconnectMax: 40*time.Millisecond,
	}
	daemonCtx, stopDaemon := context.WithCancel(ctx)
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- daemon.Run(daemonCtx) }()
	defer func() {
		stopDaemon()
		select {
		case err := <-daemonDone:
			if err != nil { t.Errorf("remote Node stopped with error: %v", err) }
		case <-time.After(2*time.Second):
			t.Error("remote Node failed to stop")
		}
	}()
	readyDeadline := time.Now().Add(5*time.Second)
	for {
		ready, err := nextManager.CommandReady(ctx, identity.Node)
		if err != nil { t.Fatal(err) }
		if ready { break }
		if time.Now().After(readyDeadline) { t.Fatal("Node did not reconnect to the restarted Server") }
		time.Sleep(20*time.Millisecond)
	}
	terminalEvents := make(chan node.NodeEvent, 2)
	nextManager.SetEventSink(func(_ context.Context, event node.NodeEvent) error {
		if event.Outcome != nil {
			select {
			case terminalEvents <- event:
			default:
			}
		}
		return nil
	})
	instance := core.HarnessInstance{ID: core.HarnessInstanceID(string(identity.Node)+"/fx"),
		Node: identity.Node, Kind: core.HarnessFX, Version: "test", Status: core.HarnessReady,
		Authentication: core.HarnessAuthentication{Authenticated: true}}
	refs := inbound
	envelope := node.WorkerEnvelope{WorkerRef: "voice-worker", TurnID: "turn-t14", AttemptID: "attempt-t14",
		OriginalUserIntent: refs, HarnessInstance: instance, LegacyWorkerTemplate: true}
	cmd := node.Command{Kind: node.CommandDispatch, Dispatch: &node.DispatchCommand{
		Metadata: core.CommandMetadata{CommandID: "cmd-t14", Node: identity.Node, WorkerRef: envelope.WorkerRef,
			TurnID: envelope.TurnID, AttemptID: envelope.AttemptID, HarnessInstanceID: instance.ID, IssuedAt: time.Now().UTC()},
		Envelope: envelope,
	}}
	dispatchCtx, cancelDispatch := context.WithTimeout(ctx, 5*time.Second)
	defer cancelDispatch()
	if err := nextManager.SendCommandAndWait(dispatchCtx, identity.Node, cmd); err != nil {
		t.Fatalf("Server could not dispatch to reconnected Node: %v", err)
	}
	select {
	case task := <-runtime.seen:
		if !strings.Contains(task, "artifact://"+original.ID) || !strings.Contains(task, "artifact://"+transcript.ID) {
			t.Fatalf("task missing references: %q", task)
		}
	case <-time.After(5*time.Second):
		t.Fatal("remote Worker could not stage and read both artifacts after reconnect")
	}
	select {
	case event := <-terminalEvents:
		if event.Outcome == nil || event.Outcome.AttemptID != "attempt-t14" ||
			event.Outcome.Status != core.OutcomeSucceeded ||
			event.Outcome.Summary != "Read original and Markdown transcript" {
			t.Fatalf("Server received wrong Worker result: %+v", event)
		}
	case <-time.After(5*time.Second):
		t.Fatal("Server did not receive the reconnected Node's terminal Worker result")
	}
}

func TestT14FailedASRRetainsOriginalAndRejectsUnauthorizedInput(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "store.sqlite"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	api, err := New(ctx, store, "bootstrap")
	if err != nil { t.Fatal(err) }
	api.AttachInternalCredential("t14-internal")
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	tr := &t14TelegramTransport{audio: []byte("audio")}
	count := 0
	adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "state"),
		OwnerChatID: 100, ASRTranscriber: t14ASR{calls: &count, fail: true}}, tr,
		&telegram.HTTPServerClient{BaseURL: server.URL, Credential: "t14-internal"})
	if err != nil { t.Fatal(err) }
	unauthorized := telegram.Update{ID: 39, Message: &telegram.Message{
		ChatID: 100, FromID: 999, MessageID: 39, Voice: &telegram.Voice{FileID: "deny", FileSize: 5},
	}}
	if err := adapter.HandleUpdate(ctx, unauthorized); err != nil { t.Fatal(err) }
	if tr.downloads != 0 { t.Fatal("unauthorized audio downloaded") }
	u := telegram.Update{ID: 40, Message: &telegram.Message{
		ChatID: 100, FromID: 100, MessageID: 40, Voice: &telegram.Voice{FileID: "fail", FileSize: 5, Duration: 2},
	}}
	if err := adapter.HandleUpdate(ctx, u); err != nil { t.Fatal(err) }
	if _, ok := adapter.Transcript("40"); ok { t.Fatal("ASR failure was recorded as successful transcript") }
	_, conversation, err := store.EnsureOwner(ctx)
	if err != nil { t.Fatal(err) }
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil { t.Fatal(err) }
	if len(entries) != 1 { t.Fatalf("failed processing must retain one original, got %d entries", len(entries)) }
	ref := strings.TrimPrefix(entries[0].Body[strings.LastIndex(entries[0].Body, "artifact://"):], "artifact://")
	artifact, content, err := store.ArtifactForPerson(ctx, conversation.PersonID, ref)
	if err != nil || !bytes.Equal(content, tr.audio) || artifact.ProcessingStatus != "failed" || artifact.TranscriptArtifactID != "" {
		t.Fatalf("ASR failure source=%+v body=%q err=%v", artifact, content, err)
	}
	if count != 1 || tr.downloads != 1 || len(tr.notices) != 1 ||
		!strings.Contains(tr.notices[0].Text, "Не удалось распознать") {
		t.Fatalf("missing processing warning: calls=%d downloads=%d notices=%v", count, tr.downloads, tr.notices)
	}
}

func TestT14ASRFailureRetryAfterTelegramNoticeOutage(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "store.sqlite"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	api, err := New(ctx, store, "bootstrap")
	if err != nil { t.Fatal(err) }
	api.AttachInternalCredential("t14-voice-retry")
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	tr := &t14TelegramTransport{audio: []byte("audio"), failNotices: 1}
	count := 0
	statePath := filepath.Join(t.TempDir(), "state.json")
	client := &telegram.HTTPServerClient{BaseURL: server.URL, Credential: "t14-voice-retry"}
	makeAdapter := func() *telegram.Adapter {
		t.Helper()
		adapter, err := telegram.New(telegram.Config{
			StatePath: statePath, OwnerChatID: 100,
			ASRTranscriber: t14ASR{calls: &count, fail: true},
		}, tr, client)
		if err != nil { t.Fatal(err) }
		return adapter
	}
	u := telegram.Update{ID: 71, Message: &telegram.Message{
		ChatID: 100, FromID: 100, MessageID: 72,
		Voice: &telegram.Voice{FileID: "voice-failure-71", Duration: 4, FileSize: 5},
	}}
	adapter := makeAdapter()
	if err := adapter.HandleUpdate(ctx, u); err == nil {
		t.Fatal("expected Telegram notification transport failure")
	}
	if count != 1 || tr.downloads != 1 { t.Fatalf("processing repeated or skipped: count=%d downloads=%d", count, tr.downloads) }
	if _, ok := adapter.Transcript("71"); ok { t.Fatal("failed ASR produced successful transcript") }
	adapter = makeAdapter()
	if err := adapter.HandleUpdate(ctx, u); err != nil { t.Fatal(err) }
	if err := adapter.HandleUpdate(ctx, u); err != nil { t.Fatal(err) }
	if count != 1 || tr.downloads != 1 || len(tr.notices) != 1 {
		t.Fatalf("ASR/download/notice repeated after restart: count=%d downloads=%d notices=%d", count, tr.downloads, len(tr.notices))
	}
	_, conversation, err := store.EnsureOwner(ctx)
	if err != nil { t.Fatal(err) }
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil { t.Fatal(err) }
	if len(entries) != 1 { t.Fatalf("retry created duplicate originals: %d", len(entries)) }
	ref := strings.TrimPrefix(entries[0].Body[strings.LastIndex(entries[0].Body, "artifact://"):], "artifact://")
	original, body, err := store.ArtifactForPerson(ctx, conversation.PersonID, ref)
	if err != nil || !bytes.Equal(body, tr.audio) || original.ProcessingStatus != "failed" || original.TranscriptArtifactID != "" {
		t.Fatalf("replayed failure changed original: %+v %q %v", original, body, err)
	}
}

func TestT14UnknownDurationAudioDocumentIsProbedBeforeASR(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	api, err := New(ctx, store, "bootstrap")
	if err != nil { t.Fatal(err) }
	api.AttachInternalCredential("t14-duration-check")
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &telegram.HTTPServerClient{BaseURL: server.URL, Credential: "t14-duration-check"}
	tr := &t14TelegramTransport{audio: []byte("audio")}
	asrCalls, probes := 0, 0
	state := filepath.Join(t.TempDir(), "duration.json")
	adapter, err := telegram.New(telegram.Config{
		StatePath: state, OwnerChatID: 100, ASRTranscriber: t14ASR{calls: &asrCalls},
		VoiceDurationProbe: func(context.Context, []byte, string) (float64, error) {
			probes++
			return 300, nil
		},
	}, tr, client)
	if err != nil { t.Fatal(err) }
	u := telegram.Update{ID: 80, Message: &telegram.Message{
		ChatID: 100, FromID: 100, MessageID: 81,
		Voice: &telegram.Voice{FileID: "unknown-duration", FileName: "long.mp3", MimeType: "audio/mpeg", FileSize: 5},
	}}
	if err := adapter.HandleUpdate(ctx, u); err != nil { t.Fatal(err) }
	if asrCalls != 0 || probes != 1 || tr.downloads != 1 {
		t.Fatalf("long audio reached ASR: asr=%d probes=%d downloads=%d", asrCalls, probes, tr.downloads)
	}
	if len(tr.notices) != 1 || !strings.Contains(tr.notices[0].Text, "лимит") { t.Fatalf("no limit warning: %v", tr.notices) }
	_, conv, err := store.EnsureOwner(ctx)
	if err != nil { t.Fatal(err) }
	entries, err := store.EntriesAfter(ctx, conv.ID, 0)
	if err != nil || len(entries) != 1 { t.Fatalf("invalid original persistence: entries=%v err=%v", entries, err) }
	ref := strings.TrimPrefix(entries[0].Body[strings.LastIndex(entries[0].Body, "artifact://"):], "artifact://")
	original, _, err := store.ArtifactForPerson(ctx, conv.PersonID, ref)
	if err != nil || original.ProcessingStatus != "failed" { t.Fatalf("long audio not marked failed: %+v %v", original, err) }
	if err := adapter.HandleUpdate(ctx, u); err != nil { t.Fatal(err) }
	if asrCalls != 0 || probes != 1 || tr.downloads != 1 || len(tr.notices) != 1 {
		t.Fatalf("long audio repeated after replay: probes=%d downloads=%d notices=%d", probes, tr.downloads, len(tr.notices))
	}
}

func TestT14AudioDocumentKnownOnlyAfterProbe(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	api, err := New(ctx, store, "bootstrap")
	if err != nil { t.Fatal(err) }
	api.AttachInternalCredential("t14-duration-positive")
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	tr := &t14TelegramTransport{audio: []byte("audio")}
	asrCalls, probes := 0, 0
	adapter, err := telegram.New(telegram.Config{
		StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100,
		ASRTranscriber: t14ASR{calls: &asrCalls},
		VoiceDurationProbe: func(_ context.Context, data []byte, name string) (float64, error) {
			probes++
			if !bytes.Equal(data, []byte("audio")) { t.Fatal("probe received incorrect audio") }
			if name == "broken.mp3" { return 0, errors.New("corrupt media") }
			if name != "short.mp3" { t.Fatalf("unexpected probed file: %q", name) }
			return 10, nil
		},
	}, tr, &telegram.HTTPServerClient{BaseURL: server.URL, Credential: "t14-duration-positive"})
	if err != nil { t.Fatal(err) }
	short := telegram.Update{ID: 90, Message: &telegram.Message{
		ChatID: 100, FromID: 100, MessageID: 91,
		Voice: &telegram.Voice{FileID: "short", FileName: "short.mp3", MimeType: "audio/mpeg", FileSize: 5},
	}}
	if err := adapter.HandleUpdate(ctx, short); err != nil { t.Fatal(err) }
	record, ok := adapter.Transcript("90")
	if !ok || record.SourceArtifactID == "" || record.TranscriptArtifactID == "" || asrCalls != 1 || probes != 1 {
		t.Fatalf("short audio not processed after duration probe: %+v calls=%d probes=%d", record, asrCalls, probes)
	}
	broken := telegram.Update{ID: 92, Message: &telegram.Message{
		ChatID: 100, FromID: 100, MessageID: 93,
		Voice: &telegram.Voice{FileID: "broken", FileName: "broken.mp3", MimeType: "audio/mpeg", FileSize: 5},
	}}
	if err := adapter.HandleUpdate(ctx, broken); err != nil { t.Fatal(err) }
	if _, ok := adapter.Transcript("92"); ok { t.Fatal("unknown/corrupt duration bypassed ASR safety gate") }
	if asrCalls != 1 || probes != 2 || tr.downloads != 2 || len(tr.notices) != 1 {
		t.Fatalf("probe failure did not fail closed: asr=%d probes=%d downloads=%d notices=%d", asrCalls, probes, tr.downloads, len(tr.notices))
	}
	if err := adapter.HandleUpdate(ctx, broken); err != nil { t.Fatal(err) }
	if asrCalls != 1 || probes != 2 || tr.downloads != 2 || len(tr.notices) != 1 {
		t.Fatal("failed duration probe processed again on replay")
	}
	_, conv, err := store.EnsureOwner(ctx)
	if err != nil { t.Fatal(err) }
	entries, err := store.EntriesAfter(ctx, conv.ID, 0)
	if err != nil { t.Fatal(err) }
	found := false
	for _, entry := range entries {
		if entry.Kind != core.EntrySystem || !strings.Contains(entry.Body, "telegram-voice") { continue }
		ref := strings.TrimPrefix(entry.Body[strings.LastIndex(entry.Body, "artifact://"):], "artifact://")
		art, _, err := store.ArtifactForPerson(ctx, conv.PersonID, ref)
		if err != nil { t.Fatal(err) }
		if art.Name == "broken.mp3" && art.ProcessingStatus == "failed" && art.TranscriptArtifactID == "" {
			found = true
		}
	}
	if !found { t.Fatal("failed duration check did not preserve the original") }
}
