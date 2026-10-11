package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type timedVoiceTransport struct {
	*fakeTransport
	failStage string
}

func (v *timedVoiceTransport) GetFile(context.Context, string) (TelegramFile, error) {
	time.Sleep(2 * time.Millisecond)
	if v.failStage == "get" {
		return TelegramFile{}, errors.New("private-download-secret")
	}
	return TelegramFile{FilePath: "private-file-id"}, nil
}
func (v *timedVoiceTransport) DownloadFile(context.Context, string, int64) ([]byte, error) {
	time.Sleep(2 * time.Millisecond)
	if v.failStage == "download" {
		return nil, errors.New("private-download-secret")
	}
	return []byte("private-audio"), nil
}

type timingInspectionServer struct {
	t              *testing.T
	adapter        *Adapter
	expectedStatus string
	calls          int
}

func (s *timingInspectionServer) SendMessage(_ context.Context, m InboundMessage) error {
	s.calls++
	tr, ok := s.adapter.Transcript(m.ExternalMessageID)
	if !ok || tr.Timing == nil || tr.Timing.FormatterStatus != s.expectedStatus {
		s.t.Fatalf("metrics not persisted before dispatch: %+v", tr)
	}
	// Dispatch is deliberately blocked after inspecting persisted measurements;
	// changing a stored value after this call would reveal a defer timing bug.
	before := *tr.Timing
	time.Sleep(2 * time.Millisecond)
	after, _ := s.adapter.Transcript(m.ExternalMessageID)
	if *after.Timing != before {
		s.t.Fatal("agent dispatch included in processing timing")
	}
	return nil
}
func (s *timingInspectionServer) SendWorkerMessage(ctx context.Context, m WorkerMessage) error {
	return s.SendMessage(ctx, InboundMessage{ExternalMessageID: m.ExternalMessageID})
}

func TestVoiceTimingsPersistBeforeDispatchAndReplay(t *testing.T) {
	for _, status := range []string{"disabled", "succeeded", "failed", "unavailable"} {
		t.Run(status, func(t *testing.T) {
			var output bytes.Buffer
			old := log.Writer()
			log.SetOutput(&output)
			defer log.SetOutput(old)
			path := filepath.Join(t.TempDir(), "state.json")
			transport := &timedVoiceTransport{fakeTransport: &fakeTransport{}}
			server := &timingInspectionServer{t: t, expectedStatus: status}
			asrCalls, formatterCalls := 0, 0
			asr := transcribeFunc(func(context.Context, []byte) (string, error) {
				asrCalls++
				time.Sleep(2 * time.Millisecond)
				return "private transcript", nil
			})
			a, err := New(Config{StatePath: path, OwnerChatID: 100, ASRTranscriber: asr}, transport, server)
			if err != nil {
				t.Fatal(err)
			}
			server.adapter = a
			if status != "disabled" {
				a.config.ASR.Formatter.Enabled = true
			}
			if status == "succeeded" || status == "failed" {
				a.formatter = formatFunc(func(context.Context, string) (string, error) {
					formatterCalls++
					time.Sleep(2 * time.Millisecond)
					if status == "failed" {
						return "", errors.New("private-formatter-secret")
					}
					return "private transcript.", nil
				})
			}
			if err := a.HandleUpdate(context.Background(), voiceUpdate(1, 0)); err != nil {
				t.Fatal(err)
			}
			transcript, _ := a.Transcript("1")
			timing := *transcript.Timing
			if timing.DownloadMS == 0 || timing.ASRMS == 0 || timing.ProcessingMS < timing.DownloadMS+timing.ASRMS+timing.FormatterMS {
				t.Fatalf("timing=%+v", timing)
			}
			if status == "succeeded" || status == "failed" {
				if timing.FormatterMS == 0 {
					t.Fatal("invoked formatter not timed")
				}
			} else if timing.FormatterMS != 0 {
				t.Fatal("uninvoked formatter has elapsed time")
			}
			want := fmt.Sprintf("download_ms=%d asr_ms=%d formatter_ms=%d processing_ms=%d formatter_status=%s", timing.DownloadMS, timing.ASRMS, timing.FormatterMS, timing.ProcessingMS, timing.FormatterStatus)
			if !strings.Contains(output.String(), want) {
				t.Fatalf("log and persisted timing disagree: %s", output.String())
			}
			for _, private := range []string{"private transcript", "private-audio", "private-file-id", "private-formatter-secret"} {
				if strings.Contains(output.String(), private) {
					t.Fatalf("log leaked %q", private)
				}
			}
			beforeCalls, beforeFormatter := asrCalls, formatterCalls
			beforeLog := output.String()
			a, err = New(Config{StatePath: path, OwnerChatID: 100, ASRTranscriber: asr}, transport, server)
			if err != nil {
				t.Fatal(err)
			}
			server.adapter = a
			if err := a.HandleUpdate(context.Background(), voiceUpdate(1, 0)); err != nil {
				t.Fatal(err)
			}
			replay, _ := a.Transcript("1")
			if replay.Timing == nil || *replay.Timing != timing || asrCalls != beforeCalls || formatterCalls != beforeFormatter || server.calls != 1 || output.String() != beforeLog {
				t.Fatal("replay changed timing or repeated processing")
			}
		})
	}
}

func TestVoiceFailureElapsedLogsArePrivate(t *testing.T) {
	for _, stage := range []string{"get", "download", "asr"} {
		t.Run(stage, func(t *testing.T) {
			var output bytes.Buffer
			old := log.Writer()
			log.SetOutput(&output)
			defer log.SetOutput(old)
			tr := &timedVoiceTransport{fakeTransport: &fakeTransport{}, failStage: stage}
			a, err := New(Config{StatePath: filepath.Join(t.TempDir(), "state"), OwnerChatID: 100, ASRTranscriber: transcribeFunc(func(context.Context, []byte) (string, error) {
				time.Sleep(2 * time.Millisecond)
				return "", errors.New("private-asr-secret")
			})}, tr, &fakeServer{})
			if err != nil {
				t.Fatal(err)
			}
			a.config.ASR.Formatter.Enabled = true
			if err := a.HandleUpdate(context.Background(), voiceUpdate(1, 0)); err != nil {
				t.Fatal(err)
			}
			expectedStage := stage
			if stage == "get" {
				expectedStage = "download"
			}
			logs := output.String()
			if !strings.Contains(logs, "stage="+expectedStage+" status=failed") || !strings.Contains(logs, "processing_ms=") {
				t.Fatalf("missing failed stage timing: %s", logs)
			}
			if !strings.Contains(logs, "formatter_status=not_run") {
				t.Fatal("uninvoked formatter misreported as unavailable")
			}
			if strings.Contains(logs, "processing_ms=0 ") {
				t.Fatal("failure elapsed time not measured")
			}
			if strings.Contains(logs, "private") {
				t.Fatalf("failure log leaked details: %s", logs)
			}
			if _, ok := a.Transcript("1"); ok {
				t.Fatal("failed ASR created transcript")
			}
		})
	}
}
func TestVoiceTranscriptLegacyTimingAbsent(t *testing.T) {
	var tr VoiceTranscript
	if err := json.Unmarshal([]byte(`{"raw":"old","delivered":"raw"}`), &tr); err != nil || tr.Timing != nil || tr.Raw != "old" {
		t.Fatalf("legacy=%+v error=%v", tr, err)
	}
}
