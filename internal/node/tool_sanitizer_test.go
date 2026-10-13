package node

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeFailureMessage_Redaction(t *testing.T) {
	input := "Failed with Bearer eyJhbGciOi... and sk-proj-1234567890abcdef. Also ghp_1234567890abcdef and xoxb-123456789012-abcdef."
	sanitized := SanitizeFailureMessage(input)

	if strings.Contains(sanitized, "eyJhbGciOi") {
		t.Fatalf("bearer token leaked: %s", sanitized)
	}
	if strings.Contains(sanitized, "sk-proj-1234567890abcdef") {
		t.Fatalf("sk token leaked: %s", sanitized)
	}
	if strings.Contains(sanitized, "ghp_1234567890abcdef") {
		t.Fatalf("ghp token leaked: %s", sanitized)
	}
	if strings.Contains(sanitized, "xoxb-123456789012-abcdef") {
		t.Fatalf("xoxb token leaked: %s", sanitized)
	}
	if !strings.Contains(sanitized, "[redacted]") {
		t.Fatalf("expected [redacted] marker in output: %s", sanitized)
	}
}

func TestSanitizeFailureMessage_RedactsSensitiveCLIArgumentsAfterJSONDetection(t *testing.T) {
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
		`{"message":"provider failed: --password review-private-password [429]"}`,
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
			t.Errorf("safe status was lost: %q", got)
		}
	}
}

func TestSanitizeFailureMessage_RedactsQuotedJSONKeys(t *testing.T) {
	input := `{"password":"private-value","access_token":"opaque-value","session_id":"native-value","safe":"kept"}`
	got := SanitizeFailureMessage(input)
	for _, secret := range []string{"private-value", "opaque-value", "native-value"} {
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

func TestSanitizeFailureMessage_RedactsUnicodeEscapedJSONKeys(t *testing.T) {
	cases := []struct {
		input    string
		wantSafe string
	}{
		{input: `{"access\u005ftoken":"opaque-value","safe":"kept"}`, wantSafe: "kept"},
		{input: `{"access\u005ftoken":"opaque-value","safe":"Bearer ordinary"}`, wantSafe: "Bearer [redacted]"},
		{input: `{"access\u005ftoken":"opaque-value","safe":"Authorization: Bearer ordinary (status 401)"}`, wantSafe: "Authorization: [redacted] (status 401)"},
	}
	for _, test := range cases {
		got := SanitizeFailureMessage(test.input)
		if strings.Contains(got, "opaque-value") {
			t.Fatalf("Unicode-escaped JSON key leaked its value: %s", got)
		}
		var decoded map[string]string
		if err := json.Unmarshal([]byte(got), &decoded); err != nil {
			t.Fatalf("sanitized JSON is invalid: %v (%s)", err, got)
		}
		if decoded["access_token"] != "[redacted]" {
			t.Fatalf("Unicode-escaped key was not sanitized: %#v", decoded)
		}
		if decoded["safe"] != test.wantSafe {
			t.Fatalf("safe field=%q, want %q", decoded["safe"], test.wantSafe)
		}
	}
}

func TestSanitizeFailureMessage_RedactsNestedJSONString(t *testing.T) {
	innerJSON := `{"access\u005ftoken":"opaque-value"}`
	quotedInnerJSON, err := json.Marshal(innerJSON)
	if err != nil {
		t.Fatal(err)
	}
	for _, safe := range []string{innerJSON, string(quotedInnerJSON)} {
		input, err := json.Marshal(map[string]string{"safe": safe})
		if err != nil {
			t.Fatal(err)
		}
		got := SanitizeFailureMessage(string(input))
		if strings.Contains(got, "opaque-value") {
			t.Fatalf("nested JSON secret leaked: %s", got)
		}
		var outer map[string]string
		if err := json.Unmarshal([]byte(got), &outer); err != nil {
			t.Fatalf("sanitized outer JSON is invalid: %v (%s)", err, got)
		}
		nestedJSON := outer["safe"]
		if strings.HasPrefix(strings.TrimSpace(nestedJSON), `"`) {
			if err := json.Unmarshal([]byte(nestedJSON), &nestedJSON); err != nil {
				t.Fatalf("sanitized quoted nested JSON is invalid: %v (%q)", err, outer["safe"])
			}
		}
		var inner map[string]string
		if err := json.Unmarshal([]byte(nestedJSON), &inner); err != nil {
			t.Fatalf("sanitized nested JSON is invalid: %v (%q)", err, nestedJSON)
		}
		if inner["access_token"] != "[redacted]" {
			t.Fatalf("nested escaped key was not sanitized: %#v", inner)
		}
	}
}

func TestSanitizeFailureMessage_RedactsJSONAfterTextPrefix(t *testing.T) {
	input := `provider failed: {"access\u005ftoken":"opaque-value","safe":"Bearer ordinary"}`
	got := SanitizeFailureMessage(input)
	if strings.Contains(got, "opaque-value") {
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
	if payload["access_token"] != "[redacted]" || payload["safe"] != "Bearer [redacted]" {
		t.Fatalf("embedded JSON fields=%#v", payload)
	}
}

func TestSanitizeFailureMessage_RedactsJSONStringAfterPrefixBeforeURLRemoval(t *testing.T) {
	input := `provider failed: "{\"access_token\":\"review-private-credential\",\"safe\":\"https://untrusted.example/path\"}"`
	got := SanitizeFailureMessage(input)
	if strings.Contains(got, "review-private-credential") || strings.Contains(got, "untrusted.example") {
		t.Fatalf("nested JSON secret/URL leaked: %q", got)
	}
	start := strings.IndexByte(got, '"')
	if start < 0 {
		t.Fatalf("embedded JSON string was lost: %q", got)
	}
	var embedded string
	if err := json.NewDecoder(strings.NewReader(got[start:])).Decode(&embedded); err != nil {
		t.Fatalf("embedded JSON string became invalid: %v (%q)", err, got)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(embedded), &payload); err != nil {
		t.Fatalf("sanitized embedded JSON is invalid: %v (%q)", err, embedded)
	}
	if payload["access_token"] != "[redacted]" || payload["safe"] != "" {
		t.Fatalf("sanitized embedded payload=%#v", payload)
	}
}

func TestSanitizeFailureMessage_RedactsJSONAfterTextPrefixInsideJSONField(t *testing.T) {
	inner := `provider failed: {"access\u005ftoken":"round9-private-value","safe":"Bearer ordinary"}`
	raw, err := json.Marshal(map[string]string{"safe": inner})
	if err != nil {
		t.Fatal(err)
	}
	got := SanitizeFailureMessage(string(raw))
	if strings.Contains(got, "round9-private-value") {
		t.Fatalf("embedded JSON secret leaked: %q", got)
	}
	var outer map[string]string
	if err := json.Unmarshal([]byte(got), &outer); err != nil {
		t.Fatalf("sanitized outer JSON is invalid: %v (%q)", err, got)
	}
	fragmentStart := strings.IndexByte(outer["safe"], '{')
	if fragmentStart < 0 {
		t.Fatalf("embedded JSON was lost: %q", outer["safe"])
	}
	var embedded map[string]string
	if err := json.Unmarshal([]byte(outer["safe"][fragmentStart:]), &embedded); err != nil {
		t.Fatalf("sanitized embedded JSON is invalid: %v (%q)", err, outer["safe"])
	}
	if embedded["access_token"] != "[redacted]" || embedded["safe"] != "Bearer [redacted]" {
		t.Fatalf("embedded JSON fields=%#v", embedded)
	}
}

func TestSanitizeFailureMessage_URLAllowlist(t *testing.T) {
	allowed := "Visit https://chatgpt.com/explore/pro or https://platform.openai.com/usage"
	sanitizedAllowed := SanitizeFailureMessage(allowed)
	if !strings.Contains(sanitizedAllowed, "https://chatgpt.com/explore/pro") {
		t.Fatalf("allowed URL removed: %s", sanitizedAllowed)
	}
	if !strings.Contains(sanitizedAllowed, "https://platform.openai.com/usage") {
		t.Fatalf("allowed URL removed: %s", sanitizedAllowed)
	}

	disallowed := "Exfiltration link https://evil.com/leak/data?token=123"
	sanitizedDisallowed := SanitizeFailureMessage(disallowed)
	if strings.Contains(sanitizedDisallowed, "evil.com") {
		t.Fatalf("unauthorized URL leaked: %s", sanitizedDisallowed)
	}
}

func TestSanitizeFailureMessage_RuneLimit(t *testing.T) {
	longMsg := strings.Repeat("абвг", 300)
	sanitized := SanitizeFailureMessage(longMsg)
	runes := []rune(sanitized)
	if len(runes) > 1000 {
		t.Fatalf("expected at most 1000 runes, got %d", len(runes))
	}
	if !strings.HasSuffix(sanitized, "...") {
		t.Fatalf("expected '...' suffix for truncated message")
	}
}

func TestSanitizeFailureMessage_URLQueryAndUserInfoRedaction(t *testing.T) {
	input := "Check https://chatgpt.com/private?api%5Fkey=plainvalue and https://chatgpt.com/test?token=secret123 and https://user:secretpass@chatgpt.com/settings"
	sanitized := SanitizeFailureMessage(input)
	if strings.Contains(sanitized, "plainvalue") {
		t.Fatalf("sensitive query value leaked: %s", sanitized)
	}
	if strings.Contains(sanitized, "secret123") {
		t.Fatalf("sensitive token leaked: %s", sanitized)
	}
	if strings.Contains(sanitized, "secretpass") {
		t.Fatalf("userinfo leaked: %s", sanitized)
	}
}

func TestSanitizeFailureMessage_ArbitraryPathAndFragmentDisallowed(t *testing.T) {
	input := "Leaked secret at https://chatgpt.com/private/plainsecret?code=plainsecret#plainsecret"
	sanitized := SanitizeFailureMessage(input)
	if strings.Contains(sanitized, "plainsecret") {
		t.Fatalf("arbitrary path/query/fragment leaked secret: %s", sanitized)
	}

	withFragment := "Visit https://chatgpt.com/explore/pro#fragment?secret=123"
	sanitizedHelp := SanitizeFailureMessage(withFragment)
	if !strings.Contains(sanitizedHelp, "https://chatgpt.com/explore/pro") {
		t.Fatalf("expected fixed help URL preserved: %s", sanitizedHelp)
	}
	if strings.Contains(sanitizedHelp, "fragment") || strings.Contains(sanitizedHelp, "secret") {
		t.Fatalf("expected fragment and query stripped from help URL: %s", sanitizedHelp)
	}
}

func TestSanitizeFailureMessage_UppercaseAndMixedCaseURLs(t *testing.T) {
	disallowedUpper := "Check HTTPS://evil.example/private/plainvalue for info"
	if res := SanitizeFailureMessage(disallowedUpper); strings.Contains(res, "evil.example") || strings.Contains(res, "plainvalue") {
		t.Fatalf("disallowed uppercase URL leaked: %s", res)
	}

	disallowedMixed := "Leak at HttPs://evil.example/private/plainsecret#plainsecret"
	if res := SanitizeFailureMessage(disallowedMixed); strings.Contains(res, "evil.example") || strings.Contains(res, "plainsecret") {
		t.Fatalf("disallowed mixed-case URL leaked: %s", res)
	}

	allowedUpper := "See HTTPS://CHATGPT.COM/SETTINGS/USAGE#fragment?token=secret for details"
	resUpper := SanitizeFailureMessage(allowedUpper)
	if !strings.Contains(resUpper, "https://chatgpt.com/settings/usage") {
		t.Fatalf("expected canonical allowed URL, got: %s", resUpper)
	}
	if strings.Contains(resUpper, "fragment") || strings.Contains(resUpper, "secret") {
		t.Fatalf("fragment or query leaked from allowed URL: %s", resUpper)
	}
}

func TestSanitizeFailureMessage_AuthorizationBasicAndBearerCredentials(t *testing.T) {
	basicInput := "Request failed: Authorization: Basic dXNlcjpwYXNz (status 401)"
	basicRes := SanitizeFailureMessage(basicInput)
	if strings.Contains(basicRes, "dXNlcjpwYXNz") {
		t.Fatalf("Basic credentials leaked: %s", basicRes)
	}
	if !strings.Contains(basicRes, "Authorization: [redacted]") {
		t.Fatalf("expected Authorization: [redacted], got: %s", basicRes)
	}

	standaloneBasic := "Failed with credentials Basic dXNlcjpwYXNz"
	standaloneRes := SanitizeFailureMessage(standaloneBasic)
	if strings.Contains(standaloneRes, "dXNlcjpwYXNz") {
		t.Fatalf("standalone Basic credentials leaked: %s", standaloneRes)
	}
	if !strings.Contains(standaloneRes, "Basic [redacted]") {
		t.Fatalf("expected Basic [redacted], got: %s", standaloneRes)
	}

	bearerInput := "HTTP Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"
	bearerRes := SanitizeFailureMessage(bearerInput)
	if strings.Contains(bearerRes, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9") {
		t.Fatalf("Bearer credentials leaked: %s", bearerRes)
	}
	if !strings.Contains(bearerRes, "Authorization: [redacted]") {
		t.Fatalf("expected Authorization: [redacted], got: %s", bearerRes)
	}
}

func TestSanitizeProviderCode_AllowlistAndLimit(t *testing.T) {
	if code := SanitizeProviderCode("usage_limit_exceeded"); code != "usage_limit_exceeded" {
		t.Fatalf("expected usage_limit_exceeded, got %q", code)
	}
	if code := SanitizeProviderCode("UsageLimitExceeded"); code != "usage_limit_exceeded" {
		t.Fatalf("expected canonical usage_limit_exceeded, got %q", code)
	}
	if code := SanitizeProviderCode("auth_required"); code != "auth_required" {
		t.Fatalf("expected auth_required, got %q", code)
	}
	if code := SanitizeProviderCode("invalid_api_key"); code != "invalid_api_key" {
		t.Fatalf("expected invalid_api_key, got %q", code)
	}

	if code := SanitizeProviderCode("sk-private-secret-12345"); code != "" {
		t.Fatalf("expected unknown/secret code excluded, got %q", code)
	}
	if code := SanitizeProviderCode("custom_unknown_code"); code != "" {
		t.Fatalf("expected unknown code excluded, got %q", code)
	}

	longCode := strings.Repeat("x", 1001)
	if code := SanitizeProviderCode(longCode); code != "" {
		t.Fatalf("expected oversize code excluded, got %q", code)
	}
}

func TestSanitizeFailureMessage_MalformedUnknownURLsAndNonHTTPSchemes(t *testing.T) {
	urlWithComma := "Exfiltration link https://evil.example/private/first,plainsecret#privatevalue"
	resComma := SanitizeFailureMessage(urlWithComma)
	if strings.Contains(resComma, "plainsecret") || strings.Contains(resComma, "privatevalue") || strings.Contains(resComma, "evil.example") {
		t.Fatalf("URL with comma/fragment leaked sensitive data: %s", resComma)
	}

	urlAlphaComma := "Exfiltration link https://evil.example/private/alpha,plainsecret#plainsecret"
	resAlpha := SanitizeFailureMessage(urlAlphaComma)
	if strings.Contains(resAlpha, "plainsecret") || strings.Contains(resAlpha, "evil.example") {
		t.Fatalf("URL with alpha comma leaked sensitive data: %s", resAlpha)
	}

	ftpURL := "Download at ftp://evil.example/private/plainsecret"
	resFTP := SanitizeFailureMessage(ftpURL)
	if strings.Contains(resFTP, "plainsecret") || strings.Contains(resFTP, "evil.example") {
		t.Fatalf("FTP URL leaked: %s", resFTP)
	}

	wsURL := "Socket connection ws://evil.example/socket/secrettoken"
	resWS := SanitizeFailureMessage(wsURL)
	if strings.Contains(resWS, "secrettoken") || strings.Contains(resWS, "evil.example") {
		t.Fatalf("WS URL leaked: %s", resWS)
	}

	gitURL := "Repository at git://github.com/secretowner/secretrepo.git"
	resGit := SanitizeFailureMessage(gitURL)
	if strings.Contains(resGit, "secretowner") || strings.Contains(resGit, "secretrepo") {
		t.Fatalf("Git URL leaked: %s", resGit)
	}

	allowedWithComma := "Please visit https://chatgpt.com/settings/usage, then check billing."
	resAllowed := SanitizeFailureMessage(allowedWithComma)
	if !strings.Contains(resAllowed, "https://chatgpt.com/settings/usage,") {
		t.Fatalf("expected allowed URL with trailing comma preserved, got: %s", resAllowed)
	}
}

func TestSanitizeFailureMessage_DigestAuthorizationCredentials(t *testing.T) {
	digestMulti := `Request failed: Authorization: Digest username="user", realm="test", nonce="plainnonce", uri="/", response="plainresponse", qop=auth (status 401)`
	resMulti := SanitizeFailureMessage(digestMulti)
	if strings.Contains(resMulti, "plainnonce") || strings.Contains(resMulti, "plainresponse") || strings.Contains(resMulti, "user") {
		t.Fatalf("Digest credentials leaked from multi-param header: %s", resMulti)
	}
	if !strings.Contains(resMulti, "Authorization: [redacted] (status 401)") {
		t.Fatalf("expected Authorization: [redacted] (status 401), got: %s", resMulti)
	}

	digestSimple := `HTTP error: Authorization: Digest username="owner", response="plaincredential"`
	resSimple := SanitizeFailureMessage(digestSimple)
	if strings.Contains(resSimple, "plaincredential") || strings.Contains(resSimple, "owner") {
		t.Fatalf("Digest credentials leaked from simple header: %s", resSimple)
	}
	if !strings.Contains(resSimple, "Authorization: [redacted]") {
		t.Fatalf("expected Authorization: [redacted], got: %s", resSimple)
	}

	standaloneDigest := `Failed with credentials Digest username="owner", response="plaincredential"`
	resStandalone := SanitizeFailureMessage(standaloneDigest)
	if strings.Contains(resStandalone, "plaincredential") || strings.Contains(resStandalone, "owner") {
		t.Fatalf("standalone Digest credentials leaked: %s", resStandalone)
	}
	if !strings.Contains(resStandalone, "Digest [redacted]") {
		t.Fatalf("expected Digest [redacted], got: %s", resStandalone)
	}

	digestWithJSONLikeUsername := `Authorization: Digest username="[123]", response="digest-response-secret", nonce="nonce-secret" (status 401)`
	got := SanitizeFailureMessage(digestWithJSONLikeUsername)
	for _, secret := range []string{"[123]", "digest-response-secret", "nonce-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("Digest credential %q leaked: %s", secret, got)
		}
	}
}

func TestSanitizeFailureMessage_RedactsOpaqueAuthorizationValues(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		secrets []string
	}{
		{
			name:    "digest spacing around parameter equals",
			input:   `Authorization: Digest username="owner", response = "plaincredential"`,
			secrets: []string{"owner", "plaincredential"},
		},
		{
			name:    "unknown authorization scheme",
			input:   `HTTP Authorization: Custom realm="private", payload = "plaincredential" (status 403)`,
			secrets: []string{"private", "plaincredential"},
		},
		{
			name:    "folded authorization value",
			input:   "Authorization: Digest username=owner,\r\n response = plaincredential\r\nnext line",
			secrets: []string{"owner", "plaincredential"},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := SanitizeFailureMessage(test.input)
			for _, secret := range test.secrets {
				if strings.Contains(got, secret) {
					t.Fatalf("authorization credential %q leaked: %q", secret, got)
				}
			}
			if !strings.Contains(got, "[redacted]") {
				t.Fatalf("missing redaction marker: %q", got)
			}
		})
	}
	if got := SanitizeFailureMessage(`Authorization: Bearer opaque-token (status 401)`); got != "Authorization: [redacted] (status 401)" {
		t.Fatalf("Bearer field=%q", got)
	}
}

func TestToolArgumentPreviewRedactsWholeAuthorizationHeader(t *testing.T) {
	arguments := []byte(`{"command":"Authorization: Digest username=\"owner\", response = \"plaincredential\""}`)
	got := ToolArgumentPreview("bash", arguments, "")
	if got != "Authorization: [redacted]" {
		t.Fatalf("preview=%q; want complete redaction marker", got)
	}
	if strings.Contains(got, "owner") || strings.Contains(got, "plaincredential") {
		t.Fatalf("preview leaked authorization credentials: %q", got)
	}
}
