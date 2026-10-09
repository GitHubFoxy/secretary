package node

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// SelectSharedOpenCodeNativeStores is the explicit owner transition for a
// co-located Secretary and Node. It preserves legacy data and never imports
// credentials, history, or session mappings.
func SelectSharedOpenCodeNativeStores(secretaryDataDir, nodeDataDir string) (OpenCodeNativeStore, OpenCodeNativeStore, error) {
	if err := validateSharedStoreOwnerDeployment(nodeDataDir); err != nil {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, err
	}
	secretaryRoot, err := prepareOpenCodeSelectionRoot(secretaryDataDir)
	if err != nil {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, err
	}
	nodeRoot, err := prepareOpenCodeSelectionRoot(nodeDataDir)
	if err != nil {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, err
	}
	if filepath.Clean(secretaryRoot) == filepath.Clean(nodeRoot) {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, errors.New("opencode: Secretary and Node selection directories must be distinct")
	}
	if hasSessions, err := hasOpenCodeSessionMappings(filepath.Join(nodeRoot, "node-state.json")); err != nil {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, err
	} else if hasSessions {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, errors.New("opencode: shared-store selection refused because managed OpenCode sessions exist in Node mappings")
	}

	secretaryPath := filepath.Join(secretaryRoot, opencodeStoreSelectionFile)
	nodePath := filepath.Join(nodeRoot, opencodeStoreSelectionFile)
	secretarySelection, hasSecretarySelection, err := readOpenCodeStoreSelection(secretaryRoot)
	if err != nil {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, err
	}
	nodeSelection, hasNodeSelection, err := readOpenCodeStoreSelection(nodeRoot)
	if err != nil {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, err
	}
	sharedHome := OpenCodeNativeDataHome(secretaryRoot)
	if hasSecretarySelection && !selectionCanUseSharedHome(secretarySelection, sharedHome) {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, errors.New("opencode: shared-store selection refused because Secretary has a conflicting native store selection")
	}
	if hasNodeSelection && !selectionCanUseSharedHome(nodeSelection, sharedHome) {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, errors.New("opencode: shared-store selection refused because Node has a conflicting native store selection")
	}
	if hasSecretarySelection && hasNodeSelection && secretarySelection.Mode == OpenCodeNativeStoreModeLegacy && nodeSelection.Mode == OpenCodeNativeStoreModeLegacy && secretarySelection.DataHome != nodeSelection.DataHome {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, errors.New("opencode: shared-store selection refused because legacy Secretary and Node stores conflict")
	}
	alreadySelected := hasSecretarySelection && hasNodeSelection && (secretarySelection.Mode == OpenCodeNativeStoreModeShared || secretarySelection.Mode == OpenCodeNativeStoreModeIsolated) && (nodeSelection.Mode == OpenCodeNativeStoreModeShared || nodeSelection.Mode == OpenCodeNativeStoreModeIsolated) && secretarySelection.DataHome == sharedHome && nodeSelection.DataHome == sharedHome
	if !alreadySelected {
		nonempty, err := openCodeStoreHasFiles(sharedHome)
		if err != nil {
			return OpenCodeNativeStore{}, OpenCodeNativeStore{}, err
		}
		if nonempty {
			return OpenCodeNativeStore{}, OpenCodeNativeStore{}, errors.New("opencode: shared-store selection refused because the target native store is nonempty and unselected")
		}
	}
	for _, path := range []string{secretaryPath + ".tmp", nodePath + ".tmp"} {
		if _, err := os.Lstat(path); err == nil {
			return OpenCodeNativeStore{}, OpenCodeNativeStore{}, errors.New("opencode: shared-store selection is incomplete; remove no files and retry the owner command")
		} else if !errors.Is(err, os.ErrNotExist) {
			return OpenCodeNativeStore{}, OpenCodeNativeStore{}, errors.New("opencode: shared-store selection cannot be verified")
		}
	}
	if err := EnsurePrivateOpenCodeNativeDataHome(sharedHome); err != nil {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, err
	}

	selection := openCodeStoreSelection{Version: 1, Mode: OpenCodeNativeStoreModeShared, DataHome: sharedHome}
	secretaryOld, secretaryHadOld, err := readSelectionBytes(secretaryPath)
	if err != nil {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, err
	}
	_, _, err = readSelectionBytes(nodePath)
	if err != nil {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, err
	}
	secretaryTemp, err := writeOpenCodeStoreSelectionTemp(secretaryPath, selection)
	if err != nil {
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, err
	}
	nodeTemp, err := writeOpenCodeStoreSelectionTemp(nodePath, selection)
	if err != nil {
		_ = os.Remove(secretaryTemp)
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, err
	}
	if err := os.Rename(secretaryTemp, secretaryPath); err != nil {
		_ = os.Remove(secretaryTemp)
		_ = os.Remove(nodeTemp)
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, errors.New("opencode: save shared-store selection failed")
	}
	if err := os.Rename(nodeTemp, nodePath); err != nil {
		_ = os.Remove(nodeTemp)
		if rollbackErr := restoreOpenCodeStoreSelection(secretaryPath, secretaryOld, secretaryHadOld); rollbackErr != nil {
			return OpenCodeNativeStore{}, OpenCodeNativeStore{}, errors.New("opencode: shared-store selection is incomplete; keep Secretary and Node stopped and rerun secretary opencode select-shared-store")
		}
		return OpenCodeNativeStore{}, OpenCodeNativeStore{}, errors.New("opencode: save shared-store selection failed; previous selection was restored")
	}
	return OpenCodeNativeStore{DataHome: sharedHome, Mode: OpenCodeNativeStoreModeShared}, OpenCodeNativeStore{DataHome: sharedHome, Mode: OpenCodeNativeStoreModeShared}, nil
}

func validateSharedStoreOwnerDeployment(nodeDataDir string) error {
	if strings.TrimSpace(nodeDataDir) == "" {
		return errors.New("opencode: co-located Node deployment config cannot be validated")
	}
	nodeRoot, err := filepath.Abs(strings.TrimSpace(nodeDataDir))
	if err != nil {
		return errors.New("opencode: co-located Node deployment config cannot be validated")
	}
	configPath := filepath.Join(filepath.Dir(filepath.Clean(nodeRoot)), "config.json")
	if _, err := os.Lstat(configPath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return errors.New("opencode: co-located Node deployment config cannot be validated")
	}
	deployment, err := LoadDeploymentConfig(configPath)
	if err != nil || deployment.Standalone {
		return errors.New("opencode: shared-store owner selection requires a valid co-located Node deployment")
	}
	configuredNodeRoot, err := filepath.Abs(deployment.DataDir)
	if err != nil || filepath.Clean(configuredNodeRoot) != filepath.Clean(nodeRoot) {
		return errors.New("opencode: shared-store owner selection requires a matching co-located Node data directory")
	}
	return nil
}

func prepareOpenCodeSelectionRoot(dataDir string) (string, error) {
	if strings.TrimSpace(dataDir) == "" {
		return "", errors.New("opencode: installation data directory is required")
	}
	root, err := filepath.Abs(strings.TrimSpace(dataDir))
	if err != nil {
		return "", errors.New("opencode: installation data directory is unavailable")
	}
	if info, err := os.Lstat(root); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return "", errors.New("opencode: installation data directory must not be a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("opencode: installation data directory unavailable")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", errors.New("opencode: installation data directory unavailable")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return "", errors.New("opencode: installation data directory permissions unavailable")
	}
	return root, nil
}

func selectionCanUseSharedHome(selection openCodeStoreSelection, sharedHome string) bool {
	switch selection.Mode {
	case OpenCodeNativeStoreModeShared, OpenCodeNativeStoreModeIsolated:
		return selection.DataHome == sharedHome
	case OpenCodeNativeStoreModeLegacy:
		return validateLegacyOpenCodeNativeDataHome(selection.DataHome) == nil
	default:
		return false
	}
}

func hasOpenCodeSessionMappings(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("opencode: local Node session mappings cannot be verified")
	}
	var state struct {
		Mappings map[string]struct {
			HarnessInstanceID string `json:"harness_instance_id"`
		} `json:"mappings"`
	}
	encoded, err := os.ReadFile(path)
	if err != nil || rejectDuplicateJSONKeys(encoded) != nil || rejectSessionMappingFieldAliases(encoded) != nil || json.Unmarshal(encoded, &state) != nil {
		return false, errors.New("opencode: local Node session mappings cannot be verified")
	}
	for _, mapping := range state.Mappings {
		id := strings.ToLower(strings.TrimSpace(mapping.HarnessInstanceID))
		if id == "opencode" || strings.HasSuffix(id, "/opencode") {
			return true, nil
		}
	}
	return false, nil
}

func rejectSessionMappingFieldAliases(encoded []byte) error {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &document); err != nil {
		return err
	}
	for key := range document {
		if strings.EqualFold(key, "mappings") && key != "mappings" {
			return errors.New("non-canonical Node mappings field")
		}
	}
	rawMappings, exists := document["mappings"]
	if !exists || string(rawMappings) == "null" {
		return nil
	}
	var mappings map[string]json.RawMessage
	if err := json.Unmarshal(rawMappings, &mappings); err != nil {
		return err
	}
	for _, rawMapping := range mappings {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawMapping, &fields); err != nil {
			return err
		}
		for key := range fields {
			if strings.EqualFold(key, "harness_instance_id") && key != "harness_instance_id" {
				return errors.New("non-canonical Node session mapping field")
			}
		}
	}
	return nil
}

func openCodeStoreHasFiles(dataHome string) (bool, error) {
	info, err := os.Lstat(dataHome)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("opencode: shared native store path is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("opencode: shared native store path must not be a symlink")
	}
	if !info.IsDir() {
		return false, errors.New("opencode: shared native store path is not a directory")
	}
	entries, err := os.ReadDir(dataHome)
	if err != nil {
		return false, errors.New("opencode: shared native store cannot be inspected safely")
	}
	for _, entry := range entries {
		path := filepath.Join(dataHome, entry.Name())
		child, err := os.Lstat(path)
		if err != nil || child.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("opencode: shared native store contains an unsafe path")
		}
		if !child.IsDir() {
			return true, nil
		}
		files, err := openCodeStoreHasFiles(path)
		if err != nil || files {
			return files, err
		}
	}
	return false, nil
}

func readSelectionBytes(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, false, errors.New("opencode: invalid native store selection")
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, false, errors.New("opencode: native store selection unavailable")
	}
	return encoded, true, nil
}

func writeOpenCodeStoreSelectionTemp(path string, selection openCodeStoreSelection) (string, error) {
	encoded, err := json.MarshalIndent(selection, "", "  ")
	if err != nil {
		return "", errors.New("opencode: encode shared-store selection failed")
	}
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", errors.New("opencode: prepare shared-store selection failed")
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return "", errors.New("opencode: prepare shared-store selection failed")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return "", errors.New("opencode: prepare shared-store selection failed")
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", errors.New("opencode: prepare shared-store selection failed")
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return "", errors.New("opencode: protect shared-store selection failed")
	}
	return tmp, nil
}

func restoreOpenCodeStoreSelection(path string, previous []byte, existed bool) error {
	if !existed {
		return os.Remove(path)
	}
	tmp := path + ".rollback.tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(previous); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
