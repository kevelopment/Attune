//go:build darwin && !cgo

package main

// ReadClipboardFiles reports that native clipboard access requires cgo.
func ReadClipboardFiles() ([]string, error) { return nil, errClipboardUnsupported }
