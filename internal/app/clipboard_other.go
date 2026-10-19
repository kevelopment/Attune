//go:build !darwin && !windows && !linux

package app

// ReadClipboardFiles returns copied local file paths from the OS clipboard.
func ReadClipboardFiles() ([]string, error) { return nil, errClipboardUnsupported }
