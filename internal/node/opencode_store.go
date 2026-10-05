package node

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const opencodeStoreSelectionFile = "opencode-native-selection.json"

// OpenCodeNativeStore keeps native harness data local to one Secretary server
// installation or one Execution Node. DataHome is the XDG data home passed to
// OpenCode, which stores its database below DataHome/opencode.
type OpenCodeNativeStore struct {
	DataHome          string
	Legacy            bool
	MigrationRequired bool
}

type openCodeStoreSelection struct {
	Version  int    `json:"version"`
	Mode     string `json:"mode"`
	DataHome string `json:"data_home"`
}

func OpenCodeNativeDataHome(dataDir string) string {
	if strings.TrimSpace(dataDir) == "" {
		return ""
	}
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return filepath.Join(dataDir, "opencode-native")
	}
	return filepath.Join(root, "opencode-native")
}

// SelectOpenCodeNativeStore pins the store choice across installation restarts.
// Existing application state without a selection record is treated as legacy
// so managed OpenCode sessions keep their data home until owner-approved
// migration; a clean installation gets a private store instead.
func SelectOpenCodeNativeStore(dataDir string, hasExistingState bool, legacyDataHome string) (OpenCodeNativeStore, error) {
	root, err := filepath.Abs(strings.TrimSpace(dataDir))
	if err != nil || strings.TrimSpace(dataDir) == "" {
		return OpenCodeNativeStore{}, errors.New("opencode: installation data directory is required")
	}
	if info, statErr := os.Lstat(root); statErr == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return OpenCodeNativeStore{}, errors.New("opencode: installation data directory must not be a symlink")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return OpenCodeNativeStore{}, errors.New("opencode: installation data directory unavailable")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return OpenCodeNativeStore{}, errors.New("opencode: installation data directory unavailable")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return OpenCodeNativeStore{}, errors.New("opencode: Node data directory permissions unavailable")
	}

	selectionPath := filepath.Join(root, opencodeStoreSelectionFile)
	if info, statErr := os.Lstat(selectionPath); statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return OpenCodeNativeStore{}, errors.New("opencode: invalid native store selection")
		}
		encoded, readErr := os.ReadFile(selectionPath)
		if readErr != nil {
			return OpenCodeNativeStore{}, errors.New("opencode: native store selection unavailable")
		}
		var selection openCodeStoreSelection
		if json.Unmarshal(encoded, &selection) != nil || selection.Version != 1 || !filepath.IsAbs(selection.DataHome) {
			return OpenCodeNativeStore{}, errors.New("opencode: invalid native store selection")
		}
		switch selection.Mode {
		case "isolated":
			privateHome := OpenCodeNativeDataHome(root)
			if filepath.Clean(selection.DataHome) != filepath.Clean(privateHome) {
				return OpenCodeNativeStore{}, errors.New("opencode: isolated native store path does not match this installation")
			}
			if err := EnsurePrivateOpenCodeNativeDataHome(privateHome); err != nil {
				return OpenCodeNativeStore{}, err
			}
			return OpenCodeNativeStore{DataHome: privateHome}, nil
		case "legacy":
			return OpenCodeNativeStore{DataHome: selection.DataHome, Legacy: true, MigrationRequired: true}, nil
		default:
			return OpenCodeNativeStore{}, errors.New("opencode: invalid native store mode")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return OpenCodeNativeStore{}, errors.New("opencode: native store selection unavailable")
	}

	selection := openCodeStoreSelection{Version: 1}
	store := OpenCodeNativeStore{}
	if hasExistingState {
		if strings.TrimSpace(legacyDataHome) == "" {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil || strings.TrimSpace(home) == "" {
				return OpenCodeNativeStore{}, errors.New("opencode: legacy native data home unavailable")
			}
			legacyDataHome = filepath.Join(home, ".local", "share")
		}
		legacyDataHome, err = filepath.Abs(legacyDataHome)
		if err != nil {
			return OpenCodeNativeStore{}, errors.New("opencode: legacy native data home must be absolute")
		}
		selection.Mode, selection.DataHome = "legacy", legacyDataHome
		store = OpenCodeNativeStore{DataHome: legacyDataHome, Legacy: true, MigrationRequired: true}
	} else {
		selection.Mode = "isolated"
		selection.DataHome = OpenCodeNativeDataHome(root)
		if err := EnsurePrivateOpenCodeNativeDataHome(selection.DataHome); err != nil {
			return OpenCodeNativeStore{}, err
		}
		store = OpenCodeNativeStore{DataHome: selection.DataHome}
	}
	if err := writeOpenCodeStoreSelection(selectionPath, selection); err != nil {
		return OpenCodeNativeStore{}, err
	}
	return store, nil
}

func writeOpenCodeStoreSelection(path string, selection openCodeStoreSelection) error {
	encoded, err := json.MarshalIndent(selection, "", "  ")
	if err != nil {
		return fmt.Errorf("opencode: encode native store selection: %w", err)
	}
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.New("opencode: save native store selection failed")
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return errors.New("opencode: save native store selection failed")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return errors.New("opencode: save native store selection failed")
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return errors.New("opencode: save native store selection failed")
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return errors.New("opencode: protect native store selection failed")
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return errors.New("opencode: save native store selection failed")
	}
	return nil
}

// EnsurePrivateOpenCodeNativeDataHome creates only the selected Secretary-owned
// native store. It deliberately never follows a symlink at the store/app root.
func EnsurePrivateOpenCodeNativeDataHome(dataHome string) error {
	if strings.TrimSpace(dataHome) == "" || !filepath.IsAbs(dataHome) {
		return errors.New("opencode: absolute native data home is required")
	}
	root := filepath.Dir(filepath.Clean(dataHome))
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("opencode: installation data directory must not be a symlink")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return errors.New("opencode: installation data directory permissions unavailable")
	}
	for _, dir := range []string{filepath.Clean(dataHome), filepath.Join(filepath.Clean(dataHome), "opencode")} {
		if info, statErr := os.Lstat(dir); statErr == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return errors.New("opencode: native store directory must not be a symlink")
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return errors.New("opencode: private native store directory unavailable")
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return errors.New("opencode: private native store directory unavailable")
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("opencode: native store directory must not be a symlink")
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return errors.New("opencode: native store directory permissions unavailable")
		}
	}
	return nil
}
