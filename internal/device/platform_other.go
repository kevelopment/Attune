//go:build !darwin && !linux && !windows

package device

import (
	"context"
	"errors"
	"fmt"
)

func exclusiveRename(temp, target string) error       { return errors.ErrUnsupported }
func volumeSpace(root string) (uint64, uint64, error) { return 0, 0, errors.ErrUnsupported }
func mountedVolumes() ([]volume, error) {
	return nil, fmt.Errorf("USB discovery is unavailable on this platform: %w", errors.ErrUnsupported)
}
func ejectVolume(context.Context, volume, func(context.Context, string, ...string) ([]byte, error)) error {
	return errors.ErrUnsupported
}
