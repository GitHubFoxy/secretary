package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesEmbeddedClient(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://secretary/", nil)
	recorder := httptest.NewRecorder()
	Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Personal Conversation") {
		t.Fatal("index was not served")
	}
}

func TestControlHandlerServesOnlyEmbeddedControlAssets(t *testing.T) {
	for _, path := range []string{"/control-room", "/control-room/", "/control-room/app.js", "/control-room/app.css"} {
		request := httptest.NewRequest(http.MethodGet, "http://secretary"+path, nil)
		recorder := httptest.NewRecorder()
		ControlHandler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("path=%s status=%d", path, recorder.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "http://secretary/control-room/src/App.svelte", nil)
	recorder := httptest.NewRecorder()
	ControlHandler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("source asset status=%d", recorder.Code)
	}
}
