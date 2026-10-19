//go:build darwin && !cgo

package app

// ReadClipboardFiles reports that native clipboard access requires cgo.
func ReadClipboardFiles() ([]string, error) { return nil, errClipboardUnsupported }
