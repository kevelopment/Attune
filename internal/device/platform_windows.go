//go:build windows

package device

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

type windowsVolume struct {
	Root        string `json:"Root"`
	ID          string `json:"ID"`
	Name        string `json:"Name"`
	Device      string `json:"Device"`
	MountDevice string `json:"MountDevice"`
	External    bool   `json:"External"`
	IsBoot      bool   `json:"IsBoot"`
	IsSystem    bool   `json:"IsSystem"`
}

func exclusiveRename(temp, target string) error {
	from, err := windows.UTF16PtrFromString(temp)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	// MOVEFILE_REPLACE_EXISTING is deliberately omitted.
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}

func volumeSpace(root string) (uint64, uint64, error) {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0, 0, err
	}
	var free, total, totalFree uint64
	if err = windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return 0, 0, err
	}
	return free, total, nil
}

// PowerShell uses fixed CIM queries; no user-provided text enters the command.
const windowsVolumesScript = `$ErrorActionPreference='Stop'; $items=foreach($v in Get-Volume | Where-Object {$_.DriveLetter -and $_.UniqueId}) { try { $p=Get-Partition -DriveLetter $v.DriveLetter -ErrorAction Stop; $d=Get-Disk -Number $p.DiskNumber -ErrorAction Stop; if($d.BusType -eq 'USB' -or $d.IsRemovable) { $serial=([string]$d.SerialNumber).Trim(); $diskId=([string]$d.UniqueId).Trim(); $vid=([string]$v.UniqueId).Trim(); if($vid -and ($serial -or $diskId)) { [pscustomobject]@{Root=([string]$v.DriveLetter+':\'); ID=('windows:'+$vid+':'+$(if($serial){$serial}else{$diskId})); Name=([string]$v.FileSystemLabel); Device=('\\.\PhysicalDrive'+$d.Number); MountDevice=('\\.\'+$v.DriveLetter+':'); External=$true; IsBoot=[bool]$d.IsBoot; IsSystem=[bool]$d.IsSystem} } } } catch {} }; ConvertTo-Json -InputObject @($items) -Compress -Depth 3`

func mountedVolumes() ([]volume, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", windowsVolumesScript).Output()
	if err != nil {
		return nil, fmt.Errorf("enumerate USB volumes with Windows storage metadata: %w", err)
	}
	if len(b) > maxCommandOutput {
		return nil, errors.New("Windows volume output exceeded limit")
	}
	return parseWindowsVolumes(b)
}

func parseWindowsVolumes(b []byte) ([]volume, error) {
	var rows []windowsVolume
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, fmt.Errorf("parse Windows volume metadata: %w", err)
	}
	out := make([]volume, 0, len(rows))
	for _, r := range rows {
		root := filepath.Clean(r.Root)
		if !r.External || r.IsBoot || r.IsSystem || !validWindowsRoot(root) || r.ID == "" || !validPhysicalDrivePath(r.Device) || !strings.EqualFold(r.MountDevice, `\\.\`+root[:1]+":") {
			continue
		}
		out = append(out, volume{Root: root, ID: r.ID, Name: first(r.Name, root), Device: r.Device, MountDevice: r.MountDevice, External: true})
	}
	return out, nil
}

func validWindowsRoot(root string) bool {
	return len(root) == 3 && root[1] == ':' && root[2] == '\\' && root[0] >= 'A' && root[0] <= 'Z'
}

func validPhysicalDrivePath(path string) bool {
	const prefix = `\\.\PhysicalDrive`
	if !strings.HasPrefix(strings.ToLower(path), strings.ToLower(prefix)) || len(path) == len(prefix) {
		return false
	}
	for _, r := range path[len(prefix):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func ejectVolume(ctx context.Context, v volume, _ func(context.Context, string, ...string) ([]byte, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validWindowsRoot(filepath.Clean(v.Root)) || !validPhysicalDrivePath(v.Device) || !strings.EqualFold(v.MountDevice, `\\.\`+v.Root[:1]+":") {
		return errors.New("validated Windows volume path is unavailable")
	}
	volumePath, err := windows.UTF16PtrFromString(v.MountDevice)
	if err != nil {
		return err
	}
	volume, err := windows.CreateFile(volumePath, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return fmt.Errorf("open volume for safe removal (check user permissions): %w", err)
	}
	defer windows.CloseHandle(volume)
	return ejectWindowsSequence(volume, controlVolume)
}

func ejectWindowsSequence(handle windows.Handle, control func(windows.Handle, uint32) error) error {
	if err := control(handle, 0x00090018); err != nil {
		return fmt.Errorf("lock volume for safe removal (close files using the device): %w", err)
	}
	if err := control(handle, 0x00090020); err != nil {
		return fmt.Errorf("dismount volume for safe removal: %w", err)
	}
	if err := control(handle, 0x002D0800); err != nil {
		return fmt.Errorf("eject USB media (check user permissions): %w", err)
	}
	return nil
}

func controlVolume(h windows.Handle, code uint32) error {
	var returned uint32
	return windows.DeviceIoControl(h, code, nil, 0, nil, 0, &returned, nil)
}
