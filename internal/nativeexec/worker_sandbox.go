package nativeexec

import "context"

// WorkerSandbox applies OS-level filesystem and process isolation to native
// Worker subprocesses, including shells and MCP child processes.
type WorkerSandbox struct {
	ProtectedDir string
	Workspace string
}

type workerSandboxContextKey struct{}

func WithWorkerSandbox(ctx context.Context, policy WorkerSandbox) context.Context {
	return context.WithValue(ctx, workerSandboxContextKey{}, policy)
}

func workerSandboxFromContext(ctx context.Context) (WorkerSandbox, bool) {
	policy, ok := ctx.Value(workerSandboxContextKey{}).(WorkerSandbox)
	return policy, ok
}
