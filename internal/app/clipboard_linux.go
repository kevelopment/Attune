//go:build linux

package app

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

const maxClipboardOutput = 1 << 20

// ReadClipboardFiles returns file paths from the desktop's copied-file
// clipboard formats. It does not read file contents or plain clipboard text.
func ReadClipboardFiles() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if path, err := exec.LookPath("wl-paste"); err == nil {
		formats, runErr := runClipboardCommand(ctx, path, "--list-types")
		if runErr == nil && containsLine(formats, "text/uri-list") {
			data, err := runClipboardCommand(ctx, path, "--no-newline", "--type", "text/uri-list")
			if err != nil {
				return nil, errClipboardUnsupported
			}
			return parseClipboardFileList(data)
		}
	}
	if path, err := exec.LookPath("xclip"); err == nil {
		formats, runErr := runClipboardCommand(ctx, path, "-selection", "clipboard", "-t", "TARGETS", "-o")
		if runErr == nil && containsLine(formats, "text/uri-list") {
			data, err := runClipboardCommand(ctx, path, "-selection", "clipboard", "-t", "text/uri-list", "-o")
			if err != nil {
				return nil, errClipboardUnsupported
			}
			return parseClipboardFileList(data)
		}
	}
	return nil, errClipboardUnsupported
}

func runClipboardCommand(ctx context.Context, binary string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	var output limitedClipboardBuffer
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil || output.exceeded {
		return "", errors.New("clipboard command failed")
	}
	return output.String(), nil
}

type limitedClipboardBuffer struct {
	bytes.Buffer
	exceeded bool
}

func (b *limitedClipboardBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxClipboardOutput {
		b.exceeded = true
		return 0, errors.New("clipboard output too large")
	}
	return b.Buffer.Write(p)
}

func containsLine(data, expected string) bool {
	for _, line := range strings.Fields(data) {
		if strings.EqualFold(strings.TrimSpace(line), expected) {
			return true
		}
	}
	return false
}
