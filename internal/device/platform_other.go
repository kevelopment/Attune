//go:build !darwin

package device

import (
	"errors"
	"fmt"
)

func exclusiveRename(temp, target string) error { return errors.ErrUnsupported }

func volumeSpace(root string) (uint64, uint64, error) {
	return 0, 0, errors.ErrUnsupported
}

func mountedVolumes() ([]volume, error) {
	return nil, fmt.Errorf("Kindle USB discovery is available on macOS only: %w", errors.ErrUnsupported)
}
