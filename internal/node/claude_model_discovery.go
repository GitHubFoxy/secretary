package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/nativeexec"
)

func (r ExecCommandRunner) ObserveClaudeModels(ctx context.Context, binary string) ([]core.ObservedModelID, error) {
	cmd, err := nativeexec.CommandContext(ctx, binary,
		"--print", "--input-format", "stream-json", "--output-format", "stream-json",
		"--verbose", "--tools", "", "--strict-mcp-config",
		"--no-session-persistence",
	)
	if err != nil {
		return nil, errors.New("Claude native model discovery unavailable")
	}
	cmd.Env = make([]string, 0, len(os.Environ())+1)
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=") {
			cmd.Env = append(cmd.Env, variable)
		}
	}
	cmd.Env = append(cmd.Env, "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1")
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.New("Claude native model discovery unavailable")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.New("Claude native model discovery unavailable")
	}
	if err := prepareClaudeProbeProcess(cmd); err != nil {
		return nil, errors.New("Claude native model discovery unavailable")
	}
	if err := cmd.Start(); err != nil {
		return nil, errors.New("Claude native model discovery unavailable")
	}
	cleanup, err := attachClaudeProbeProcess(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, errors.New("Claude native model discovery unavailable")
	}
	defer func() {
		cleanup()
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	request := map[string]any{"type": "control_request", "request_id": "secretary-model-inventory", "request": map[string]any{"subtype": "initialize"}}
	if err := json.NewEncoder(stdin).Encode(request); err != nil {
		_ = stdin.Close()
		return nil, errors.New("Claude native model discovery unavailable")
	}
	type observation struct {
		models []core.ObservedModelID
		err error
	}
	results := make(chan observation, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			models, matched, err := parseClaudeInitializeModels(scanner.Bytes())
			if matched {
				results <- observation{models: models, err: err}
				return
			}
		}
		results <- observation{err: errors.New("Claude initialization returned no models")}
	}()
	select {
	case <-ctx.Done():
		return nil, errors.New("Claude native model discovery timed out or canceled")
	case result := <-results:
		return result.models, result.err
	}
}

func parseClaudeInitializeModels(line []byte) ([]core.ObservedModelID, bool, error) {
	var message struct {
		Type string `json:"type"`
		Response struct {
			RequestID string `json:"request_id"`
			Subtype string `json:"subtype"`
			Response struct {
				Models []struct {
					Value string `json:"value"`
					ResolvedModel string `json:"resolvedModel"`
				} `json:"models"`
			} `json:"response"`
		} `json:"response"`
	}
	if json.Unmarshal(line, &message) != nil || message.Type != "control_response" || message.Response.RequestID != "secretary-model-inventory" {
		return nil, false, nil
	}
	if message.Response.Subtype != "success" {
		return nil, true, errors.New("Claude native initialization rejected model discovery")
	}
	seen := map[core.ObservedModelID]bool{}
	var models []core.ObservedModelID
	for _, row := range message.Response.Response.Models {
		for _, value := range []string{row.ResolvedModel, row.Value} {
			if row.ResolvedModel != "" && value == row.Value {
				continue
			}
			value = strings.TrimSpace(value)
			if value == "" || isModelAlias(value) {
				continue
			}
			id := core.ObservedModelID(value)
			if !seen[id] {
				seen[id] = true
				models = append(models, id)
			}
		}
	}
	if len(models) == 0 {
		return nil, true, errors.New("Claude native initialization reported no concrete models")
	}
	return models, true, nil
}
