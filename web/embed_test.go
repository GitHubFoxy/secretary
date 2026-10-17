package web

import (
	"bytes"
	"encoding/json"
	"image/png"
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


func TestInstallablePWAUsesSameWebAndNeverCachesPrivateContent(t *testing.T) {
	handler := Handler()
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://secretary"+path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, recorder.Code)
		}
		return recorder
	}
	index := get("/")
	if !strings.Contains(index.Body.String(), `href="/manifest.webmanifest"`) ||
		!strings.Contains(index.Body.String(), `src="/pwa.js"`) ||
		!strings.Contains(index.Body.String(), `href="/icons/icon-192.png"`) {
		t.Fatal("installed app entrypoint is not the existing Personal Conversation")
	}
	if index.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("entrypoint must not be cached")
	}
	manifest := get("/manifest.webmanifest")
	if contentType := manifest.Header().Get("Content-Type"); contentType != "application/manifest+json" {
		t.Fatalf("manifest content-type=%q", contentType)
	}
	if manifest.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("manifest should refresh on deployment")
	}
	var config struct {
		Name string `json:"name"`
		ShortName string `json:"short_name"`
		StartURL string `json:"start_url"`
		Scope string `json:"scope"`
		Display string `json:"display"`
		Icons []struct {
			Src string `json:"src"`
			Sizes string `json:"sizes"`
			Type string `json:"type"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(manifest.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if config.Name == "" || config.ShortName == "" || config.StartURL != "/" || config.Scope != "/" || config.Display != "standalone" {
		t.Fatalf("invalid install metadata: %+v", config)
	}
	icons := map[string]bool{}
	for _, icon := range config.Icons {
		if icon.Type != "image/png" {
			t.Fatalf("icon type=%q", icon.Type)
		}
		image := get(icon.Src)
		if contentType := image.Header().Get("Content-Type"); contentType != "image/png" {
			t.Fatalf("icon content-type=%q", contentType)
		}
		decoded, err := png.DecodeConfig(bytes.NewReader(image.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		switch icon.Sizes {
		case "192x192":
			want = 192
		case "512x512":
			want = 512
		default:
			t.Fatalf("unexpected icon size=%q", icon.Sizes)
		}
		if decoded.Width != want || decoded.Height != want {
			t.Fatalf("icon %s: dimensions %dx%d", icon.Src, decoded.Width, decoded.Height)
		}
		icons[icon.Sizes] = true
	}
	if !icons["192x192"] || !icons["512x512"] {
		t.Fatalf("required PWA icon dimensions missing: %v", icons)
	}
	workerUI := get("/app.js").Body.String()
	if !strings.Contains(workerUI, "/v1/workers") || !strings.Contains(workerUI, "/v1/web/session") {
		t.Fatal("PWA did not reuse the shipped Web session and Worker observer")
	}
	sw := get("/sw.js")
	if sw.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("service worker must be refreshed on deployment")
	}
	if !strings.Contains(sw.Body.String(), "fetch(event.request)") || strings.Contains(sw.Body.String(), "caches.open") {
		t.Fatal("service worker must be network-only and not cache private data")
	}
	pwa := get("/pwa.js").Body.String()
	if !strings.Contains(pwa, "navigator.serviceWorker.register('/sw.js'") ||
		!strings.Contains(pwa, "Secretary Server is unreachable") ||
		!strings.Contains(pwa, "/v1/health") {
		t.Fatal("PWA install or network-error indicator is missing")
	}
	if status := get("/control-room/app.js").Code; status != http.StatusOK {
		t.Fatalf("control assets status=%d", status)
	}
}
