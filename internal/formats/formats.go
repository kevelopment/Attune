// Package formats separates storage, reading, transfer, and conversion support.
package formats

import (
	"path/filepath"
	"regexp"
	"strings"
)

var formatName = regexp.MustCompile(`^[a-z0-9][a-z0-9+_-]{0,15}$`)

// Normalize accepts a Calibre format name or a filename extension.
func Normalize(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if strings.HasPrefix(value, ".") {
		value = value[1:]
	}
	if strings.ContainsAny(value, `/\\`) {
		value = strings.TrimPrefix(filepath.Ext(value), ".")
	}
	if !formatName.MatchString(value) {
		return ""
	}
	return value
}

// CanStore is intentionally permissive: imported Calibre formats that Attune
// cannot render remain intact for later transfer or external opening.
func CanStore(value string) bool { return Normalize(value) != "" }

func CanRead(value string) bool {
	switch Normalize(value) {
	case "epub", "pdf", "mobi", "azw", "azw3", "fb2", "txt", "cbz", "cbr":
		return true
	default:
		return false
	}
}

// CanTransfer identifies common ebook formats suitable for device profiles.
// A profile may narrow this set further.
func CanTransfer(value string) bool {
	switch Normalize(value) {
	case "epub", "pdf", "mobi", "azw", "azw3", "prc", "fb2", "txt", "cbz", "cbr", "rtf", "doc", "docx", "odt", "html", "htmlz", "pdb", "lit", "zip":
		return true
	default:
		return false
	}
}

func CanConvert(source, target string) bool {
	return Normalize(source) == "epub" && Normalize(target) == "mobi"
}
