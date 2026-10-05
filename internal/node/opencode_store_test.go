package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeNativeStoreSelectionIsStableAndInstallationPrivate(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "home", ".local", "share")
	selected, err := SelectOpenCodeNativeStore(filepath.Join(root, "node-data"), false, legacy)
	if err != nil {
		t.Fatal("clean Node did not select a private native store")
	}
	want := OpenCodeNativeDataHome(filepath.Join(root, "node-data"))
	if selected.Mode != OpenCodeNativeStoreModeIsolated || selected.DataHome != want {
		t.Fatal("clean Node selected a legacy or unexpected OpenCode store")
	}
	nativeDB := filepath.Join(want, "opencode", "opencode.db")
	if err := os.WriteFile(nativeDB, []byte("existing native sessions"), 0o600); err != nil {
		t.Fatal("private native DB fixture unavailable")
	}
	if err := os.MkdirAll(filepath.Join(legacy, "opencode"), 0o700); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(legacy, "opencode", "opencode.db")
	if err := os.WriteFile(canary, []byte("personal native history"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := SelectOpenCodeNativeStore(filepath.Join(root, "node-data"), true, legacy)
	if err != nil || reopened.DataHome != want || reopened.Mode != OpenCodeNativeStoreModeIsolated {
		t.Fatal("reopening setup switched or reset the selected native store")
	}
	data, err := os.ReadFile(canary)
	if err != nil || string(data) != "personal native history" {
		t.Fatal("clean Node store selection touched the personal OpenCode database")
	}
	nativeData, err := os.ReadFile(nativeDB)
	if err != nil || string(nativeData) != "existing native sessions" {
		t.Fatal("reopening setup reset the existing native DB")
	}
	for _, path := range []string{want, filepath.Join(want, "opencode")} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatal("private native store directory is not mode 0700")
		}
	}
	selectionInfo, err := os.Stat(filepath.Join(root, "node-data", opencodeStoreSelectionFile))
	if err != nil || selectionInfo.Mode().Perm() != 0o600 {
		t.Fatal("Node native store selection is not mode 0600")
	}
}

func TestOpenCodeNativeStorePreservesLegacySessionsUntilApprovedMigration(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "legacy-data")
	if err := os.MkdirAll(filepath.Join(legacy, "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(legacy, "opencode", "opencode.db")
	if err := os.WriteFile(canary, []byte("legacy session database"), 0o644); err != nil {
		t.Fatal(err)
	}
	selected, err := SelectOpenCodeNativeStore(filepath.Join(root, "node-data"), true, legacy)
	if err != nil || selected.Mode != OpenCodeNativeStoreModeLegacy || selected.DataHome != legacy {
		t.Fatal("old Node state did not preserve its existing native data path")
	}
	reopened, err := SelectOpenCodeNativeStore(filepath.Join(root, "node-data"), false, filepath.Join(root, "another-store"))
	if err != nil || reopened.Mode != OpenCodeNativeStoreModeLegacy || reopened.DataHome != legacy {
		t.Fatal("restart silently changed a pinned legacy native store")
	}
	data, err := os.ReadFile(canary)
	if err != nil || string(data) != "legacy session database" {
		t.Fatal("legacy store contents were changed during selection")
	}
	info, err := os.Stat(legacy)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatal("legacy user's data directory permissions were changed")
	}
	if _, err := os.Stat(OpenCodeNativeDataHome(filepath.Join(root, "node-data"))); !os.IsNotExist(err) {
		t.Fatal("legacy selection created a misleading empty replacement store")
	}
}

func TestOpenCodeNativeStoreRejectsDuplicateAndMalformedJSONFields(t *testing.T) {
	root := t.TempDir()
	expected := OpenCodeNativeDataHome(root)
	personal := filepath.Join(t.TempDir(), "personal-opencode-native")
	if err := os.MkdirAll(filepath.Join(personal, "opencode"), 0o700); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(personal, "opencode", "opencode.db")
	if err := os.WriteFile(canary, []byte("personal canary"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"duplicate data_home":          `{"version":1,"mode":"isolated","data_home":"` + expected + `","data_home":"` + expected + `"}`,
		"escaped duplicate key":        `{"version":1,"mode":"isolated","data_home":"` + expected + `","data\u005fhome":"` + expected + `"}`,
		"duplicate mode":               `{"version":1,"mode":"isolated","mo\u0064e":"isolated","data_home":"` + expected + `"}`,
		"malformed escape":             `{"version":1,"mode":"isolated","data_home":"\uZZZZ"}`,
		"escaped control":              `{"version":1,"mode":"isolated","data_home":"` + expected + `\u000a"}`,
		"case-insensitive field alias": `{"version":1,"MODE":"isolated","data_home":"` + expected + `"}`,
		"unknown field":                `{"version":1,"mode":"isolated","data_home":"` + expected + `","fallback":"` + personal + `"}`,
		"trailing JSON":                `{"version":1,"mode":"isolated","data_home":"` + expected + `"}{}`,
	}
	for name, manifest := range cases {
		t.Run(name, func(t *testing.T) {
			selectionPath := filepath.Join(root, opencodeStoreSelectionFile)
			if err := os.WriteFile(selectionPath, []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := SelectOpenCodeNativeStoreWithOptions(root, OpenCodeNativeStoreOptions{SharedDataHome: expected}); err == nil {
				t.Fatal("invalid selection JSON was accepted")
			}
			if err := os.Remove(selectionPath); err != nil {
				t.Fatal(err)
			}
		})
	}
	if data, err := os.ReadFile(canary); err != nil || string(data) != "personal canary" {
		t.Fatal("rejected selection changed the personal canary")
	}
	if _, err := os.Stat(filepath.Join(personal, "opencode", "auth.json")); !os.IsNotExist(err) {
		t.Fatal("rejected selection created credentials in the personal store")
	}
}

func TestOpenCodeNativeStoreAcceptsEscapedCanonicalPath(t *testing.T) {
	root := t.TempDir()
	expected := OpenCodeNativeDataHome(root)
	escaped := strings.ReplaceAll(expected, "/", `\/`)
	manifest := `{"version":1,"mode":"isolated","data_home":"` + escaped + `"}`
	if err := os.WriteFile(filepath.Join(root, opencodeStoreSelectionFile), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	selected, err := SelectOpenCodeNativeStoreWithOptions(root, OpenCodeNativeStoreOptions{SharedDataHome: expected})
	if err != nil || selected.DataHome != expected || selected.Mode != OpenCodeNativeStoreModeIsolated {
		t.Fatalf("valid escaped canonical path was not decoded consistently: store=%#v err=%v", selected, err)
	}
}

func TestReadOpenCodeStoreSelectionRejectsLossyUnicode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, opencodeStoreSelectionFile)
	invalidUTF8 := append([]byte(`{"version":1,"mode":"legacy","data_home":"/tmp/legacy-`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)
	for name, encoded := range map[string][]byte{
		"invalid UTF-8":           invalidUTF8,
		"unpaired high surrogate": []byte(`{"version":1,"mode":"legacy","data_home":"/tmp/legacy-\uD800"}`),
		"unpaired low surrogate":  []byte(`{"version":1,"mode":"legacy","data_home":"/tmp/legacy-\uDC00"}`),
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, exists, err := readOpenCodeStoreSelection(root); err == nil || exists {
				t.Fatalf("malformed Unicode manifest was accepted: exists=%v err=%v", exists, err)
			}
		})
	}
}

func TestReadOpenCodeStoreSelectionAcceptsUnicodeAndPairedSurrogate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store-😀")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	expected := OpenCodeNativeDataHome(root)
	path := filepath.Join(root, opencodeStoreSelectionFile)
	manifest := openCodeStoreSelection{Version: 1, Mode: OpenCodeNativeStoreModeLegacy, DataHome: expected}
	for name, replaceUnicode := range map[string]bool{"raw valid Unicode": false, "paired surrogate escape": true} {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if replaceUnicode {
				encoded = []byte(strings.Replace(string(encoded), "😀", `\uD83D\uDE00`, 1))
			}
			if err := os.WriteFile(path, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			selected, exists, err := readOpenCodeStoreSelection(root)
			if err != nil || !exists || selected.DataHome != expected {
				t.Fatalf("valid Unicode manifest failed: selection=%#v exists=%v err=%v", selected, exists, err)
			}
		})
	}
}

func TestEncodeOpenCodeNativeStoreCLIRecordsUseOneTrustedSchema(t *testing.T) {
	secretary := OpenCodeNativeStore{Mode: OpenCodeNativeStoreModeLegacy, DataHome: "/private/tmp/legacy store"}
	worker := OpenCodeNativeStore{Mode: OpenCodeNativeStoreModeShared, DataHome: "/private/tmp/shared store"}
	secretaryRecord, err := EncodeSecretaryOpenCodeNativeStoreRecord(secretary, worker, "/private/tmp/node data", false)
	if err != nil || secretaryRecord != strings.Join([]string{"secretary", "legacy", secretary.DataHome, "shared", worker.DataHome, "/private/tmp/node data", "false"}, OpenCodeNativeStoreRecordSeparator) {
		t.Fatalf("Secretary store record omitted trusted pair data: record=%q err=%v", secretaryRecord, err)
	}
	nodeRecord, err := EncodeNodeOpenCodeNativeStoreRecord(worker, "/private/tmp/node data", false, true)
	if err != nil || nodeRecord != strings.Join([]string{"node", "shared", worker.DataHome, "/private/tmp/node data", "false", "true"}, OpenCodeNativeStoreRecordSeparator) {
		t.Fatalf("Node store record omitted trusted deployment fields: record=%q err=%v", nodeRecord, err)
	}
	if _, err := EncodeNodeOpenCodeNativeStoreRecord(worker, "/private/tmp/node\ndata", false, true); err == nil {
		t.Fatal("control character was allowed in shell-consumed data_dir")
	}
	worker.DataHome += "\nunauthorized-field"
	if _, err := EncodeNodeOpenCodeNativeStoreRecord(worker, "/private/tmp/node data", false, true); err == nil {
		t.Fatal("control character was allowed in shell-consumed native store record")
	}
}

func TestHasOpenCodeSessionMappingsRejectsDuplicateAndCaseAliases(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "node-state.json")
	for name, encoded := range map[string]string{
		"duplicate mapping key":           `{"mappings":{"attempt":{"harness_instance_id":"local/opencode"},"attempt":{}}}`,
		"escaped duplicate session field": `{"mappings":{"attempt":{"harness\u005finstance_id":"fx","harness_instance_id":"local/opencode"}}}`,
		"case alias mapping root":         `{"Mappings":{"attempt":{"harness_instance_id":"local/opencode"}}}`,
		"case alias session field":        `{"mappings":{"attempt":{"harness_instance_id":"fx","HARNESS_INSTANCE_ID":"local/opencode"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
				t.Fatal(err)
			}
			if hasSessions, err := hasOpenCodeSessionMappings(path); err == nil || hasSessions {
				t.Fatal("ambiguous managed-session mapping was accepted")
			}
		})
	}
}

func TestOpenCodeNativeStoreSelectionTempSymlinkCannotTruncatePersonalDB(t *testing.T) {
	root := t.TempDir()
	nodeData := filepath.Join(root, "node-data")
	if err := os.MkdirAll(nodeData, 0o700); err != nil {
		t.Fatal(err)
	}
	personalDB := filepath.Join(root, "personal-opencode.db")
	if err := os.WriteFile(personalDB, []byte("personal sessions"), 0o600); err != nil {
		t.Fatal(err)
	}
	tempPath := filepath.Join(nodeData, opencodeStoreSelectionFile+".tmp")
	if err := os.Symlink(personalDB, tempPath); err != nil {
		t.Fatal(err)
	}
	if _, err := SelectOpenCodeNativeStore(nodeData, false, ""); err == nil {
		t.Fatal("selection followed a temp symlink and overwrote a personal DB")
	}
	data, err := os.ReadFile(personalDB)
	if err != nil || string(data) != "personal sessions" {
		t.Fatal("failed selection changed personal native DB")
	}
}

func TestOpenCodeNativeStoreRejectsSymlinkToPersonalStore(t *testing.T) {
	root := t.TempDir()
	personal := filepath.Join(root, "personal-opencode")
	if err := os.MkdirAll(personal, 0o700); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(personal, "opencode.db")
	if err := os.WriteFile(canary, []byte("must not be touched"), 0o600); err != nil {
		t.Fatal(err)
	}
	nodeData := filepath.Join(root, "node-data")
	if err := os.MkdirAll(nodeData, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(personal, OpenCodeNativeDataHome(nodeData)); err != nil {
		t.Fatal(err)
	}
	if _, err := SelectOpenCodeNativeStore(nodeData, false, ""); err == nil {
		t.Fatal("native store selection followed a symlink to personal data")
	}
	data, err := os.ReadFile(canary)
	if err != nil || string(data) != "must not be touched" {
		t.Fatal("rejected symlink changed the personal store")
	}
	if _, err := os.Stat(filepath.Join(personal, "opencode")); !os.IsNotExist(err) {
		t.Fatal("rejected symlink caused native files to be created in personal data")
	}
}
