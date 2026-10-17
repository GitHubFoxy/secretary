package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

// Exercises the actual owner session cookie across a complete HTTP server and
// SQLite close/reopen boundary. No bootstrap re-login occurs after restart.
func TestMemoryOwnerAPIAfterServerSQLiteRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := filepath.Join(dir, "secretary.db")
	userPath := filepath.Join(dir, "user.md")
	legacyText := "# Existing owner data\nNever remove this document.\n"
	store, err := core.Open(ctx, db)
	if err != nil { t.Fatal(err) }
	legacy, err := store.SaveUserDocument(ctx, userPath, legacyText)
	if err != nil { t.Fatal(err) }
	api, err := New(ctx, store, "bootstrap")
	if err != nil { t.Fatal(err) }
	api.AttachUserDocument(userPath)
	srv := httptest.NewServer(api.Handler())
	jar, err := cookiejar.New(nil)
	if err != nil { t.Fatal(err) }
	client := &http.Client{Jar: jar}
	login(t, client, srv.URL)
	get := func(path string, out any) int {
		t.Helper()
		resp, err := client.Get(srv.URL + path)
		if err != nil { t.Fatal(err) }
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK && out != nil {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil { t.Fatal(err) }
		}
		return resp.StatusCode
	}
	put := func(kind, value, key string) core.UserDocument {
		t.Helper()
		var document core.UserDocument
		if status := get("/v1/memory/"+kind, &document); status != http.StatusOK {
			t.Fatalf("read %s returned %d", kind, status)
		}
		payload, _ := json.Marshal(map[string]any{"content": value, "expected_revision": document.Revision, "idempotency_key": key})
		req, err := http.NewRequest(http.MethodPut, srv.URL+"/v1/memory/"+kind, bytes.NewReader(payload))
		if err != nil { t.Fatal(err) }
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		resp, err := client.Do(req)
		if err != nil { t.Fatal(err) }
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PUT %s returned %d", kind, resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(&document); err != nil { t.Fatal(err) }
		return document
	}
	facts := put("memory", "- Has a cat\n", "durable-facts-1")
	preferences := put("preferences", "- Respond in Russian\n", "durable-prefs-1")
	var beforeUser core.UserDocument
	if get("/v1/user", &beforeUser) != http.StatusOK || beforeUser.Content != legacyText {
		t.Fatalf("legacy user data before restart: %+v", beforeUser)
	}
	srv.Close()
	if err := store.Close(); err != nil { t.Fatal(err) }
	store, err = core.Open(ctx, db)
	if err != nil { t.Fatal(err) }
	defer store.Close()
	api, err = New(ctx, store, "bootstrap")
	if err != nil { t.Fatal(err) }
	api.AttachUserDocument(userPath)
	srv = httptest.NewServer(api.Handler())
	defer srv.Close()
	// Same client, same cookie: both files and legacy data must be readable.
	for _, tc := range []struct {kind string; want core.UserDocument}{
		{"memory", facts}, {"preferences", preferences},
	} {
		var found core.UserDocument
		if status := get("/v1/memory/"+tc.kind, &found); status != http.StatusOK {
			t.Fatalf("stored owner session rejected after restart: %s status=%d", tc.kind, status)
		}
		if found.Content != tc.want.Content || found.Revision != tc.want.Revision {
			t.Fatalf("%s not durable: got=%+v expected=%+v", tc.kind, found, tc.want)
		}
		disk, err := os.ReadFile(found.Path)
		if err != nil || string(disk) != found.Content {
			t.Fatalf("missing Markdown file for %s: %q %v", tc.kind, disk, err)
		}
	}
	var afterUser core.UserDocument
	if get("/v1/user", &afterUser) != http.StatusOK || afterUser.Content != legacy.Content || afterUser.Revision != legacy.Revision {
		t.Fatalf("legacy user document changed: %+v vs %+v", afterUser, legacy)
	}
	disk, err := os.ReadFile(userPath)
	if err != nil || string(disk) != legacyText {
		t.Fatalf("legacy user.md bytes were destroyed: %q err=%v", disk, err)
	}
	// Existing authenticated session remains writable after restart.
	updated := put("preferences", "- Respond in Russian\n- Prefer Go\n", "durable-prefs-2")
	if updated.Revision != preferences.Revision+1 {
		t.Fatalf("revision after restart=%d", updated.Revision)
	}
}
