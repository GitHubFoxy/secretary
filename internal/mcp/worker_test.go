package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestWorkerHandlerExposesOnlySpawnAndChecksScope(t *testing.T) {
	handler := WorkerHandler{
		WorkerRef: "worker-1",
		Authorize: func(_ context.Context, ref string) error {
			if ref != "worker-1" {
				t.Fatalf("ref=%q", ref)
			}
			return nil
		},
		Spawn: func(_ context.Context, ref, text string) (any, error) { return ref + ":" + text, nil },
	}
	tools := handler.Tools()
	if len(tools) != 1 || tools[0].Name != "spawn_subagent" {
		t.Fatalf("tools=%#v", tools)
	}
	value, err := handler.Call(context.Background(), "spawn_subagent", json.RawMessage(`{"text":"research"}`))
	if err != nil || value != "worker-1:research" {
		t.Fatalf("value=%#v err=%v", value, err)
	}
}

func TestChildHandlerHasNoTools(t *testing.T) {
	if tools := (EmptyHandler{}).Tools(); len(tools) != 0 {
		t.Fatalf("tools=%#v", tools)
	}
}
