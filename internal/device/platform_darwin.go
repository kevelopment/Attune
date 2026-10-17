//go:build darwin

package device

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func exclusiveRename(temp, target string) error {
	err := unix.RenamexNp(temp, target, unix.RENAME_EXCL)
	if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
		return fmt.Errorf("exclusive rename unsupported: %w", errors.ErrUnsupported)
	}
	return err
}

func volumeSpace(root string) (uint64, uint64, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(root, &s); err != nil {
		return 0, 0, err
	}
	return s.Bavail * uint64(s.Bsize), s.Blocks * uint64(s.Bsize), nil
}

// mountedVolumes gets candidate mount points from /Volumes then asks diskutil
// for authoritative USB/removable metadata; arbitrary directories never qualify.
func mountedVolumes() ([]volume, error) {
	entries, err := os.ReadDir("/Volumes")
	if err != nil {
		return nil, err
	}
	out := []volume{}
	for _, entry := range entries {
		root := filepath.Join("/Volumes", entry.Name())
		if !isDirNoSymlink(root) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		b, err := exec.CommandContext(ctx, "diskutil", "info", "-plist", root).Output()
		cancel()
		if err != nil || len(b) > maxCommandOutput {
			continue
		}
		meta := readPlistStrings(b)
		if meta["MountPoint"] == "" || filepath.Clean(meta["MountPoint"]) != filepath.Clean(root) {
			continue
		}
		internal := strings.EqualFold(meta["Internal"], "true")
		bus := strings.ToLower(meta["BusProtocol"] + " " + meta["Protocol"])
		external := !internal && (strings.Contains(bus, "usb") || strings.Contains(bus, "sd") || strings.Contains(bus, "external"))
		if !external {
			continue
		}
		id := meta["VolumeUUID"]
		if id == "" {
			id = meta["DeviceIdentifier"]
		}
		out = append(out, volume{Root: root, ID: id, Name: first(meta["VolumeName"], meta["MediaName"], entry.Name()), External: true})
	}
	return out, nil
}
