package nativeexec

import "context"

// WorkerSandbox applies OS-level filesystem and process isolation to native
// Worker subprocesses, including shells and MCP child processes.
type WorkerSandbox struct {
	ProtectedDir string
	// Co-located Secretary Server state: its SQLite DB contains encrypted
	// Node credentials and the transport secret needed to decrypt them.
	ServerDataDir string
	Workspace string
	WritableDataHome string
}

type workerSandboxContextKey struct{}

func WithWorkerSandbox(ctx context.Context, policy WorkerSandbox) context.Context {
	return context.WithValue(ctx, workerSandboxContextKey{}, policy)
}

// The selected OpenCode provider store is a deliberate, dedicated carve-out
// from the private Node directory; never expose any other Node state.
func WithWorkerWritableDataHome(ctx context.Context, dataHome string) context.Context {
	policy, ok := workerSandboxFromContext(ctx)
	if !ok { return ctx }
	policy.WritableDataHome = dataHome
	return WithWorkerSandbox(ctx, policy)
}

func workerSandboxFromContext(ctx context.Context) (WorkerSandbox, bool) {
	policy, ok := ctx.Value(workerSandboxContextKey{}).(WorkerSandbox)
	return policy, ok
}

func IsWorkerSandboxed(ctx context.Context) bool {
	_, ok := workerSandboxFromContext(ctx)
	return ok
}
