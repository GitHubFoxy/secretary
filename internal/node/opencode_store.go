package node

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

const (
	opencodeStoreSelectionFile = "opencode-native-selection.json"

	// OpenCodeNativeStoreRecordSeparator separates trusted fields emitted to the CLI.
	OpenCodeNativeStoreRecordSeparator = "\x1f"
)

type OpenCodeNativeStoreMode string

const (
	OpenCodeNativeStoreModeIsolated OpenCodeNativeStoreMode = "isolated"
	OpenCodeNativeStoreModeShared   OpenCodeNativeStoreMode = "shared"
	OpenCodeNativeStoreModeLegacy   OpenCodeNativeStoreMode = "legacy"
)

// OpenCodeNativeStore keeps native harness data local to one Secretary server
// installation or one Execution Node. Mode is mutually exclusive by design.
type OpenCodeNativeStore struct {
	DataHome string
	Mode     OpenCodeNativeStoreMode
}

type OpenCodeNativeStoreOptions struct {
	HasExistingState bool
	LegacyDataHome   string
	SharedDataHome   string
	Standalone       bool
}

type openCodeStoreSelection struct {
	Version  int                     `json:"version"`
	Mode     OpenCodeNativeStoreMode `json:"mode"`
	DataHome string                  `json:"data_home"`
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

// SelectOpenCodeNativeStore pins a server-role store. Existing application
// state without a selection is treated as legacy so managed OpenCode sessions
// keep their data home until owner-approved migration.
func SelectOpenCodeNativeStore(dataDir string, hasExistingState bool, legacyDataHome string) (OpenCodeNativeStore, error) {
	return SelectOpenCodeNativeStoreWithOptions(dataDir, OpenCodeNativeStoreOptions{
		HasExistingState: hasExistingState,
		LegacyDataHome:   legacyDataHome,
		SharedDataHome:   OpenCodeNativeDataHome(dataDir),
	})
}

// SelectOpenCodeNativeStoreWithOptions applies the same parser and canonical
// path checks to Secretary and Node selections before touching the selected
// native store. A standalone Node must not accept a shared Secretary store.
func SelectOpenCodeNativeStoreWithOptions(dataDir string, options OpenCodeNativeStoreOptions) (OpenCodeNativeStore, error) {
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

	legacyHome, err := canonicalLegacyDataHome(options.LegacyDataHome)
	if err != nil {
		return OpenCodeNativeStore{}, err
	}
	selection, exists, err := readOpenCodeStoreSelection(root)
	if err != nil {
		return OpenCodeNativeStore{}, err
	}
	if exists {
		switch selection.Mode {
		case OpenCodeNativeStoreModeIsolated:
			privateHome := OpenCodeNativeDataHome(root)
			if !isCanonicalStoreHome(selection.DataHome, privateHome) {
				return OpenCodeNativeStore{}, errors.New("opencode: isolated native store path does not match this installation")
			}
			if err := EnsurePrivateOpenCodeNativeDataHome(privateHome); err != nil {
				return OpenCodeNativeStore{}, err
			}
			return OpenCodeNativeStore{DataHome: privateHome, Mode: OpenCodeNativeStoreModeIsolated}, nil
		case OpenCodeNativeStoreModeShared:
			if options.Standalone {
				return OpenCodeNativeStore{}, errors.New("opencode: standalone Node cannot use the shared Secretary store")
			}
			if strings.TrimSpace(options.SharedDataHome) == "" || !isCanonicalStoreHome(selection.DataHome, options.SharedDataHome) {
				return OpenCodeNativeStore{}, errors.New("opencode: shared native store path does not match this installation")
			}
			sharedHome, err := filepath.Abs(options.SharedDataHome)
			if err != nil {
				return OpenCodeNativeStore{}, errors.New("opencode: shared native store path is unavailable")
			}
			if err := EnsurePrivateOpenCodeNativeDataHome(sharedHome); err != nil {
				return OpenCodeNativeStore{}, err
			}
			return OpenCodeNativeStore{DataHome: sharedHome, Mode: OpenCodeNativeStoreModeShared}, nil
		case OpenCodeNativeStoreModeLegacy:
			if err := validateLegacyOpenCodeNativeDataHome(selection.DataHome); err != nil {
				return OpenCodeNativeStore{}, err
			}
			return OpenCodeNativeStore{DataHome: selection.DataHome, Mode: OpenCodeNativeStoreModeLegacy}, nil
		default:
			return OpenCodeNativeStore{}, errors.New("opencode: invalid native store mode")
		}
	}

	selection = openCodeStoreSelection{Version: 1}
	store := OpenCodeNativeStore{}
	if options.HasExistingState {
		if err := validateLegacyOpenCodeNativeDataHome(legacyHome); err != nil {
			return OpenCodeNativeStore{}, err
		}
		selection.Mode, selection.DataHome = OpenCodeNativeStoreModeLegacy, legacyHome
		store = OpenCodeNativeStore{DataHome: legacyHome, Mode: OpenCodeNativeStoreModeLegacy}
	} else {
		selection.Mode = OpenCodeNativeStoreModeIsolated
		selection.DataHome = OpenCodeNativeDataHome(root)
		if err := EnsurePrivateOpenCodeNativeDataHome(selection.DataHome); err != nil {
			return OpenCodeNativeStore{}, err
		}
		store = OpenCodeNativeStore{DataHome: selection.DataHome, Mode: OpenCodeNativeStoreModeIsolated}
	}
	if err := writeOpenCodeStoreSelection(filepath.Join(root, opencodeStoreSelectionFile), selection); err != nil {
		return OpenCodeNativeStore{}, err
	}
	return store, nil
}

func readOpenCodeStoreSelection(root string) (openCodeStoreSelection, bool, error) {
	path := filepath.Join(root, opencodeStoreSelectionFile)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return openCodeStoreSelection{}, false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return openCodeStoreSelection{}, false, errors.New("opencode: invalid native store selection")
	}
	encoded, err := os.ReadFile(path)
	var selection openCodeStoreSelection
	if err != nil || decodeStrictJSON(encoded, &selection) != nil || selection.Version != 1 || !filepath.IsAbs(selection.DataHome) || selection.DataHome != filepath.Clean(selection.DataHome) || strings.IndexFunc(selection.DataHome, unicode.IsControl) >= 0 {
		return openCodeStoreSelection{}, false, errors.New("opencode: invalid native store selection")
	}
	if selection.Mode != OpenCodeNativeStoreModeIsolated && selection.Mode != OpenCodeNativeStoreModeLegacy && selection.Mode != OpenCodeNativeStoreModeShared {
		return openCodeStoreSelection{}, false, errors.New("opencode: invalid native store mode")
	}
	return selection, true, nil
}

// EncodeSecretaryOpenCodeNativeStoreRecord emits the validated Secretary and
// local Node stores plus the expected local Node data directory.
func EncodeSecretaryOpenCodeNativeStoreRecord(secretary, worker OpenCodeNativeStore, workerDataDir string, workerStandalone bool) (string, error) {
	secretaryFields, err := openCodeNativeStoreRecordFields(secretary)
	if err != nil {
		return "", err
	}
	workerFields, err := openCodeNativeStoreRecordFields(worker)
	if err != nil {
		return "", err
	}
	workerDataDir, err = canonicalCLIDataDir(workerDataDir)
	if err != nil {
		return "", err
	}
	fields := []string{"secretary"}
	fields = append(fields, secretaryFields...)
	fields = append(fields, workerFields...)
	fields = append(fields, workerDataDir, strconv.FormatBool(workerStandalone))
	return strings.Join(fields, OpenCodeNativeStoreRecordSeparator), nil
}

// EncodeNodeOpenCodeNativeStoreRecord emits one validated Node store together
// with trusted deployment scope and probe policy from the same Go config read.
func EncodeNodeOpenCodeNativeStoreRecord(store OpenCodeNativeStore, dataDir string, standalone, includeOpenCode bool) (string, error) {
	storeFields, err := openCodeNativeStoreRecordFields(store)
	if err != nil {
		return "", err
	}
	dataDir, err = canonicalCLIDataDir(dataDir)
	if err != nil {
		return "", err
	}
	fields := []string{"node"}
	fields = append(fields, storeFields...)
	fields = append(fields, dataDir, strconv.FormatBool(standalone), strconv.FormatBool(includeOpenCode))
	return strings.Join(fields, OpenCodeNativeStoreRecordSeparator), nil
}

func openCodeNativeStoreRecordFields(store OpenCodeNativeStore) ([]string, error) {
	if store.Mode != OpenCodeNativeStoreModeIsolated && store.Mode != OpenCodeNativeStoreModeShared && store.Mode != OpenCodeNativeStoreModeLegacy {
		return nil, errors.New("opencode: invalid selected native store mode")
	}
	if !filepath.IsAbs(store.DataHome) || store.DataHome != filepath.Clean(store.DataHome) || strings.IndexFunc(store.DataHome, unicode.IsControl) >= 0 {
		return nil, errors.New("opencode: selected native store path cannot be safely emitted")
	}
	return []string{string(store.Mode), store.DataHome}, nil
}

func canonicalCLIDataDir(dataDir string) (string, error) {
	if strings.TrimSpace(dataDir) == "" || strings.TrimSpace(dataDir) != dataDir || strings.IndexFunc(dataDir, unicode.IsControl) >= 0 {
		return "", errors.New("opencode: selected Node data directory cannot be safely emitted")
	}
	absolute, err := filepath.Abs(dataDir)
	if err != nil {
		return "", errors.New("opencode: selected Node data directory is unavailable")
	}
	absolute = filepath.Clean(absolute)
	if strings.IndexFunc(absolute, unicode.IsControl) >= 0 {
		return "", errors.New("opencode: selected Node data directory cannot be safely emitted")
	}
	return absolute, nil
}

func canonicalLegacyDataHome(dataHome string) (string, error) {
	if strings.TrimSpace(dataHome) == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return "", errors.New("opencode: legacy native data home unavailable")
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	if !filepath.IsAbs(dataHome) {
		absolute, err := filepath.Abs(dataHome)
		if err != nil {
			return "", errors.New("opencode: legacy native data home must be absolute")
		}
		dataHome = absolute
	}
	return filepath.Clean(dataHome), nil
}

func isCanonicalStoreHome(selected, expected string) bool {
	if !filepath.IsAbs(expected) || selected != filepath.Clean(selected) {
		return false
	}
	absolute, err := filepath.Abs(expected)
	return err == nil && selected == filepath.Clean(absolute)
}

func validateLegacyOpenCodeNativeDataHome(dataHome string) error {
	for _, path := range []string{filepath.Dir(dataHome), dataHome, filepath.Join(dataHome, "opencode")} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("opencode: legacy native store path is unavailable or unsafe")
		}
	}
	return nil
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
