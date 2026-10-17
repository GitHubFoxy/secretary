package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBotVoiceDecodeAndBoundedDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/botTOKEN/getUpdates":
			w.Write([]byte(`{"ok":true,"result":[{"update_id":1,"message":{"message_id":2,"chat":{"id":100},"from":{"id":100},"voice":{"file_id":"f","file_unique_id":"u","duration":12,"file_size":5}}}]}`))
		case "/botTOKEN/getFile":
			w.Write([]byte(`{"ok":true,"result":{"file_path":"voice.ogg","file_size":5}}`))
		case "/file/botTOKEN/voice.ogg":
			w.Write([]byte("audio"))
		default:
			t.Errorf("path=%s", r.URL.Path)
		}
	}))
	defer srv.Close()
	tr := &BotAPITransport{BaseURL: srv.URL, BotToken: "TOKEN"}
	u, err := tr.GetUpdates(context.Background(), 1, time.Second)
	if err != nil || len(u) != 1 || u[0].Message.Voice.Duration != 12 {
		t.Fatalf("updates=%+v %v", u, err)
	}
	f, err := tr.GetFile(context.Background(), "f")
	if err != nil {
		t.Fatal(err)
	}
	b, err := tr.DownloadFile(context.Background(), f.FilePath, 5)
	if err != nil || string(b) != "audio" {
		t.Fatalf("audio=%q %v", b, err)
	}
	_, err = tr.DownloadFile(context.Background(), f.FilePath, 4)
	if err == nil || strings.Contains(err.Error(), "TOKEN") {
		t.Fatalf("limit/secret=%v", err)
	}
}

func TestTelegramAudioAndSupportedAudioDocumentDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN/getUpdates" {
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[
			{"update_id":2,"message":{"message_id":20,"chat":{"id":100},"from":{"id":100},"caption":"Summarize this","audio":{"file_id":"song","file_unique_id":"u2","file_name":"note.mp3","duration":10,"file_size":5,"mime_type":"audio/mpeg"}}},
			{"update_id":3,"message":{"message_id":21,"chat":{"id":100},"from":{"id":100},"document":{"file_id":"doc","file_unique_id":"u3","file_name":"recording.m4a","file_size":5,"mime_type":"audio/mp4"}}},
			{"update_id":4,"message":{"message_id":22,"chat":{"id":100},"from":{"id":100},"document":{"file_id":"pdf","file_name":"paper.pdf","mime_type":"application/pdf"}}}
		]}`))
	}))
	defer srv.Close()
	tr := &BotAPITransport{BaseURL: srv.URL, BotToken: "TOKEN"}
	updates, err := tr.GetUpdates(context.Background(), 1, time.Second)
	if err != nil || len(updates) != 3 {
		t.Fatalf("updates: %+v %v", updates, err)
	}
	if updates[0].Message.Voice == nil || updates[0].Message.Voice.FileName != "note.mp3" ||
		updates[0].Message.Caption != "Summarize this" || updates[0].Message.Voice.MimeType != "audio/mpeg" {
		t.Fatalf("audio message: %+v", updates[0].Message)
	}
	if updates[1].Message.Voice == nil || updates[1].Message.Voice.FileName != "recording.m4a" {
		t.Fatalf("audio document: %+v", updates[1].Message)
	}
	if updates[2].Message.Voice != nil {
		t.Fatalf("non-audio document was ingested: %+v", updates[2].Message)
	}
}
