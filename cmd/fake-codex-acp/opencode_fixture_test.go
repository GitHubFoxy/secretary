package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCodePackagingFixtureOnlyEchoesConfiguredSelections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	config := `{"default_agent":"managed","model":"fixture/model","agents":{"managed":{"description":"fixture-marker"}},"providers":{"fixture":{"models":{"model":{"variants":[{"id":"low"}]}}}}}`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_CONFIG", path)
	for _, test := range []struct {
		id, value string
		allowed   bool
	}{{"mode", "managed", true}, {"mode", "build", false}, {"model", "fixture/model/low", true}, {"model", "fixture/model/xhigh", false}, {"model", "other/model", false}} {
		params, _ := json.Marshal(map[string]string{"configId": test.id, "value": test.value})
		result, ok := openCodeFixtureSelection(params)
		if ok != test.allowed || ok && result == nil {
			t.Fatal("packaging fixture invented a selection")
		}
	}
}
