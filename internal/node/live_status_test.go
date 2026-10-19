package node

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"
    "time"
)

func TestLiveStatusWriterTransitionsAndPermissions(t *testing.T) {
    path := filepath.Join(t.TempDir(), "live-status.json")
    writer := NewLiveStatusWriter(path)
    for _, state := range []string{"reconnecting", "connected", "reconnecting", "offline"} {
        writer.Publish(state, nil)
        data, err := os.ReadFile(path)
        if err != nil { t.Fatal(err) }
        var snapshot liveSnapshot
        if err := json.Unmarshal(data, &snapshot); err != nil { t.Fatal(err) }
        if snapshot.Server != state { t.Fatalf("expected %s, got %s", state, snapshot.Server) }
        if time.Since(snapshot.UpdatedAt) > time.Minute { t.Fatalf("stale snapshot: %v", snapshot.UpdatedAt) }
        if snapshot.Harnesses == nil { t.Fatal("harness list absent") }
        info, err := os.Stat(path)
        if err != nil { t.Fatal(err) }
        if info.Mode().Perm() != 0600 { t.Fatalf("file permissions: %v", info.Mode()) }
    }
}

func TestLiveStatusWriterRejectsMissingDirectory(t *testing.T) {
    path := filepath.Join(t.TempDir(), "missing", "live-status.json")
    NewLiveStatusWriter(path).Publish("connected", nil)
    if _, err := os.Stat(path); !os.IsNotExist(err) { t.Fatalf("unexpected status file: %v", err) }
}
