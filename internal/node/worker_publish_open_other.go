//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !windows

package node

import (
	"errors"
	"os"
)

// Without a handle-relative O_NOFOLLOW equivalent, publication fails closed.
func openPublishedFileNoFollow(workspace, relativePath string) (*os.File, error) {
	return nil, errors.New("artifact publish: atomic no-follow file opening unsupported on this OS")
}
