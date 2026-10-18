//go:build windows

package device

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestParseWindowsVolumesRequiresStableExternalDriveVolume(t *testing.T) {
	got, err := parseWindowsVolumes([]byte(`[{"Root":"E:\\","ID":"windows:Volume{abc}:serial-1","Name":"Kindle","Device":"\\\\.\\PhysicalDrive2","MountDevice":"\\\\.\\E:","External":true},{"Root":"F:\\","ID":"windows:boot-volume","Device":"\\\\.\\PhysicalDrive3","MountDevice":"\\\\.\\F:","External":true,"IsBoot":true},{"Root":"G:\\","ID":"windows:system-volume","Device":"\\\\.\\PhysicalDrive4","MountDevice":"\\\\.\\G:","External":true,"IsSystem":true},{"Root":"H:\\","ID":"","Device":"\\\\.\\PhysicalDrive5","MountDevice":"\\\\.\\H:","External":true},{"Root":"I:\\","ID":"internal","Device":"\\\\.\\PhysicalDrive6","MountDevice":"\\\\.\\I:","External":false},{"Root":"\\\\server\\share","ID":"network","Device":"\\\\.\\PhysicalDrive7","MountDevice":"\\\\.\\J:","External":true}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Root != `E:\` || got[0].ID != "windows:Volume{abc}:serial-1" || got[0].Device != `\\.\PhysicalDrive2` || got[0].MountDevice != `\\.\E:` {
		t.Fatalf("parseWindowsVolumes() = %+v", got)
	}
}

func TestWindowsEjectStopsWhenVolumeLockFails(t *testing.T) {
	called := []string{}
	var gotHandles []windows.Handle
	err := ejectWindowsSequence(42, func(h windows.Handle, code uint32) error {
		gotHandles = append(gotHandles, h)
		called = append(called, ejectControlName(code))
		if code == 0x00090018 {
			return errDeviceBusy
		}
		return nil
	})
	if err == nil || len(called) != 1 || called[0] != "lock" {
		t.Fatalf("lock failure sequence = %v, error = %v", called, err)
	}
	if len(gotHandles) != 1 || gotHandles[0] != 42 {
		t.Fatalf("lock used handles %v", gotHandles)
	}
	called = nil
	gotHandles = nil
	err = ejectWindowsSequence(42, func(h windows.Handle, code uint32) error {
		gotHandles = append(gotHandles, h)
		called = append(called, ejectControlName(code))
		if code == 0x00090020 {
			return errDeviceBusy
		}
		return nil
	})
	if err == nil || len(called) != 2 || called[1] != "dismount" {
		t.Fatalf("dismount failure sequence = %v, error = %v", called, err)
	}
	if len(gotHandles) != 2 || gotHandles[0] != 42 || gotHandles[1] != 42 {
		t.Fatalf("dismount used handles %v", gotHandles)
	}
	called = nil
	gotHandles = nil
	if err = ejectWindowsSequence(42, func(h windows.Handle, code uint32) error {
		gotHandles = append(gotHandles, h)
		called = append(called, ejectControlName(code))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(called) != 3 || called[2] != "eject" || len(gotHandles) != 3 || gotHandles[2] != 42 {
		t.Fatalf("successful eject sequence=%v handles=%v", called, gotHandles)
	}
}

func ejectControlName(code uint32) string {
	switch code {
	case 0x00090018:
		return "lock"
	case 0x00090020:
		return "dismount"
	case 0x002D0800:
		return "eject"
	}
	return "unknown"
}

var errDeviceBusy = errors.New("device busy")
