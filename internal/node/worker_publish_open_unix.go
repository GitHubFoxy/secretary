//go:build linux || darwin || freebsd || netbsd || openbsd

package node

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Secure directory traversal uses one openat(O_NOFOLLOW) per component.
// Even if a Worker swaps a directory entry into a symlink between steps,
// the kernel resolves the next step relative to a pinned directory fd.
func openPublishedFileNoFollow(workspace, relativePath string) (*os.File, error) {
	if !ValidPublishedSourcePath(filepath.ToSlash(relativePath)) {
		return nil, errors.New("artifact publish: unsafe relative path")
	}
	directory, err := unix.Open(workspace, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil { return nil, err }
	parts := strings.Split(filepath.ToSlash(relativePath), "/")
	for _, part := range parts[:len(parts)-1] {
		next, openErr := unix.Openat(directory, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(directory)
		if openErr != nil { return nil, openErr }
		directory = next
	}
	fd, openErr := unix.Openat(directory, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	_ = unix.Close(directory)
	if openErr != nil { return nil, openErr }
	return os.NewFile(uintptr(fd), relativePath), nil
}
