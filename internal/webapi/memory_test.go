package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestOwnerMemoryAPIWriteReadConflictAndInvalidFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := core.Open(ctx, filepath.Join(dir, "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachUserDocument(filepath.Join(dir, "user.md"))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	anonymous, err := http.Get(server.URL + "/v1/memory/memory")
	if err != nil {
		t.Fatal(err)
	}
	anonymous.Body.Close()
	if anonymous.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized memory GET: %d", anonymous.StatusCode)
	}
	jar, err := cookiejar.New(nil)
	if err != nil { t.Fatal(err) }
	client := server.Client()
	client.Jar = jar
	// Session auth and the existing single-owner web login.
	login(t, client, server.URL)
	get := func(kind string) (core.UserDocument, int) {
		t.Helper()
		response, err := client.Get(server.URL + "/v1/memory/" + kind)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var document core.UserDocument
		if response.StatusCode == http.StatusOK {
			if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
				t.Fatal(err)
			}
		}
		return document, response.StatusCode
	}
	original, status := get("memory")
	if status != http.StatusOK || original.Revision != 1 || original.Content != "" {
		t.Fatalf("fresh document=%+v status=%d", original, status)
	}
	put := func(kind string, revision int64, content, key string) (core.UserDocument, int) {
		t.Helper()
		encoded, _ := json.Marshal(map[string]any{"content": content, "expected_revision": revision, "idempotency_key": key})
		request, err := http.NewRequest(http.MethodPut, server.URL+"/v1/memory/"+kind, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", key)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var document core.UserDocument
		if response.StatusCode == http.StatusOK {
			if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
				t.Fatal(err)
			}
		}
		return document, response.StatusCode
	}
	saved, status := put("memory", 1, "# Facts\n\n- Likes Go\n", "memory-1")
	if status != http.StatusOK || saved.Revision != 2 {
		t.Fatalf("memory PUT result=%+v status=%d", saved, status)
	}
	replay, status := put("memory", 1, "# Facts\n\n- Likes Go\n", "memory-1")
	if status != http.StatusOK || replay.Revision != saved.Revision {
		t.Fatalf("idempotent retry result=%+v status=%d", replay, status)
	}
	_, status = put("memory", 1, "stale edit", "memory-2")
	if status != http.StatusConflict {
		t.Fatalf("stale write status=%d", status)
	}
	prefs, status := get("preferences")
	if status != http.StatusOK {
		t.Fatalf("preferences GET status=%d", status)
	}
	_, status = put("preferences", prefs.Revision, "- Prefers Russian\n", "prefs-1")
	if status != http.StatusOK {
		t.Fatalf("preferences PUT status=%d", status)
	}
	actual, status := get("memory")
	if status != http.StatusOK || actual.Content != saved.Content {
		t.Fatalf("read-after-write=%+v status=%d", actual, status)
	}
	if err := os.WriteFile(actual.Path, []byte("corrupt\x00text"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, status = get("memory")
	if status != http.StatusInternalServerError {
		t.Fatalf("corrupt memory should surface as server error, got %d", status)
	}
}
