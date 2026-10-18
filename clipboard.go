package main

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

const maxClipboardFiles = 500

var errClipboardUnsupported = errors.New("clipboard does not contain copied files or file clipboard access is unsupported")

// parseClipboardFileList parses the URI formats used by file managers and
// returns local filesystem paths. It never treats clipboard text as a path.
func parseClipboardFileList(data string) ([]string, error) {
	lines := strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	if len(lines) > 0 && (strings.EqualFold(strings.TrimSpace(lines[0]), "copy") || strings.EqualFold(strings.TrimSpace(lines[0]), "cut")) {
		lines = lines[1:]
	}
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || !strings.EqualFold(u.Scheme, "file") || (u.Host != "" && !strings.EqualFold(u.Host, "localhost")) || u.RawQuery != "" || u.Fragment != "" {
			return nil, errClipboardUnsupported
		}
		path, err := url.PathUnescape(u.EscapedPath())
		if err != nil || path == "" {
			return nil, errClipboardUnsupported
		}
		path = filepath.FromSlash(path)
		if !filepath.IsAbs(path) {
			return nil, errClipboardUnsupported
		}
		paths = append(paths, path)
		if len(paths) > maxClipboardFiles {
			return nil, errors.New("clipboard contains more than 500 copied files")
		}
	}
	return paths, nil
}

func validateClipboardPaths(paths []string) ([]string, error) {
	if len(paths) > maxClipboardFiles {
		return nil, errors.New("clipboard contains more than 500 copied files")
	}
	clean := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" || strings.Contains(path, "://") || !filepath.IsAbs(path) {
			return nil, errClipboardUnsupported
		}
		clean = append(clean, filepath.Clean(path))
	}
	return clean, nil
}
