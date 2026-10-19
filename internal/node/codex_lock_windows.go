//go:build windows

package node

import "errors"

func lockCodexPairScope(scope string) (func(), error) {
 return nil, errors.New("native Codex pair provisioning is unsupported on Windows")
}
