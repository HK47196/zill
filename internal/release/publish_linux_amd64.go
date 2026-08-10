//go:build linux && amd64

package release

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	sysRenameat2   = 316
	renameExchange = 2
)

func publishAll(items []publishItem) ([]error, error) {
	prepared, err := preparePublish(items)
	if err != nil {
		return nil, err
	}
	published := make([]preparedPublishItem, 0, len(prepared))
	for _, item := range prepared {
		if item.destinationExists {
			err = exchangePaths(item.staging, item.destination)
		} else {
			err = os.Rename(item.staging, item.destination)
		}
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("publish %s: %w", item.destination, err),
				rollbackPublished(published),
			)
		}
		published = append(published, item)
	}

	var warnings []error
	for _, item := range published {
		if !item.destinationExists {
			continue
		}
		if err := os.RemoveAll(item.staging); err != nil {
			warnings = append(warnings, fmt.Errorf("remove exchanged old output %s: %w", item.staging, err))
		}
	}
	return warnings, nil
}

func rollbackPublished(items []preparedPublishItem) error {
	var rollbackErrors []error
	for index := len(items) - 1; index >= 0; index-- {
		item := items[index]
		var err error
		if item.destinationExists {
			err = exchangePaths(item.staging, item.destination)
		} else {
			err = os.Rename(item.destination, item.staging)
		}
		if err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("roll back %s: %w", item.destination, err))
		}
	}
	return errors.Join(rollbackErrors...)
}

func exchangePaths(leftPath, rightPath string) error {
	left, err := syscall.BytePtrFromString(leftPath)
	if err != nil {
		return err
	}
	right, err := syscall.BytePtrFromString(rightPath)
	if err != nil {
		return err
	}
	directoryFD := ^uintptr(99) // AT_FDCWD (-100) as an unsigned syscall argument.
	_, _, errno := syscall.Syscall6(sysRenameat2, directoryFD, uintptr(unsafe.Pointer(left)), directoryFD, uintptr(unsafe.Pointer(right)), renameExchange, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
