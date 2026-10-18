//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework AppKit
void attune_follow_system_app_icon(const void *dark, int dark_len, const void *light, int light_len);
*/
import "C"

import (
	_ "embed"
	"unsafe"
)

//go:embed build/appicon.png
var appIconDark []byte

//go:embed build/appicon-light.png
var appIconLight []byte

// followSystemAppIcon keeps the Dock icon in step with the macOS light/dark appearance.
func followSystemAppIcon() {
	C.attune_follow_system_app_icon(unsafe.Pointer(&appIconDark[0]), C.int(len(appIconDark)), unsafe.Pointer(&appIconLight[0]), C.int(len(appIconLight)))
}
