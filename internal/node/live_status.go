package node

import (
    "encoding/json"
    "os"
    "path/filepath"
    "sync"
    "time"

    "github.com/beruseruko/secretary/internal/core"
)

type LiveStatusWriter struct { path string; mu sync.Mutex; disconnected bool }

func NewLiveStatusWriter(path string) *LiveStatusWriter { return &LiveStatusWriter{path: path} }

type liveHarness struct {
    Kind string `json:"kind"`
    Status string `json:"status"`
}

type liveSnapshot struct {
    Server string `json:"server"`
    UpdatedAt time.Time `json:"updated_at"`
    Harnesses []liveHarness `json:"harnesses"`
}

func (w *LiveStatusWriter) Disconnected() {
    if w == nil { return }
    w.mu.Lock()
    defer w.mu.Unlock()
    w.disconnected = true
    w.publish("reconnecting", nil)
}

func (w *LiveStatusWriter) Connected(inventory *core.HarnessInventorySnapshot) {
    if w == nil { return }
    w.mu.Lock()
    defer w.mu.Unlock()
    if !w.disconnected { w.publish("connected", inventory) }
}

func (w *LiveStatusWriter) Publish(server string, inventory *core.HarnessInventorySnapshot) {
    if w == nil { return }
    w.mu.Lock()
    defer w.mu.Unlock()
    if server == "reconnecting" { w.disconnected = false }
    if server == "offline" { w.disconnected = true }
    w.publish(server, inventory)
}

func (w *LiveStatusWriter) publish(server string, inventory *core.HarnessInventorySnapshot) {
    snapshot := liveSnapshot{Server: server, UpdatedAt: time.Now().UTC(), Harnesses: []liveHarness{}}
    if inventory != nil {
        data, err := json.Marshal(inventory)
        if err == nil {
            var decoded struct { Instances []liveHarness `json:"instances"` }
            if json.Unmarshal(data, &decoded) == nil {
                snapshot.Harnesses = decoded.Instances
            }
        }
    }
    encoded, err := json.Marshal(snapshot)
    if err != nil { return }
    dir := filepath.Dir(w.path)
    if info, err := os.Stat(dir); err != nil || !info.IsDir() { return }
    tmp, err := os.CreateTemp(dir, ".node-live-*")
    if err != nil { return }
    defer os.Remove(tmp.Name())
    defer tmp.Close()
    if tmp.Chmod(0600) != nil { return }
    if _, err := tmp.Write(encoded); err != nil { return }
    if tmp.Sync() != nil { return }
    _ = os.Rename(tmp.Name(), w.path)
}
