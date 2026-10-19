package node

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/beruseruko/secretary/internal/core"
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

func TestLiveDisconnectOverridesDelayedConnectedAndClearsReady(t *testing.T) {
    path := filepath.Join(t.TempDir(), "live-status.json")
    writer := NewLiveStatusWriter(path)
    inventory := core.HarnessInventorySnapshot{
        Node: "macbook",
        ObservedAt: time.Now().UTC(),
        Instances: []core.HarnessInstance{{
            ID: "macbook/fx", Node: "macbook", Kind: core.HarnessFX,
            Status: core.HarnessReady,
        }},
    }
    read := func() liveSnapshot {
        t.Helper()
        data, err := os.ReadFile(path)
        if err != nil { t.Fatal(err) }
        var snapshot liveSnapshot
        if err := json.Unmarshal(data, &snapshot); err != nil { t.Fatal(err) }
        return snapshot
    }
    writer.Publish("reconnecting", nil)
    writer.Connected(&inventory)
    connected := read()
    if connected.Server != "connected" || len(connected.Harnesses) != 1 || connected.Harnesses[0].Kind != "fx" || connected.Harnesses[0].Status != "ready" {
        t.Fatalf("inventory was not reflected: %#v", connected)
    }
    writer.Disconnected()
    writer.Connected(&inventory)
    disconnected := read()
    if disconnected.Server != "reconnecting" || len(disconnected.Harnesses) != 0 {
        t.Fatalf("delayed heartbeat revived stale status: %#v", disconnected)
    }
    writer.Publish("reconnecting", nil)
    writer.Connected(&inventory)
    if restored := read(); restored.Server != "connected" || len(restored.Harnesses) != 1 {
        t.Fatalf("reconnection did not restore newly verified inventory: %#v", restored)
    }
}

func TestLiveDiscoveryFailureInvalidatesReadyWhileServerConnected(t *testing.T) {
    path := filepath.Join(t.TempDir(), "live-status.json")
    writer := NewLiveStatusWriter(path)
    inventory := daemonInventoryFixture("macbook")
    writer.Publish("reconnecting", nil)
    writer.Connected(&inventory)
    writer.Connected(nil)
    writer.Connected(nil)
    data, err := os.ReadFile(path)
    if err != nil { t.Fatal(err) }
    var snapshot liveSnapshot
    if err := json.Unmarshal(data, &snapshot); err != nil { t.Fatal(err) }
    if snapshot.Server != "connected" || len(snapshot.Harnesses) != 0 {
        t.Fatalf("failed discovery incorrectly retained ready harness: %#v", snapshot)
    }
    writer.Connected(&inventory)
    data, err = os.ReadFile(path)
    if err != nil { t.Fatal(err) }
    if err := json.Unmarshal(data, &snapshot); err != nil { t.Fatal(err) }
    if len(snapshot.Harnesses) != 1 || snapshot.Harnesses[0].Status != "ready" {
        t.Fatalf("restored discovery not reflected: %#v", snapshot)
    }
}
