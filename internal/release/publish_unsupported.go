//go:build !linux || !amd64

package release

import (
	"fmt"
	"os"
)

func publishDirectory(staging, destination string) (error, error) {
	if _, err := os.Lstat(destination); os.IsNotExist(err) {
		return nil, os.Rename(staging, destination)
	} else if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("atomic replacement of an existing release requires Linux amd64 renameat2(RENAME_EXCHANGE)")
}
