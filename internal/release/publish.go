// SPDX-License-Identifier: GPL-3.0-or-later

package release

import (
	"fmt"
	"os"
	"path/filepath"
)

type publishItem struct {
	staging     string
	destination string
}

type preparedPublishItem struct {
	publishItem
	destinationExists bool
}

func preparePublish(items []publishItem) ([]preparedPublishItem, error) {
	prepared := make([]preparedPublishItem, 0, len(items))
	paths := make(map[string]string, len(items)*2)
	for _, item := range items {
		if item.staging == "" || item.destination == "" {
			return nil, fmt.Errorf("publish paths must not be empty")
		}
		for role, path := range map[string]string{
			"staging":     item.staging,
			"destination": item.destination,
		} {
			clean := filepath.Clean(path)
			if previous, ok := paths[clean]; ok {
				return nil, fmt.Errorf("publish path %s is used as both %s and %s", path, previous, role)
			}
			paths[clean] = role
		}
		if _, err := os.Lstat(item.staging); err != nil {
			return nil, fmt.Errorf("inspect staged output %s: %w", item.staging, err)
		}
		_, err := os.Lstat(item.destination)
		switch {
		case err == nil:
			prepared = append(prepared, preparedPublishItem{publishItem: item, destinationExists: true})
		case os.IsNotExist(err):
			prepared = append(prepared, preparedPublishItem{publishItem: item})
		default:
			return nil, fmt.Errorf("inspect publish destination %s: %w", item.destination, err)
		}
	}
	return prepared, nil
}
