package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const maxArchiveFiles = 20000

func ExtractPackage(archive, destination, goos, goarch string) error {
	if goos == "linux" {
		return extractTarGzip(archive, destination, goarch)
	}
	return extractZip(archive, destination)
}

func extractZip(filename, destination string) error {
	r, err := zip.OpenReader(filename)
	if err != nil {
		return err
	}
	defer r.Close()
	if len(r.File) > maxArchiveFiles {
		return errors.New("update archive has too many entries")
	}
	seen := map[string]bool{}
	var total uint64
	for _, f := range r.File {
		name, err := safeArchivePath(f.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		if seen[name] {
			return fmt.Errorf("update archive repeats path %q", name)
		}
		seen[name] = true
		mode := f.Mode()
		if mode&os.ModeSymlink != 0 || mode&os.ModeType != 0 && !mode.IsDir() {
			return fmt.Errorf("update archive contains a non-regular entry: %s", name)
		}
		if f.UncompressedSize64 > uint64(maxDownloadBytes) {
			return errors.New("update archive entry is too large")
		}
		total += f.UncompressedSize64
		if total > uint64(maxDownloadBytes) {
			return errors.New("expanded update archive exceeds size limit")
		}
		if strings.HasPrefix(name, "__MACOSX/") || name == "__MACOSX" {
			continue
		}
		dst, err := archiveDestination(destination, name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		in, err := f.Open()
		if err != nil {
			return err
		}
		perm := os.FileMode(0o644)
		if mode&0o111 != 0 {
			perm = 0o755
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
		if err != nil {
			in.Close()
			return err
		}
		copied, copyErr := io.Copy(out, io.LimitReader(in, int64(f.UncompressedSize64)+1))
		closeIn := in.Close()
		closeOut := out.Close()
		if copyErr == nil && uint64(copied) != f.UncompressedSize64 {
			copyErr = errors.New("zip entry size differs from its header")
		}
		if copyErr != nil {
			return copyErr
		}
		if closeIn != nil {
			return closeIn
		}
		if closeOut != nil {
			return closeOut
		}
	}
	return nil
}

func extractTarGzip(filename, destination, goarch string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	prefix := "Attune-linux-" + goarch + "/"
	seen := map[string]bool{}
	var total int64
	for count := 0; ; count++ {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if count >= maxArchiveFiles {
			return errors.New("update archive has too many entries")
		}
		name, err := safeArchivePath(h.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		if name == strings.TrimSuffix(prefix, "/") {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			return fmt.Errorf("unexpected Linux package path %q", name)
		}
		name = strings.TrimPrefix(name, prefix)
		if name == "" {
			continue
		}
		if seen[name] {
			return fmt.Errorf("update archive repeats path %q", name)
		}
		seen[name] = true
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA && h.Typeflag != tar.TypeDir {
			return fmt.Errorf("update archive contains a link or special file: %s", name)
		}
		if h.Size < 0 || h.Size > maxDownloadBytes {
			return errors.New("update archive entry is too large")
		}
		total += h.Size
		if total > maxDownloadBytes {
			return errors.New("expanded update archive exceeds size limit")
		}
		dst, err := archiveDestination(destination, name)
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		perm := os.FileMode(0o644)
		if h.Mode&0o111 != 0 {
			perm = 0o755
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(out, tr, h.Size)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func safeArchivePath(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\\') || strings.ContainsRune(name, ':') || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("unsafe path in update archive: %q", name)
	}
	for _, part := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
		if part == ".." {
			return "", fmt.Errorf("path traverses update archive: %q", name)
		}
	}
	clean := path.Clean(name)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path escapes update archive: %q", name)
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".." || part == "" {
			return "", fmt.Errorf("unsafe path in update archive: %q", name)
		}
	}
	return clean, nil
}

func archiveDestination(root, name string) (string, error) {
	dst := filepath.Join(root, filepath.FromSlash(name))
	rel, err := filepath.Rel(root, dst)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes update staging directory: %q", name)
	}
	return dst, nil
}

func ValidatePackage(root, goos string) error {
	var required []string
	switch goos {
	case "darwin":
		required = []string{"Attune.app/Contents/MacOS/Attune", "Attune.app/Contents/Resources/kindling-cli", "Attune.app/Contents/Resources/licenses/Attune-LICENSE.txt", "Attune.app/Contents/Resources/licenses/THIRD_PARTY_NOTICES.md", "Attune.app/Contents/Resources/licenses/THIRD_PARTY_LICENSES.txt", "Attune.app/Contents/Resources/licenses/Kindling-LICENSE.txt", "Attune.app/Contents/Resources/licenses/Kindling-THIRD_PARTY_LICENSES.txt"}
	case "windows":
		required = []string{"Attune.exe", "kindling-cli.exe", "INSTALL.txt", "licenses/Attune-LICENSE.txt", "licenses/THIRD_PARTY_NOTICES.md", "licenses/THIRD_PARTY_LICENSES.txt", "licenses/Kindling-LICENSE.txt", "licenses/Kindling-THIRD_PARTY_LICENSES.txt"}
	case "linux":
		required = []string{"Attune", "kindling-cli", "INSTALL.txt", "licenses/Attune-LICENSE.txt", "licenses/THIRD_PARTY_NOTICES.md", "licenses/THIRD_PARTY_LICENSES.txt", "licenses/Kindling-LICENSE.txt", "licenses/Kindling-THIRD_PARTY_LICENSES.txt"}
	default:
		return fmt.Errorf("unsupported update platform %q", goos)
	}
	for _, name := range required {
		st, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return fmt.Errorf("update package is missing %s: %w", name, err)
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("update package entry is not a regular file: %s", name)
		}
	}
	return nil
}
