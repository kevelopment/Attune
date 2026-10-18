//go:build linux

package device

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseMountInfoEscapesMountPaths(t *testing.T) {
	got := parseMountInfo("37 25 8:17 / /media/My\\040Kindle rw,relatime - vfat /dev/sdb1 rw\ninvalid\n")
	if len(got) != 1 || got[0].majorMinor != "8:17" || got[0].fsRoot != "/" || got[0].root != "/media/My Kindle" {
		t.Fatalf("parseMountInfo() = %+v", got)
	}
}

func TestLinuxVolumesRequireMountedExternalStableVolume(t *testing.T) {
	root := t.TempDir()
	parsed := lsblkResult{Blockdevices: []lsblkDevice{{Name: "sdb", Path: "/dev/sdb", Type: "disk", Tran: "usb", Serial: "usb-serial", WWN: "wwn-1", MajMin: "8:16", Children: []lsblkDevice{
		{Name: "sdb1", Path: "/dev/sdb1", Type: "part", FSType: "vfat", Label: "Kindle", UUID: "fs-uuid", PartUUID: "part-uuid", PKName: "sdb", MajMin: "8:17"},
		{Name: "sdb2", Path: "/dev/sdb2", Type: "part", FSType: "vfat", Label: "unmounted", UUID: "fs-2", PKName: "sdb", MajMin: "8:18"},
	}}, {Name: "nvme0n1", Path: "/dev/nvme0n1", Type: "disk", Tran: "nvme", Serial: "internal", MajMin: "259:0", Children: []lsblkDevice{
		{Name: "nvme0n1p1", Path: "/dev/nvme0n1p1", Type: "part", FSType: "ext4", Label: "arbitrary", UUID: "internal-uuid", PKName: "nvme0n1", MajMin: "259:1"},
	}}}}
	mounts := []mountedEntry{{root: root, fsRoot: "/", majorMinor: "8:17"}, {root: filepath.Join(root, "unmounted"), fsRoot: "/", majorMinor: "8:18"}, {root: root, fsRoot: "/", majorMinor: "259:1"}, {root: root, fsRoot: "/", majorMinor: "8:99"}}
	got := volumesFromBlockMetadata(parsed, mounts)
	if len(got) != 1 {
		t.Fatalf("expected only one mounted external volume, got %+v", got)
	}
	if got[0].Root != root || got[0].ID != "linux:wwn-1:fs-uuid" || got[0].Device != "/dev/sdb" || got[0].MountDevice != "/dev/sdb1" || !got[0].External {
		t.Fatalf("unexpected mounted volume: %+v", got[0])
	}
}

func TestLinuxVolumesFailClosedWithoutStableID(t *testing.T) {
	root := t.TempDir()
	parsed := lsblkResult{Blockdevices: []lsblkDevice{{Name: "sda", Path: "/dev/sda", Type: "disk", RM: true, MajMin: "8:0", Children: []lsblkDevice{{Name: "sda1", Path: "/dev/sda1", Type: "part", FSType: "vfat", PKName: "sda", MajMin: "8:1"}}}}}
	if got := volumesFromBlockMetadata(parsed, []mountedEntry{{root: root, fsRoot: "/", majorMinor: "8:1"}}); len(got) != 0 {
		t.Fatalf("volume without UUID/serial accepted: %+v", got)
	}
}

func TestLinuxVolumesRejectUSBSystemRootAndBootMounts(t *testing.T) {
	root := t.TempDir()
	parsed := lsblkResult{Blockdevices: []lsblkDevice{{Name: "sdb", Path: "/dev/sdb", Type: "disk", Tran: "usb", Serial: "boot-usb", MajMin: "8:16", Children: []lsblkDevice{
		{Name: "sdb1", Path: "/dev/sdb1", Type: "part", FSType: "ext4", UUID: "root-uuid", PKName: "sdb", MajMin: "8:17"},
		{Name: "sdb2", Path: "/dev/sdb2", Type: "part", FSType: "vfat", UUID: "kindle-uuid", PKName: "sdb", MajMin: "8:18"},
	}}}}
	mounts := []mountedEntry{{root: "/", fsRoot: "/", majorMinor: "8:17"}, {root: root, fsRoot: "/", majorMinor: "8:18"}, {root: "/boot/efi", fsRoot: "/", majorMinor: "8:18"}}
	if got := volumesFromBlockMetadata(parsed, mounts); len(got) != 0 {
		t.Fatalf("USB system/root or boot volume accepted: %+v", got)
	}
}

func TestLinuxRootDiskWalksEncryptedParentChain(t *testing.T) {
	root := t.TempDir()
	parsed := lsblkResult{Blockdevices: []lsblkDevice{{Name: "sdb", Path: "/dev/sdb", Type: "disk", Tran: "usb", Serial: "boot-usb", MajMin: "8:16", Children: []lsblkDevice{
		{Name: "sdb1", Path: "/dev/sdb1", Type: "part", PKName: "sdb", MajMin: "8:17", Children: []lsblkDevice{{Name: "cryptroot", Path: "/dev/mapper/cryptroot", Type: "crypt", FSType: "ext4", UUID: "root-uuid", PKName: "sdb1", MajMin: "253:0"}}},
		{Name: "sdb2", Path: "/dev/sdb2", Type: "part", FSType: "vfat", UUID: "kindle-uuid", PKName: "sdb", MajMin: "8:18"},
	}}}}
	mounts := []mountedEntry{{root: "/", fsRoot: "/", majorMinor: "253:0"}, {root: root, fsRoot: "/", majorMinor: "8:18"}}
	if got := volumesFromBlockMetadata(parsed, mounts); len(got) != 0 {
		t.Fatalf("other partition on USB root disk accepted: %+v", got)
	}
}

func TestLinuxRejectsHotplugOnlyAndBindSubdirectoryMounts(t *testing.T) {
	root := t.TempDir()
	parsed := lsblkResult{Blockdevices: []lsblkDevice{{Name: "sdc", Path: "/dev/sdc", Type: "disk", Tran: "sata", Hotplug: true, Serial: "internal-hotplug", MajMin: "8:32", Children: []lsblkDevice{{Name: "sdc1", Path: "/dev/sdc1", Type: "part", FSType: "vfat", UUID: "fs-id", PKName: "sdc", MajMin: "8:33"}}}}}
	mounts := []mountedEntry{{root: root, fsRoot: "/", majorMinor: "8:33"}, {root: filepath.Join(root, "bind"), fsRoot: "/Documents", majorMinor: "8:33"}}
	if got := volumesFromBlockMetadata(parsed, mounts); len(got) != 0 {
		t.Fatalf("hotplug-only bind mount accepted: %+v", got)
	}
}

func TestLinuxEjectUsesValidatedBlockPathsAndInjectedCommand(t *testing.T) {
	v := volume{Root: "/media/kindle", ID: "usb:uuid", External: true, Device: "/dev/sdb", MountDevice: "/dev/sdb1"}
	var got [][]string
	err := ejectLinuxVolume(context.Background(), v, func(path string) (os.FileInfo, error) {
		if path != "/dev/sdb" {
			t.Fatalf("stat path = %q", path)
		}
		return nil, nil
	}, func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append(got, append([]string{name}, args...))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"udisksctl", "unmount", "--block-device", "/dev/sdb1"}, {"udisksctl", "power-off", "--block-device", "/dev/sdb"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
	err = ejectLinuxVolume(context.Background(), v, func(string) (os.FileInfo, error) { return nil, nil }, func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("permission denied") })
	if err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("eject error = %v, expected permission guidance", err)
	}
}
