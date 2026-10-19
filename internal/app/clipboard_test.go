//go:build linux || darwin

package app

import (
	"strings"
	"testing"
)

func TestParseClipboardFileList(t *testing.T) {
	got, err := parseClipboardFileList("copy\nfile:///tmp/one%20book.epub\n# comment\nfile://localhost/tmp/two.epub\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/tmp/one book.epub", "/tmp/two.epub"}
	if len(got) != len(want) {
		t.Fatalf("paths = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paths = %#v, want %#v", got, want)
		}
	}
}

func TestParseClipboardFileListRejectsTextAndRemoteURLs(t *testing.T) {
	for _, input := range []string{"some copied text", "https://example.test/book.epub", "file://server/share/book.epub", "file:relative.epub"} {
		if _, err := parseClipboardFileList(input); err == nil {
			t.Errorf("accepted clipboard data %q", input)
		}
	}
}

func TestParseClipboardFileListCapsCount(t *testing.T) {
	data := strings.Repeat("file:///tmp/book.epub\n", maxClipboardFiles+1)
	if _, err := parseClipboardFileList(data); err == nil {
		t.Fatal("accepted more than 500 copied files")
	}
}
