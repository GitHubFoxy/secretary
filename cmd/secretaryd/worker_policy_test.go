package main

import (
	"github.com/beruseruko/secretary/internal/config"
	"github.com/beruseruko/secretary/internal/core"
	"testing"
)

func TestProductionWorkerPolicyKeepsModelAndReasoningSeparateFromSecretary(t *testing.T) {
	c := config.Config{Secretary: config.SecretaryPolicy{Harness: "opencode", Model: "openai/gpt-6.1-sol", Reasoning: "low"}, WorkerPolicy: config.WorkerPolicy{DefaultHarness: "opencode", Model: "openai/gpt-6-luna", Reasoning: "xhigh", PreferredHarnesses: []string{"opencode"}}}
	policy := configuredWorkerPolicy(c)
	if policy.DefaultHarness != core.HarnessOpenCode || policy.ModelID != "openai/gpt-6-luna" || policy.Reasoning != "xhigh" {
		t.Fatalf("production Worker policy=%#v", policy)
	}
	c.WorkerPolicy.Model = "default"
	c.WorkerPolicy.Reasoning = "default"
	policy = configuredWorkerPolicy(c)
	if policy.ModelID != "" || policy.Reasoning != "" {
		t.Fatal("legacy defaults became literal observed pins")
	}
}
