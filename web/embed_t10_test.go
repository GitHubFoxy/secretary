package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Test the real Go embedded production Handler, not Vite's source/dev server.
// The checked-in index can import the separate ES module; the generated build
// can instead compile that module directly into app.js.
func TestEmbeddedProductionWebImageAttachmentEntrypoint(t *testing.T) {
	handler := Handler()
	get := func(path string) string {
		t.Helper()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("embedded GET %s returned %d", path, recorder.Code)
		}
		if recorder.Body.Len() == 0 {
			t.Fatalf("embedded GET %s returned an empty body", path)
		}
		return recorder.Body.String()
	}
	html := get("/")
	app := get("/app.js")
	uploader := get("/artifact-upload.js")
	if !strings.Contains(html, `type="module" crossorigin src="/app.js"`) &&
		!strings.Contains(html, `type="module" src="/app.js"`) {
		t.Fatal("production index does not load main Web app.js module")
	}
	if strings.Contains(html, `<script defer src="/artifact-upload.js"`) {
		t.Fatal("production index loads artifact-upload ES module as a classic script")
	}
	direct := strings.Contains(html, `type="module" src="/artifact-upload.js"`)
	compiled := strings.Contains(app, "/v1/artifacts?name=") &&
		strings.Contains(app, "secretaryArtifactInput") &&
		strings.Contains(app, "Idempotency-Key")
	if !direct && !compiled {
		t.Fatal("production embedded HTML cannot activate artifact upload: no module or compiled controller")
	}
	if !strings.Contains(uploader, "export function installUpload") ||
		!strings.Contains(uploader, "new MutationObserver(installArtifactUpload)") {
		t.Fatal("embedded uploader is missing ES module export or dynamic composer installation")
	}
}
