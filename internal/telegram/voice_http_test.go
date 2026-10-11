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
