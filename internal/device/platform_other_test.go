//go:build !darwin

package device

import (
	"errors"
	"testing"
)

func TestNonDarwinDevicePlatformIsExplicitlyUnsupported(t *testing.T) {
	if _, err := mountedVolumes(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("mountedVolumes() error = %v, want ErrUnsupported", err)
	}
	if _, _, err := volumeSpace("/"); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("volumeSpace() error = %v, want ErrUnsupported", err)
	}
	if err := exclusiveRename("temp", "target"); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("exclusiveRename() error = %v, want ErrUnsupported", err)
	}
}
