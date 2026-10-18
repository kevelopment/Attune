//go:build linux

package device

import (
	"context"
	"encoding/json"
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

type lsblkResult struct {
	Blockdevices []lsblkDevice `json:"blockdevices"`
}
type lsblkDevice struct {
	Name        string        `json:"name"`
	Path        string        `json:"path"`
	Type        string        `json:"type"`
	FSType      string        `json:"fstype"`
	Label       string        `json:"label"`
	UUID        string        `json:"uuid"`
	PartUUID    string        `json:"partuuid"`
	Tran        string        `json:"tran"`
	RM          any           `json:"rm"`
	Hotplug     any           `json:"hotplug"`
	Mountpoints []string      `json:"mountpoints"`
	PKName      string        `json:"pkname"`
	Serial      string        `json:"serial"`
	WWN         string        `json:"wwn"`
	MajMin      string        `json:"maj:min"`
	Children    []lsblkDevice `json:"children"`
}

type mountedEntry struct{ root, fsRoot, majorMinor string }

func exclusiveRename(temp, target string) error {
	err := unix.Renameat2(unix.AT_FDCWD, temp, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOTSUP) {
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

func mountedVolumes() ([]volume, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "lsblk", "--json", "--bytes", "--paths", "--output", "NAME,PATH,TYPE,FSTYPE,LABEL,UUID,PARTUUID,TRAN,RM,HOTPLUG,MOUNTPOINTS,PKNAME,SERIAL,WWN,MAJ:MIN").Output()
	if err != nil {
		return nil, fmt.Errorf("enumerate block devices with lsblk: %w", err)
	}
	if len(b) > maxCommandOutput {
		return nil, errors.New("lsblk output exceeded limit")
	}
	var parsed lsblkResult
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, fmt.Errorf("parse lsblk output: %w", err)
	}
	mounts, err := readMountInfo("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	return volumesFromBlockMetadata(parsed, mounts), nil
}

func volumesFromBlockMetadata(parsed lsblkResult, mounts []mountedEntry) []volume {
	byDevice := map[string]lsblkDevice{}
	var index func([]lsblkDevice, lsblkDevice)
	index = func(ds []lsblkDevice, parent lsblkDevice) {
		for _, d := range ds {
			if d.PKName == "" {
				d.PKName = parent.Name
			}
			byDevice[d.MajMin] = d
			index(d.Children, d)
		}
	}
	index(parsed.Blockdevices, lsblkDevice{})
	rootDisk := systemRootDisk(parsed.Blockdevices, mounts)
	out := make([]volume, 0)
	for _, m := range mounts {
		if m.fsRoot != "/" || isProtectedSystemMount(m.root) {
			continue
		}
		d, ok := byDevice[m.majorMinor]
		if !ok || d.Type != "part" && d.Type != "disk" || d.FSType == "" || d.Path == "" {
			continue
		}
		physical := physicalBlock(d, byDevice, parsed.Blockdevices)
		if rootDisk != "" && physical.MajMin == rootDisk {
			continue
		}
		if !externalBlock(d, physical) {
			continue
		}
		id := stableBlockID(d, physical)
		if id == "" || !isDirNoSymlink(m.root) {
			continue
		}
		out = append(out, volume{Root: m.root, ID: id, Name: first(d.Label, filepath.Base(m.root)), Device: physical.Path, MountDevice: d.Path, External: true})
	}
	return out
}

func isProtectedSystemMount(root string) bool {
	clean := filepath.Clean(root)
	return clean == "/" || clean == "/boot" || clean == "/boot/efi" || clean == "/usr"
}

func systemRootDisk(devices []lsblkDevice, mounts []mountedEntry) string {
	byDevice := map[string]lsblkDevice{}
	var index func([]lsblkDevice, lsblkDevice)
	index = func(ds []lsblkDevice, parent lsblkDevice) {
		for _, d := range ds {
			if d.PKName == "" {
				d.PKName = parent.Name
			}
			byDevice[d.MajMin] = d
			index(d.Children, d)
		}
	}
	index(devices, lsblkDevice{})
	for _, mount := range mounts {
		if filepath.Clean(mount.root) != "/" {
			continue
		}
		root, ok := byDevice[mount.majorMinor]
		if !ok {
			return ""
		}
		return physicalBlock(root, byDevice, devices).MajMin
	}
	return ""
}

func physicalBlock(d lsblkDevice, byDevice map[string]lsblkDevice, devices []lsblkDevice) lsblkDevice {
	seen := map[string]bool{}
	for d.Type != "disk" && d.PKName != "" && !seen[d.Name] {
		seen[d.Name] = true
		parent, ok := findBlock(devices, d.PKName)
		if !ok {
			break
		}
		if indexed, ok := byDevice[parent.MajMin]; ok {
			parent = indexed
		}
		d = parent
	}
	return d
}

func findBlock(ds []lsblkDevice, name string) (lsblkDevice, bool) {
	for _, d := range ds {
		if d.Name == name || filepath.Base(d.Path) == name {
			return d, true
		}
		if v, ok := findBlock(d.Children, name); ok {
			return v, true
		}
	}
	return lsblkDevice{}, false
}

func externalBlock(part, disk lsblkDevice) bool {
	tran := strings.ToLower(first(part.Tran, disk.Tran))
	if tran == "usb" {
		return true
	}
	return truthy(part.RM) || truthy(disk.RM)
}

func stableBlockID(part, disk lsblkDevice) string {
	vol := first(part.UUID, part.PartUUID)
	physical := first(disk.WWN, disk.Serial)
	if physical != "" && vol != "" {
		return "linux:" + physical + ":" + vol
	}
	if part.UUID != "" {
		return "linux:uuid:" + part.UUID
	}
	if part.PartUUID != "" {
		return "linux:partuuid:" + part.PartUUID
	}
	return ""
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x == "1" || strings.EqualFold(x, "true")
	}
	return false
}

func readMountInfo(path string) ([]mountedEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mounted volume table: %w", err)
	}
	return parseMountInfo(string(b)), nil
}

func parseMountInfo(s string) []mountedEntry {
	out := []mountedEntry{}
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}
		// mountinfo fields 3 and 4 are major:minor and mount point.
		out = append(out, mountedEntry{fsRoot: unescapeMount(fields[3]), root: unescapeMount(fields[4]), majorMinor: fields[2]})
	}
	return out
}

func unescapeMount(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

func ejectVolume(ctx context.Context, v volume, command func(context.Context, string, ...string) ([]byte, error)) error {
	return ejectLinuxVolume(ctx, v, os.Stat, command)
}

func ejectLinuxVolume(ctx context.Context, v volume, stat func(string) (os.FileInfo, error), command func(context.Context, string, ...string) ([]byte, error)) error {
	if v.Device == "" || v.MountDevice == "" || !filepath.IsAbs(v.Device) || !filepath.IsAbs(v.MountDevice) || filepath.Clean(v.Device) != v.Device || filepath.Clean(v.MountDevice) != v.MountDevice {
		return errors.New("validated block device path is unavailable")
	}
	if _, err := stat(v.Device); err != nil {
		return fmt.Errorf("USB device disappeared: %w", err)
	}
	if _, err := command(ctx, "udisksctl", "unmount", "--block-device", v.MountDevice); err != nil {
		return fmt.Errorf("unmount volume (check user permissions): %w", err)
	}
	_, err := command(ctx, "udisksctl", "power-off", "--block-device", v.Device)
	if err != nil {
		return fmt.Errorf("power off USB device (check user permissions): %w", err)
	}
	return nil
}
