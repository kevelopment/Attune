package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestExtractAndValidateSupportedPackageLayouts(t *testing.T) {
	for _, tc := range []struct {
		goos, goarch string
		files        []string
	}{
		{"windows", "amd64", []string{"Attune.exe", "kindling-cli.exe", "INSTALL.txt", "licenses/Attune-LICENSE.txt", "licenses/THIRD_PARTY_NOTICES.md", "licenses/THIRD_PARTY_LICENSES.txt", "licenses/Kindling-LICENSE.txt", "licenses/Kindling-THIRD_PARTY_LICENSES.txt"}},
		{"darwin", "arm64", []string{"Attune.app/Contents/MacOS/Attune", "Attune.app/Contents/Resources/kindling-cli", "Attune.app/Contents/Resources/licenses/Attune-LICENSE.txt", "Attune.app/Contents/Resources/licenses/THIRD_PARTY_NOTICES.md", "Attune.app/Contents/Resources/licenses/THIRD_PARTY_LICENSES.txt", "Attune.app/Contents/Resources/licenses/Kindling-LICENSE.txt", "Attune.app/Contents/Resources/licenses/Kindling-THIRD_PARTY_LICENSES.txt"}},
		{"linux", "amd64", []string{"Attune", "kindling-cli", "INSTALL.txt", "licenses/Attune-LICENSE.txt", "licenses/THIRD_PARTY_NOTICES.md", "licenses/THIRD_PARTY_LICENSES.txt", "licenses/Kindling-LICENSE.txt", "licenses/Kindling-THIRD_PARTY_LICENSES.txt"}},
	} {
		t.Run(tc.goos+"-"+tc.goarch, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "package.archive")
			if tc.goos == "linux" {
				writeTarGzip(t, archive, tc.goarch, tc.files, nil)
			} else {
				writeZip(t, archive, tc.files, nil)
			}
			dest := filepath.Join(t.TempDir(), "staged")
			if err := ExtractPackage(archive, dest, tc.goos, tc.goarch); err != nil {
				t.Fatal(err)
			}
			if err := ValidatePackage(dest, tc.goos); err != nil {
				t.Fatalf("valid package rejected: %v", err)
			}
		})
	}
}

func TestZipExtractionRejectsTraversalSymlinkDuplicateAndOversize(t *testing.T) {
	cases := []struct {
		name     string
		entries  []zipFixtureEntry
		oversize bool
	}{
		{"traversal", []zipFixtureEntry{{name: "../escape.txt", data: "x"}}, false},
		{"symlink", []zipFixtureEntry{{name: "link", data: "target", mode: os.ModeSymlink | 0o777}}, false},
		{"duplicate", []zipFixtureEntry{{name: "one", data: "a"}, {name: "one", data: "b"}}, false},
		{"oversize", []zipFixtureEntry{{name: "large", data: "x"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, "input.zip")
			writeZip(t, archive, nil, tc.entries)
			if tc.oversize {
				patchZipSize(t, archive, uint32(maxDownloadBytes+1))
			}
			dest := filepath.Join(root, "out")
			if err := ExtractPackage(archive, dest, "windows", "amd64"); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			if _, err := os.Stat(filepath.Join(root, "escape.txt")); !os.IsNotExist(err) {
				t.Fatalf("archive wrote outside staging: %v", err)
			}
		})
	}
}

func TestZipExtractionRejectsCaseAliasesOnWindows(t *testing.T) {
	root := t.TempDir()
	probe := filepath.Join(root, "case-probe")
	if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, caseInsensitiveErr := os.Stat(filepath.Join(root, "CASE-PROBE"))
	caseInsensitive := caseInsensitiveErr == nil
	archive := filepath.Join(root, "case.zip")
	writeZip(t, archive, nil, []zipFixtureEntry{{name: "Attune.exe", data: "first"}, {name: "attune.exe", data: "second"}})
	err := ExtractPackage(archive, filepath.Join(root, "out"), "windows", "amd64")
	if caseInsensitive && err == nil {
		t.Fatal("case-colliding paths accepted on case-insensitive filesystem")
	}
	if !caseInsensitive && err != nil {
		t.Fatalf("case-distinct paths rejected on case-sensitive filesystem: %v", err)
	}
}

func TestZipExtractionEnforcesEntryCountLimit(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "many.zip")
	entries := make([]zipFixtureEntry, maxArchiveFiles+1)
	for i := range entries {
		entries[i] = zipFixtureEntry{name: filepath.Join("files", strings.Repeat("x", 0)+strconv.Itoa(i)), data: ""}
	}
	writeZip(t, archive, nil, entries)
	if err := ExtractPackage(archive, filepath.Join(root, "out"), "windows", "amd64"); err == nil {
		t.Fatal("archive over the entry-count limit accepted")
	}
}

func TestTarExtractionRejectsTraversalSymlinkHardlinkAndDuplicate(t *testing.T) {
	cases := []struct {
		name    string
		entries []tarFixtureEntry
	}{
		{"traversal", []tarFixtureEntry{{name: "../../escape.txt", typeflag: tar.TypeReg, data: "x"}}},
		{"symlink", []tarFixtureEntry{{name: "link", typeflag: tar.TypeSymlink, link: "target"}}},
		{"hardlink", []tarFixtureEntry{{name: "link", typeflag: tar.TypeLink, link: "target"}}},
		{"duplicate", []tarFixtureEntry{{name: "same", typeflag: tar.TypeReg, data: "a"}, {name: "same", typeflag: tar.TypeReg, data: "b"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, "input.tar.gz")
			writeTarGzip(t, archive, "amd64", nil, tc.entries)
			if err := ExtractPackage(archive, filepath.Join(root, "out"), "linux", "amd64"); err == nil {
				t.Fatal("unsafe tar entry accepted")
			}
			if _, err := os.Stat(filepath.Join(root, "escape.txt")); !os.IsNotExist(err) {
				t.Fatalf("archive wrote outside staging: %v", err)
			}
		})
	}
}

func TestTarExtractionRejectsOversizeEntryHeader(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "oversize.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	w := tar.NewWriter(gz)
	if err := w.WriteHeader(&tar.Header{Name: "Attune-linux-amd64/large", Mode: 0o644, Size: maxDownloadBytes + 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	// The extractor should reject the oversized header before reading payload bytes.
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ExtractPackage(archive, filepath.Join(root, "out"), "linux", "amd64"); err == nil {
		t.Fatal("oversized tar entry accepted")
	}
}

type zipFixtureEntry struct {
	name, data string
	mode       os.FileMode
}

func writeZip(t *testing.T, filename string, files []string, extra []zipFixtureEntry) {
	t.Helper()
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entries := append([]zipFixtureEntry(nil), extra...)
	for _, name := range files {
		entries = append(entries, zipFixtureEntry{name: name, data: "package file"})
	}
	for _, entry := range entries {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		if entry.mode != 0 {
			h.SetMode(entry.mode)
		}
		writer, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

type tarFixtureEntry struct {
	name, data, link string
	typeflag         byte
}

func writeTarGzip(t *testing.T, filename, arch string, files []string, extra []tarFixtureEntry) {
	t.Helper()
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	w := tar.NewWriter(gz)
	entries := append([]tarFixtureEntry(nil), extra...)
	for _, name := range files {
		entries = append(entries, tarFixtureEntry{name: name, typeflag: tar.TypeReg, data: "package file"})
	}
	if len(entries) == 0 {
		entries = append(entries, tarFixtureEntry{name: "placeholder", typeflag: tar.TypeReg, data: "x"})
	}
	for _, entry := range entries {
		name := entry.name
		if !strings.HasPrefix(name, "Attune-linux-") {
			name = "Attune-linux-" + arch + "/" + name
		}
		kind := entry.typeflag
		if kind == 0 {
			kind = tar.TypeReg
		}
		h := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(entry.data)), Typeflag: kind, Linkname: entry.link}
		if kind == tar.TypeSymlink || kind == tar.TypeLink {
			h.Size = 0
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			if _, err := w.Write([]byte(entry.data)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func patchZipSize(t *testing.T, filename string, size uint32) {
	t.Helper()
	b, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	idx := bytes.LastIndex(b, []byte{'P', 'K', 1, 2})
	if idx < 0 || idx+28 > len(b) {
		t.Fatal("zip central directory not found")
	}
	binary.LittleEndian.PutUint32(b[idx+24:idx+28], size)
	if err := os.WriteFile(filename, b, 0o600); err != nil {
		t.Fatal(err)
	}
}
