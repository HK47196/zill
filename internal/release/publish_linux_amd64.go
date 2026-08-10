//go:build linux && amd64

package release

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	sysRenameat2   = 316
	renameExchange = 2
)

func publishDirectory(staging, destination string) (error, error) {
	_, err := os.Lstat(destination)
	if os.IsNotExist(err) {
		return nil, os.Rename(staging, destination)
	}
	if err != nil {
		return nil, err
	}
	left, err := syscall.BytePtrFromString(staging)
	if err != nil {
		return nil, err
	}
	right, err := syscall.BytePtrFromString(destination)
	if err != nil {
		return nil, err
	}
	directoryFD := ^uintptr(99) // AT_FDCWD (-100) as an unsigned syscall argument.
	_, _, errno := syscall.Syscall6(sysRenameat2, directoryFD, uintptr(unsafe.Pointer(left)), directoryFD, uintptr(unsafe.Pointer(right)), renameExchange, 0)
	if errno != 0 {
		return nil, errno
	}
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("published release, but could not remove exchanged old output %s: %w", staging, err), nil
	}
	return nil, nil
}
