package node

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrOpenCodeCredentialUnavailable = errors.New("harness probe: OpenCode stored credential is unavailable")

var openCodeAuthenticationArgs = []string{"auth", "list", "--format", "json", "--standalone"}

// CheckOpenCodeAuthentication checks stored provider metadata in one explicitly
// selected native data home. The command runs without the background service or
// ambient provider credentials; raw native output is never returned to callers.
func CheckOpenCodeAuthentication(ctx context.Context, command, dataHome string) error {
	if strings.TrimSpace(command) == "" || strings.TrimSpace(dataHome) == "" {
		return ErrOpenCodeCredentialUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	stepCtx, cancel := context.WithTimeout(ctx, defaultProbeStepTimeout)
	defer cancel()
	result, err := (ExecCommandRunner{OpenCodeDataHome: dataHome}).Run(stepCtx, command, openCodeAuthenticationArgs...)
	if err != nil || result.ExitCode != 0 || !openCodeAuthOutputHasStoredCredential(result.Stdout) {
		return ErrOpenCodeCredentialUnavailable
	}
	return nil
}

func openCodeAuthOutputHasStoredCredential(output string) bool {
	if !utf8.ValidString(output) || !json.Valid([]byte(output)) {
		return false
	}
	var records []json.RawMessage
	if err := json.Unmarshal([]byte(output), &records); err != nil || records == nil {
		return false
	}
	storedCredential := false
	for _, rawRecord := range records {
		var record map[string]json.RawMessage
		if err := json.Unmarshal(rawRecord, &record); err != nil || record == nil {
			return false
		}
		if !nonEmptyJSONString(record["id"]) || !nonEmptyJSONString(record["name"]) {
			return false
		}
		rawConnections, ok := record["connections"]
		if !ok {
			return false
		}
		var connections []json.RawMessage
		if err := json.Unmarshal(rawConnections, &connections); err != nil || connections == nil {
			return false
		}
		for _, rawConnection := range connections {
			var connection map[string]json.RawMessage
			if err := json.Unmarshal(rawConnection, &connection); err != nil || connection == nil || !nonEmptyJSONString(connection["type"]) {
				return false
			}
			var kind string
			if err := json.Unmarshal(connection["type"], &kind); err != nil {
				return false
			}
			storedCredential = storedCredential || kind == "credential"
		}
	}
	return storedCredential
}

func nonEmptyJSONString(raw json.RawMessage) bool {
	var value string
	return len(raw) > 0 && json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != ""
}
