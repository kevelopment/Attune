//go:build !darwin || !cgo

package main

// followSystemAppIcon is a no-op where the bundled icon cannot follow the system appearance.
func followSystemAppIcon() {}
