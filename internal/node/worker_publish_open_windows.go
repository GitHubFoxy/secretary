//go:build windows

package node

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows NtCreateFile opens one path component relative to a pinned parent
// HANDLE. OBJ_DONT_REPARSE and FILE_OPEN_REPARSE_POINT reject symlinks,
// junctions and all other reparse points atomically (no Lstat/Open race).
func openPublishedFileNoFollow(workspace, relativePath string) (*os.File, error) {
	if !ValidPublishedSourcePath(filepath.ToSlash(relativePath)) {
		return nil, errors.New("artifact publish: invalid Windows workspace path")
	}
	rootName, err := syscall.UTF16PtrFromString(workspace)
	if err != nil { return nil, err }
	directory, err := syscall.CreateFile(rootName, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil { return nil, err }
	var rootInfo syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(directory, &rootInfo); err != nil {
		_ = syscall.CloseHandle(directory)
		return nil, err
	}
	if rootInfo.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 ||
		rootInfo.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
		_ = syscall.CloseHandle(directory)
		return nil, errors.New("artifact publish: workspace root is not a regular directory")
	}
	parts := strings.Split(filepath.ToSlash(relativePath), "/")
	for _, part := range parts[:len(parts)-1] {
		next, openErr := openNtNoFollow(directory, part, true)
		_ = syscall.CloseHandle(directory)
		if openErr != nil { return nil, openErr }
		directory = next
	}
	fd, openErr := openNtNoFollow(directory, parts[len(parts)-1], false)
	_ = syscall.CloseHandle(directory)
	if openErr != nil { return nil, openErr }
	return os.NewFile(uintptr(fd), relativePath), nil
}

func openNtNoFollow(parent syscall.Handle, name string, isDir bool) (syscall.Handle, error) {
	encoded, err := windows.NewNTUnicodeString(name)
	if err != nil { return syscall.InvalidHandle, err }
	attr := windows.OBJECT_ATTRIBUTES{
		ObjectName: encoded,
		RootDirectory: parent,
		Attributes: windows.OBJ_DONT_REPARSE,
	}
	attr.Length = uint32(unsafe.Sizeof(attr))
	options := uint32(windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT)
	if isDir { options |= windows.FILE_DIRECTORY_FILE } else { options |= windows.FILE_NON_DIRECTORY_FILE }
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, windows.FILE_GENERIC_READ, &attr, &windows.IO_STATUS_BLOCK{},
		nil, uint32(syscall.FILE_ATTRIBUTE_NORMAL),
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		windows.FILE_OPEN, options, 0, 0)
	if err != nil { return syscall.InvalidHandle, err }
	fd := syscall.Handle(handle)
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(fd, &info); err != nil {
		_ = syscall.CloseHandle(fd)
		return syscall.InvalidHandle, err
	}
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = syscall.CloseHandle(fd)
		return syscall.InvalidHandle, errors.New("artifact publish: reparse paths are forbidden")
	}
	return fd, nil
}
