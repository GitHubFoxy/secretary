package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/telegram"
)

type fakeTelegramPairer struct{}

func (fakeTelegramPairer) CreatePairing(bot string) (telegram.Pairing, error) {
	return telegram.Pairing{Code: "one-time", DeepLink: "https://t.me/" + bot + "?start=one-time", ExpiresAt: time.Now().UTC().Add(time.Minute)}, nil
}

func TestTelegramPairingRequiresOwnerSessionAndNeverReturnsCredential(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "telegram.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachTelegramPairer(fakeTelegramPairer{})
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	unauthorized, err := http.Post(server.URL+"/v1/telegram/pairing", "application/json", strings.NewReader(`{"bot_username":"secretary_bot"}`))
	if err != nil {
		t.Fatal(err)
	}
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized pairing status=%d", unauthorized.StatusCode)
	}
	unauthorized.Body.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	loginResponse, err := client.Post(server.URL+"/v1/web/session", "application/json", strings.NewReader(`{"bootstrap_token":"bootstrap"}`))
	if err != nil {
		t.Fatal(err)
	}
	loginResponse.Body.Close()
	pairingResponse, err := client.Post(server.URL+"/v1/telegram/pairing", "application/json", strings.NewReader(`{"bot_username":"secretary_bot"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer pairingResponse.Body.Close()
	if pairingResponse.StatusCode != http.StatusCreated {
		t.Fatalf("owner pairing status=%d", pairingResponse.StatusCode)
	}
	var result map[string]any
	if err := json.NewDecoder(pairingResponse.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if _, exists := result["credential"]; exists {
		t.Fatalf("pairing response contains credential: %#v", result)
	}
}

func TestTelegramInternalCredentialCanSubmitMessageWithoutClientCredential(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "telegram-internal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachInternalCredential("telegram-internal-secret")
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", strings.NewReader(`{"external_message_id":"telegram-update-unauthorized","body":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer wrong-secret")
	request.Header.Set("Idempotency-Key", "telegram:telegram-update-unauthorized")
	unauthorized, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong internal credential status=%d", unauthorized.StatusCode)
	}

	request, err = http.NewRequest(http.MethodPost, server.URL+"/v1/messages", strings.NewReader(`{"external_message_id":"telegram-update-1","body":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer telegram-internal-secret")
	request.Header.Set("Idempotency-Key", "telegram:telegram-update-1")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("internal credential message status=%d", response.StatusCode)
	}
}

func TestTelegramPairingResponseShapeHasNoCredential(t *testing.T) {
	pairing := telegram.Pairing{Code: "code", DeepLink: "https://t.me/bot?start=code", Credential: "server-secret"}
	encoded, err := json.Marshal(pairing)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "server-secret") || strings.Contains(string(encoded), "credential") {
		t.Fatalf("credential leaked in pairing response: %s", encoded)
	}
}
