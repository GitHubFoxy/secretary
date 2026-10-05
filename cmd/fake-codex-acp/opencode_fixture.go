package main

import (
	"encoding/json"
	"os"
	"strings"
)

// Packaging tests use this protocol double, not a native acceptance result.
// Echo only choices present in the generated managed config. Native OpenCode
// prompt/permission/history proof lives in opt-in internal/node tests.
func openCodeFixtureSelection(params json.RawMessage) (any, bool) {
	var request struct {
		ConfigID string `json:"configId"`
		Value    string `json:"value"`
	}
	var config struct {
		DefaultAgent string `json:"default_agent"`
		Model        string `json:"model"`
		Agents       map[string]struct {
			Description string `json:"description"`
		} `json:"agents"`
		Providers map[string]struct {
			Models map[string]struct {
				Variants []struct {
					ID string `json:"id"`
				} `json:"variants"`
			} `json:"models"`
		} `json:"providers"`
	}
	data, err := os.ReadFile(os.Getenv("OPENCODE_CONFIG"))
	if err != nil || json.Unmarshal(data, &config) != nil || json.Unmarshal(params, &request) != nil {
		return nil, false
	}
	agent, exists := config.Agents[config.DefaultAgent]
	if !exists {
		return nil, false
	}
	effort := "default"
	if request.ConfigID == "mode" {
		if request.Value != config.DefaultAgent {
			return nil, false
		}
	} else if request.ConfigID == "model" {
		provider, id, _ := strings.Cut(config.Model, "/")
		if request.Value != config.Model {
			matched := false
			for _, variant := range config.Providers[provider].Models[id].Variants {
				if request.Value == config.Model+"/"+variant.ID {
					effort = variant.ID
					matched = true
				}
			}
			if !matched {
				return nil, false
			}
		}
	} else {
		return nil, false
	}
	return map[string]any{"configOptions": []any{
		map[string]any{"id": "mode", "currentValue": config.DefaultAgent, "options": []map[string]string{{"value": config.DefaultAgent, "description": agent.Description}}},
		map[string]string{"id": "model", "currentValue": config.Model},
		map[string]string{"id": "effort", "currentValue": effort},
	}}, true
}
