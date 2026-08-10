//go:build !linux || !amd64

package release

import (
	"errors"
	"fmt"
	"os"
)

func publishAll(items []publishItem) ([]error, error) {
	prepared, err := preparePublish(items)
	if err != nil {
		return nil, err
	}
	for _, item := range prepared {
		if item.destinationExists {
			return nil, fmt.Errorf("atomic replacement of existing release output %s requires Linux amd64 renameat2(RENAME_EXCHANGE)", item.destination)
		}
	}

	published := make([]preparedPublishItem, 0, len(prepared))
	for _, item := range prepared {
		if err := os.Rename(item.staging, item.destination); err != nil {
			var rollbackErrors []error
			for index := len(published) - 1; index >= 0; index-- {
				publishedItem := published[index]
				if rollbackErr := os.Rename(publishedItem.destination, publishedItem.staging); rollbackErr != nil {
					rollbackErrors = append(rollbackErrors, fmt.Errorf("roll back %s: %w", publishedItem.destination, rollbackErr))
				}
			}
			return nil, errors.Join(
				fmt.Errorf("publish %s: %w", item.destination, err),
				errors.Join(rollbackErrors...),
			)
		}
		published = append(published, item)
	}
	return nil, nil
}
