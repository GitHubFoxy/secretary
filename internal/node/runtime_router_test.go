package node

import (
	"context"
	"testing"
)

type recordingRuntime struct{ started chan StartRequest }

func (r recordingRuntime) Start(_ context.Context, request StartRequest) (Session, error) {
	r.started <- request
	return nil, nil
}

func TestRuntimeRouterUsesProfileHarness(t *testing.T) {
	compat := recordingRuntime{started: make(chan StartRequest, 1)}
	fx := recordingRuntime{started: make(chan StartRequest, 1)}
	native := recordingRuntime{started: make(chan StartRequest, 1)}
	router := RuntimeRouter{DefaultHarness: "codex", ACP: compat, FX: fx, OpenCode: native}
	if _, err := router.Start(context.Background(), StartRequest{Profile: ManagedProfile{Runtime: "opencode"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-native.started:
	default:
		t.Fatal("OpenCode runtime was not selected")
	}
	if _, err := router.Start(context.Background(), StartRequest{Profile: ManagedProfile{Runtime: "codex"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-compat.started:
	default:
		t.Fatal("compatibility runtime was not selected")
	}
	if _, err := router.Start(context.Background(), StartRequest{Profile: ManagedProfile{Runtime: "fx"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fx.started:
	default:
		t.Fatal("fx runtime was not selected")
	}
}
