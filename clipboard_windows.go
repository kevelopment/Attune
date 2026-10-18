//go:build windows

package main

import (
	"errors"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const cfHDrop = 15

var (
	clipboardUser32  = windows.NewLazySystemDLL("user32.dll")
	clipboardShell32 = windows.NewLazySystemDLL("shell32.dll")
	openClipboard    = clipboardUser32.NewProc("OpenClipboard")
	closeClipboard   = clipboardUser32.NewProc("CloseClipboard")
	getClipboardData = clipboardUser32.NewProc("GetClipboardData")
	dragQueryFileW   = clipboardShell32.NewProc("DragQueryFileW")
)

// ReadClipboardFiles reads CF_HDROP paths. It never requests CF_UNICODETEXT.
func ReadClipboardFiles() ([]string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := openClipboard.Find(); err != nil {
		return nil, errClipboardUnsupported
	}
	ok, _, _ := openClipboard.Call(0)
	if ok == 0 {
		return nil, errClipboardUnsupported
	}
	defer closeClipboard.Call()

	hdrop, _, _ := getClipboardData.Call(cfHDrop)
	if hdrop == 0 {
		return nil, errClipboardUnsupported
	}
	count, _, _ := dragQueryFileW.Call(hdrop, 0xffffffff, 0, 0)
	if count > maxClipboardFiles {
		return nil, errors.New("clipboard contains more than 500 copied files")
	}
	paths := make([]string, 0, int(count))
	for i := uintptr(0); i < count; i++ {
		length, _, _ := dragQueryFileW.Call(hdrop, i, 0, 0)
		if length > 32767 {
			return nil, errClipboardUnsupported
		}
		buffer := make([]uint16, int(length)+1)
		written, _, _ := dragQueryFileW.Call(hdrop, i, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
		if written == 0 {
			return nil, errClipboardUnsupported
		}
		path := syscall.UTF16ToString(buffer)
		// UNC paths refer to remote shares, not local copied files.
		if strings.HasPrefix(path, `\`) && !strings.HasPrefix(path, `\?`) {
			return nil, errClipboardUnsupported
		}
		paths = append(paths, path)
	}
	return validateClipboardPaths(paths)
}
