package webapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
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
}

func (t *t14TelegramTransport) GetUpdates(context.Context, int64, time.Duration) ([]telegram.Update, error) { return nil, nil }
func (t *t14TelegramTransport) SendMessage(_ context.Context, message telegram.OutgoingMessage) error {
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
	for _, entry := range entries {
		if strings.Contains(entry.Body, "artifact://"+original.ID) { nOriginal++ }
		if strings.Contains(entry.Body, "artifact://"+transcript.ID) { nTranscript++ }
	}
	if nOriginal != 1 || nTranscript != 1 { t.Fatalf("duplicate Artifact announcements: %d %d", nOriginal, nTranscript) }
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
	identity.ConnectURL = strings.Replace(identity.ConnectURL, server.URL, nextServer.URL, 1)
	reconnectedOriginal, err := identity.FetchArtifact(ctx, original.ID)
	if err != nil || !bytes.Equal(reconnectedOriginal.Data, originalBytes) { t.Fatalf("reconnected original: %v", err) }
	reconnectedTranscript, err := identity.FetchArtifact(ctx, transcript.ID)
	if err != nil || !bytes.Equal(reconnectedTranscript.Data, transcriptBytes) { t.Fatalf("reconnected transcript: %v", err) }
	runtime := &t14VoiceWorker{original: originalBytes, transcript: transcriptBytes, seen: make(chan string, 1)}
	nodeStore, err := node.OpenLocalStore(filepath.Join(t.TempDir(), "remote-state.json"))
	if err != nil { t.Fatal(err) }
	defer nodeStore.Close()
	execution := node.NewExecutionNode(identity.Node, runtime, nodeStore)
	execution.SetArtifactFetcher(identity.FetchArtifact)
	instance := core.HarnessInstance{ID: core.HarnessInstanceID(string(identity.Node)+"/fx"),
		Node: identity.Node, Kind: core.HarnessFX, Version: "test", Status: core.HarnessReady,
		Authentication: core.HarnessAuthentication{Authenticated: true}}
	refs := "Transcribe and inspect artifact://"+transcript.ID+" and artifact://"+original.ID
	envelope := node.WorkerEnvelope{WorkerRef: "voice-worker", TurnID: "turn-t14", AttemptID: "attempt-t14",
		OriginalUserIntent: refs, HarnessInstance: instance, LegacyWorkerTemplate: true}
	cmd := node.Command{Kind: node.CommandDispatch, Dispatch: &node.DispatchCommand{
		Metadata: core.CommandMetadata{CommandID: "cmd-t14", Node: identity.Node, WorkerRef: envelope.WorkerRef,
			TurnID: envelope.TurnID, AttemptID: envelope.AttemptID, HarnessInstanceID: instance.ID, IssuedAt: time.Now().UTC()},
		Envelope: envelope,
	}}
	outcome, err := execution.HandleCommand(ctx, cmd)
	if err != nil || outcome.State != node.CommandAccepted { t.Fatalf("remote dispatch: %+v %v", outcome, err) }
	select {
	case task := <-runtime.seen:
		if !strings.Contains(task, "artifact://"+original.ID) || !strings.Contains(task, "artifact://"+transcript.ID) {
			t.Fatalf("task missing references: %q", task)
		}
	case <-time.After(2*time.Second):
		t.Fatal("remote Worker could not stage and read both artifacts after reconnect")
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
		ChatID: 100, FromID: 100, MessageID: 40, Voice: &telegram.Voice{FileID: "fail", FileSize: 5},
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
