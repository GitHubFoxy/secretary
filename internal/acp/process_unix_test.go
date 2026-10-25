//go:build !windows

package acp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/nativeexec"
)

// A background child would write the marker after its ACP parent is cancelled.
func TestACPCancellationTerminatesNativeDescendants(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "orphaned-acp-child")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cmd, err := nativeexec.CommandContext(ctx, "/bin/sh", "-c",
		"(sleep 1; printf orphan > \"$1\") & wait", "acp-test", marker)
	if err != nil {
		t.Fatal(err)
	}
	configureACPProcess(cmd)
	if err := cmd.Run(); err == nil {
		t.Fatal("ACP process did not terminate on context cancellation")
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("ACP process exited before timeout: %v", ctx.Err())
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ACP child survived cancellation and wrote %s: %v", marker, err)
	}
}
