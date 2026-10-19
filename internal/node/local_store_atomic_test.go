package node

import (
 "encoding/json"
 "os"
 "path/filepath"
 "sync"
 "testing"
)

func TestLocalStoreConcurrentAtomicWritesUseDistinctTempFiles(t *testing.T) {
 path := filepath.Join(t.TempDir(), "node-state.json")
 const writers = 8
 const writes = 24
 stores := make([]*LocalStore, writers)
 for i := range stores {
  store, err := OpenLocalStore(path)
  if err != nil { t.Fatal(err) }
  stores[i] = store
 }
 var workers sync.WaitGroup
 errors := make(chan error, writers*writes)
 for _, store := range stores {
  workers.Add(1)
  go func(store *LocalStore) {
   defer workers.Done()
   for i := 0; i < writes; i++ {
    store.mu.Lock()
    err := store.persistLocked()
    store.mu.Unlock()
    if err != nil { errors <- err }
   }
  }(store)
 }
 workers.Wait()
 close(errors)
 for err := range errors { t.Errorf("concurrent state persistence: %v", err) }
 content, err := os.ReadFile(path)
 if err != nil { t.Fatal(err) }
 var state localState
 if err := json.Unmarshal(content, &state); err != nil { t.Fatalf("atomic state is invalid: %v", err) }
 matches, err := filepath.Glob(path+".tmp-*")
 if err != nil { t.Fatal(err) }
 if len(matches) != 0 { t.Fatalf("stale local state temporary files: %v", matches) }
}
