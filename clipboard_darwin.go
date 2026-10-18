//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>
char *attune_read_clipboard_file_urls(void);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"unsafe"
)

// ReadClipboardFiles reads only local file URLs advertised by NSPasteboard.
func ReadClipboardFiles() ([]string, error) {
	serialized := C.attune_read_clipboard_file_urls()
	if serialized == nil {
		return nil, errClipboardUnsupported
	}
	defer C.free(unsafe.Pointer(serialized))
	var paths []string
	if err := json.Unmarshal([]byte(C.GoString(serialized)), &paths); err != nil {
		return nil, errors.New("could not read copied files from clipboard")
	}
	return validateClipboardPaths(paths)
}
