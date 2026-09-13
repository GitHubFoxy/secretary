package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// RemoteSecretary is the MCP-side proxy for the server-owned Worker service.
// The capability is sent only as an Authorization header to the private server
// endpoint, never through model-visible tool arguments or prompts.
type RemoteSecretary struct {
	BaseURL    string
	Capability string
	Client     *http.Client
}

func (r RemoteSecretary) Tools() []Tool { return (Secretary{}).Tools() }

func (r RemoteSecretary) Call(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	base := strings.TrimRight(strings.TrimSpace(r.BaseURL), "/")
	if base == "" {
		return nil, errors.New("mcp: remote Secretary server URL is required")
	}
	if strings.TrimSpace(r.Capability) == "" {
		return nil, errors.New("mcp: remote Secretary capability is required")
	}
	if raw == nil {
		raw = json.RawMessage(`{}`)
	}
	body, err := json.Marshal(struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}{Name: name, Arguments: raw})
	if err != nil {
		return nil, fmt.Errorf("mcp: encode remote call: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/internal/secretary/tools/call", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp: create remote call: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+r.Capability)
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("mcp: remote call: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("mcp: remote call returned HTTP %d", response.StatusCode)
	}
	var result struct {
		Value json.RawMessage `json:"value"`
		Error string          `json:"error,omitempty"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("mcp: decode remote call: %w", err)
	}
	if result.Error != "" {
		return nil, errors.New(result.Error)
	}
	if len(result.Value) == 0 {
		return nil, errors.New("mcp: remote call returned no value")
	}
	var value any
	if err := json.Unmarshal(result.Value, &value); err != nil {
		return nil, fmt.Errorf("mcp: decode remote value: %w", err)
	}
	return value, nil
}
