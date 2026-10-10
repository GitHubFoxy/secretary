package webapi

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestBootstrapRestoresFailedSecretaryTurnFromAnotherChannel(t *testing.T) {
	ctx := context.Background()
	store, api, server, client := controlRoomTestAPI(t)
	controlRoomLogin(t, client, server.URL)
	conversation, err := store.ConversationForPerson(ctx, api.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, api.owner.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{Version: "test", Harness: "fx", Model: "m", Reasoning: "high", ProfileVersion: "test", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "private-profile-sentinel"}); err != nil {
		t.Fatal(err)
	}
	turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "Telegram-origin input")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, turn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishSecretaryTurn(ctx, turn.ID, core.SecretaryTurnFailed, "canonical Secretary context unavailable"); err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(server.URL + "/v1/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	var bootstrap map[string]any
	if err := json.Unmarshal(body, &bootstrap); err != nil {
		t.Fatal(err)
	}
	if bootstrap["secretary_turn_id"] != turn.ID {
		t.Fatalf("failed turn unavailable after reload: %v", bootstrap["secretary_turn_id"])
	}
	if strings.Contains(string(body), "private-profile-sentinel") || strings.Contains(string(body), "context_snapshot") {
		t.Fatal("bootstrap exposed private context")
	}
	response, err = client.Get(server.URL + "/v1/secretary/turns/" + turn.ID + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if !strings.Contains(string(body), "canonical Secretary context unavailable") || !strings.Contains(string(body), "secretary.turn.finished") {
		t.Fatal("failed terminal absent from public replay")
	}
	foreign, foreignConversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foreignIdentity, err := store.EnsureSecretaryIdentity(ctx, foreign.ID, foreignConversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnqueueSecretaryTurn(ctx, foreignIdentity.ID, "foreign channel input"); err != nil {
		t.Fatal(err)
	}
	response, err = client.Get(server.URL + "/v1/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(&bootstrap); err != nil {
		t.Fatal(err)
	}
	if bootstrap["secretary_turn_id"] != turn.ID {
		t.Fatal("foreign turn replaced scoped owner turn")
	}
}
