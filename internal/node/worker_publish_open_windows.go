//go:build windows

package node

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// Windows NtCreateFile is handle-relative: Openat(O_NOFOLLOW_ANY) rejects
// symlinks, junctions and other reparse traversals atomically while walking
// pinned directory handles. No Lstat/Open race or intermediate relinking can
// escape a trusted Worker workspace.
func openPublishedFileNoFollow(workspace, relativePath string) (*os.File, error) {
	if !ValidPublishedSourcePath(filepath.ToSlash(relativePath)) {
		return nil, errors.New("artifact publish: invalid Windows workspace path")
	}
	directory, err := syscall.Open(workspace, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil { return nil, err }
	parts := strings.Split(filepath.ToSlash(relativePath), "/")
	for _, part := range parts[:len(parts)-1] {
		next, openErr := windows.Openat(directory, part, syscall.O_RDONLY|windows.O_DIRECTORY|windows.O_NOFOLLOW_ANY|syscall.O_CLOEXEC, 0)
		_ = syscall.CloseHandle(directory)
		if openErr != nil { return nil, openErr }
		directory = next
	}
	fd, openErr := windows.Openat(directory, parts[len(parts)-1], syscall.O_RDONLY|windows.O_NOFOLLOW_ANY|syscall.O_CLOEXEC, 0)
	_ = syscall.CloseHandle(directory)
	if openErr != nil { return nil, openErr }
	return os.NewFile(uintptr(fd), relativePath), nil
}
