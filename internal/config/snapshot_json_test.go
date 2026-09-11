package config

import (
	"encoding/json"
	"testing"
)

func TestSnapshotJSONRoundTripKeepsProfileContent(t *testing.T) {
	original := Snapshot{Version: "v1", Config: Config{Runtime: Runtime{Harness: "opencode"}}, Profiles: map[string]Profile{"worker": {Name: "worker", Content: "managed", Skills: []Skill{{Path: "/tmp/s", Content: "skill", Hash: "h"}}}}}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Snapshot
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Version != original.Version || decoded.Profiles["worker"].Content != original.Profiles["worker"].Content || decoded.Profiles["worker"].Skills[0].Content != "skill" {
		t.Fatalf("decoded=%#v", decoded)
	}
}
