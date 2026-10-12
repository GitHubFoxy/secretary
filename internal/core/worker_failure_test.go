package core

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReconcileWorkerCommandReceiptSanitizesNodeFailureMetadata(t *testing.T) {
	ctx := context.Background()
	store, _, worker, turn, attempt, _ := createReceiptFixture(t, ctx, filepath.Join(t.TempDir(), "secretary.db"), "receipt-sanitizer", "macbook")
	defer store.Close()
	command, _, err := store.ClaimWorkerCommand(ctx, "steering", "steering:sanitize", worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	receipt := WorkerCommandReceipt{
		CommandID: command.ID, Kind: "steering", TurnID: turn.ID, AttemptID: attempt.ID,
		State:           WorkerCommandReceiptDenied,
		ErrorCode:       `provider failed: "{\"access_token\":\"review-private-credential\",\"safe\":\"retry at 4:37 PM\"}"; provider failed: {'access_token': 'opaque-private-credential', 'safe': 'retry at 4:37 PM'}; password="prefix[123]private-password"; token=[123]private-token; token=prefix{"x":1}private-object-token; Authorization: Digest username="receipt-owner", response = "plaincredential"`,
		ErrorMessage:    `{"password":"private-value","access_token":"opaque-value","session_id":"native-value","worker_id":"worker-value","embedded":"\"{\\\"access\\\\u005ftoken\\\":\\\"nested-opaque-value\\\"}\"","embedded_text":"provider failed: {\"access\\u005ftoken\":\"prefixed-opaque-value\",\"safe\":\"Bearer ordinary\"}","digest":"Authorization: Digest username=\"[123]\", response=\"digest-response-secret\", nonce=\"nonce-secret\" (status 401)","basic":"Failed with credentials Basic dXNlcjpwYXNz"}`,
		FailureCategory: "sk-private-secret", ProviderCode: "sk-private-secret",
		Technical: `{"password":"technical-private-value","access_token":"technical-opaque-value","session_id":"technical-native-value"}` + strings.Repeat("界", 1101),
	}
	if _, err := store.ReconcileWorkerCommandReceipt(ctx, "macbook", receipt); err != nil {
		t.Fatal(err)
	}
	saved, err := store.WorkerCommandReceiptOutcome(ctx, command.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.FailureCategory != FailureCategoryInternalError || saved.ProviderCode != "" {
		t.Fatalf("unsafe category/provider code persisted: %#v", saved)
	}
	var rawCategory, rawProviderCode, rawErrorCode, rawErrorMessage, rawTechnical string
	if err := store.db.QueryRowContext(ctx, `SELECT failure_category, provider_code, error_code, error_message, technical FROM phase4_worker_command_outcomes WHERE command_id = ?`, command.ID).Scan(&rawCategory, &rawProviderCode, &rawErrorCode, &rawErrorMessage, &rawTechnical); err != nil {
		t.Fatal(err)
	}
	if rawCategory != string(FailureCategoryInternalError) || rawProviderCode != "" {
		t.Fatalf("unsafe category/provider code reached persistence: category=%q provider=%q", rawCategory, rawProviderCode)
	}
	for name, value := range map[string]string{"error_code": saved.ErrorCode, "error_message": saved.ErrorMessage, "technical": saved.Technical} {
		if utf8.RuneCountInString(value) > 1000 {
			t.Errorf("%s has %d runes", name, utf8.RuneCountInString(value))
		}
		for _, secret := range []string{"private-secret", "private-value", "opaque-value", "nested-opaque-value", "prefixed-opaque-value", "native-value", "worker-value", "receipt-owner", "plaincredential", "dXNlcjpwYXNz", "digest-response-secret", "nonce-secret", "private-password", "private-token", "private-object-token", "opaque-private-credential", "review-private-credential"} {
			if strings.Contains(value, secret) {
				t.Errorf("%s leaked Node text: %q", name, value)
			}
		}
	}
	for name, value := range map[string]string{"error_code": rawErrorCode, "error_message": rawErrorMessage, "technical": rawTechnical} {
		leaked := false
		for _, secret := range []string{"private-secret", "private-value", "opaque-value", "nested-opaque-value", "prefixed-opaque-value", "native-value", "worker-value", "receipt-owner", "plaincredential", "dXNlcjpwYXNz", "digest-response-secret", "nonce-secret", "private-password", "private-token", "private-object-token", "opaque-private-credential", "review-private-credential"} {
			leaked = leaked || strings.Contains(value, secret)
		}
		if utf8.RuneCountInString(value) > 1000 || leaked {
			t.Errorf("unsafe %s reached persistence: runes=%d value=%q", name, utf8.RuneCountInString(value), value)
		}
	}
	var safeReceiptJSON map[string]string
	if err := json.Unmarshal([]byte(saved.ErrorMessage), &safeReceiptJSON); err != nil {
		t.Fatalf("sanitized JSON receipt is no longer valid JSON: %v (%q)", err, saved.ErrorMessage)
	}
	embeddedText := safeReceiptJSON["embedded_text"]
	fragmentStart := strings.IndexByte(embeddedText, '{')
	if fragmentStart < 0 {
		t.Fatalf("embedded JSON receipt fragment was lost: %q", embeddedText)
	}
	var safeEmbeddedText map[string]string
	if err := json.Unmarshal([]byte(embeddedText[fragmentStart:]), &safeEmbeddedText); err != nil {
		t.Fatalf("embedded JSON receipt fragment is invalid: %v (%q)", err, embeddedText)
	}
	if safeEmbeddedText["access_token"] != "[redacted]" {
		t.Fatalf("embedded receipt JSON key was not sanitized: %#v", safeEmbeddedText)
	}
	if safeEmbeddedText["safe"] != "Bearer [redacted]" {
		t.Fatalf("embedded receipt text was not sanitized: %#v", safeEmbeddedText)
	}
	if safeReceiptJSON["digest"] != "Authorization: [redacted] (status 401)" {
		t.Fatalf("Digest receipt was not fully sanitized: %q", safeReceiptJSON["digest"])
	}
	var safeEmbedded map[string]string
	nestedReceiptJSON := safeReceiptJSON["embedded"]
	if strings.HasPrefix(strings.TrimSpace(nestedReceiptJSON), `"`) {
		if err := json.Unmarshal([]byte(nestedReceiptJSON), &nestedReceiptJSON); err != nil {
			t.Fatalf("quoted nested receipt JSON is invalid: %v (%q)", err, safeReceiptJSON["embedded"])
		}
	}
	if err := json.Unmarshal([]byte(nestedReceiptJSON), &safeEmbedded); err != nil {
		t.Fatalf("nested receipt JSON is invalid: %v (%q)", err, nestedReceiptJSON)
	}
	if safeEmbedded["access_token"] != "[redacted]" {
		t.Fatalf("nested receipt key was not sanitized: %#v", safeEmbedded)
	}
}

func TestSanitizeFailureMessageRedactsSecretsAndUnknownURLs(t *testing.T) {
	got := SanitizeFailureMessage("Bearer private-token sk-private-secret https://attacker.invalid/path?token=value ftp://chatgpt.com/settings/usage try again at 4:37 PM")
	for _, secret := range []string{"private-token", "private-secret", "attacker.invalid", "token=value", "ftp://"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized failure leaked %q: %q", secret, got)
		}
	}
	if !strings.Contains(got, "try again at 4:37 PM") {
		t.Fatalf("safe reset explanation was lost: %q", got)
	}
	if got := SanitizeFailureCategory("sk-private-secret"); got != FailureCategoryInternalError {
		t.Fatalf("unknown category sanitized to %q", got)
	}
}

func TestSanitizeFailureMessageRedactsStandaloneBasicCredentials(t *testing.T) {
	got := SanitizeFailureMessage("Failed with credentials Basic dXNlcjpwYXNz")
	if got != "Failed with credentials Basic [redacted]" {
		t.Fatalf("standalone Basic credentials=%q", got)
	}
}

func TestSanitizeFailureMessageRedactsSensitiveCLIArguments(t *testing.T) {
	for _, input := range []string{
		"provider failed: --api-key review-private-credential [429]",
		"provider failed: --api-key prefix,review-private-credential [429]",
		`provider failed: --api-key prefix" review-private-credential" [429]`,
		`provider failed: --api-key "prefix[429]review-private-credential"`,
		`provider failed: --api-key "prefix\"[429]review-private-credential"`,
		"curl --user alice:review-private-password https://attacker.invalid/path [429]",
		"curl --user alice:prefix,review-private-password [429]",
		`curl --user alice:prefix" review-private-password" [429]`,
		`curl --user "alice:prefix[429]review-private-password"`,
		`curl --user "alice:prefix\"[429]review-private-password"`,
		`{"message":"provider failed: --password review-private-password [429]","safe":"kept"}`,
	} {
		got := SanitizeFailureMessage(input)
		for _, secret := range []string{"review-private-credential", "review-private-password", "alice:"} {
			if strings.Contains(got, secret) {
				t.Errorf("CLI credential %q leaked from %q: %q", secret, input, got)
			}
		}
		if !strings.Contains(got, "[redacted]") {
			t.Errorf("CLI redaction marker is missing: %q", got)
		}
		if strings.Contains(input, " [429]") && !strings.Contains(got, "429") {
			t.Errorf("safe status was lost: input=%q output=%q", input, got)
		}
	}
}

func TestSanitizeFailureMessageDoesNotSplitSecretValuesAtJSONLikeArrays(t *testing.T) {
	for _, input := range []string{
		`password="prefix[123]private-password"`,
		`token=[123]private-token`,
		`token=prefix{"x":1}private-object-token`,
	} {
		got := SanitizeFailureMessage(input)
		for _, secret := range []string{"prefix", "private-password", "private-token", "private-object-token"} {
			if strings.Contains(got, secret) {
				t.Fatalf("secret %q leaked from %q: %q", secret, input, got)
			}
		}
	}
}

func TestSanitizeFailureMessageRedactsSingleQuotedSecretAssignments(t *testing.T) {
	input := `provider failed: {'access_token': 'opaque-private-credential', 'safe': 'retry at 4:37 PM'}`
	got := SanitizeFailureMessage(input)
	if strings.Contains(got, "opaque-private-credential") {
		t.Fatalf("single-quoted secret leaked: %q", got)
	}
	if !strings.Contains(got, "retry at 4:37 PM") {
		t.Fatalf("safe reset explanation was lost: %q", got)
	}
}

func TestSanitizeFailureMessageRedactsEmbeddedJSONStringAfterTextPrefix(t *testing.T) {
	input := `provider failed: "{\"access_token\":\"review-private-credential\",\"safe\":\"retry at 4:37 PM\"}"`
	got := SanitizeFailureMessage(input)
	if strings.Contains(got, "review-private-credential") {
		t.Fatalf("embedded JSON string secret leaked: %q", got)
	}
	start := strings.IndexByte(got, '"')
	if start < 0 {
		t.Fatalf("embedded JSON string was lost: %q", got)
	}
	var embedded string
	if err := json.NewDecoder(strings.NewReader(got[start:])).Decode(&embedded); err != nil {
		t.Fatalf("sanitized embedded string is invalid: %v (%q)", err, got)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(embedded), &payload); err != nil {
		t.Fatalf("sanitized embedded JSON is invalid: %v (%q)", err, embedded)
	}
	if payload["access_token"] != "[redacted]" || payload["safe"] != "retry at 4:37 PM" {
		t.Fatalf("sanitized embedded payload=%#v", payload)
	}
}

func TestSanitizeFailureMessageRedactsDigestBeforeEmbeddedJSONScan(t *testing.T) {
	input := `Authorization: Digest username="[123]", response="digest-response-secret", nonce="nonce-secret" (status 401)`
	got := SanitizeFailureMessage(input)
	for _, secret := range []string{"[123]", "digest-response-secret", "nonce-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("Digest credential %q leaked: %q", secret, got)
		}
	}
	if !strings.Contains(got, "Authorization: [redacted] (status 401)") {
		t.Fatalf("status suffix was not preserved after Digest redaction: %q", got)
	}
}

func TestSanitizeFailureMessageRedactsEmbeddedJSONAfterTextPrefix(t *testing.T) {
	input := `provider failed: {"access\u005ftoken":"round8-private-value","safe":"kept"}`
	got := SanitizeFailureMessage(input)
	if strings.Contains(got, "round8-private-value") {
		t.Fatalf("embedded JSON secret leaked: %q", got)
	}
	start := strings.IndexByte(got, '{')
	if start < 0 {
		t.Fatalf("embedded JSON was lost: %q", got)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(got[start:]), &payload); err != nil {
		t.Fatalf("sanitized embedded JSON is invalid: %v (%q)", err, got)
	}
	if payload["access_token"] != "[redacted]" || payload["safe"] != "kept" {
		t.Fatalf("embedded JSON fields=%#v", payload)
	}
}

func TestSanitizeFailureMessageRedactsQuotedJSONKeys(t *testing.T) {
	input := `{"password":"private-value","api_token":"api-token-value","access_token":"opaque-value","session_id":"native-value","worker_id":"worker-value","authorization":"Bearer private-token","safe":"kept"}`
	got := SanitizeFailureMessage(input)
	for _, secret := range []string{"private-value", "api-token-value", "opaque-value", "native-value", "worker-value", "private-token"} {
		if strings.Contains(got, secret) {
			t.Fatalf("quoted JSON secret %q leaked: %s", secret, got)
		}
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("sanitized JSON is invalid: %v (%s)", err, got)
	}
	if decoded["safe"] != "kept" {
		t.Fatalf("safe field was lost: %s", got)
	}
}

func TestSanitizeFailureMessageRedactsCompleteDigestAuthorization(t *testing.T) {
	for _, input := range []string{
		`Authorization: Digest username="owner", response = "plaincredential" (status 401)`,
		"Authorization: Digest username=owner,\n\tresponse=plaincredential\nretry after 4:37 PM",
		`Digest username="owner", response="plaincredential"`,
	} {
		got := SanitizeFailureMessage(input)
		for _, secret := range []string{"owner", "plaincredential"} {
			if strings.Contains(got, secret) {
				t.Fatalf("Digest credential %q leaked from %q: %q", secret, input, got)
			}
		}
		if strings.Contains(input, "status 401") && !strings.Contains(got, "(status 401)") {
			t.Fatalf("safe status suffix was lost: %q", got)
		}
		if strings.Contains(input, "retry after") && !strings.Contains(got, "retry after 4:37 PM") {
			t.Fatalf("safe retry explanation was lost: %q", got)
		}
	}

	input := `{"message":"Authorization: Digest username=\"owner\", response=\"plaincredential\"","safe":"kept"}`
	got := SanitizeFailureMessage(input)
	var payload map[string]string
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("sanitized JSON is invalid: %v (%q)", err, got)
	}
	if strings.Contains(got, "owner") || strings.Contains(got, "plaincredential") || payload["safe"] != "kept" {
		t.Fatalf("JSON Authorization value was not safely preserved: %q", got)
	}
}
