// Package device discovers mounted Kindle volumes and transfers books to them.
package device

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"attune/internal/model"
)

const maxCommandOutput = 1 << 20

type volume struct {
	Root, ID, Name, Device, MountDevice string
	External                            bool
}

// Manager accesses mounted volumes. Its unexported hooks keep production
// selection tied to diskutil while allowing filesystem-only tests.
type Manager struct {
	volumes func() ([]volume, error)
	space   func(string) (uint64, uint64, error)
	copy    func(context.Context, string, string) (string, error)
	command func(context.Context, string, ...string) ([]byte, error)
	metaMu  sync.Mutex
	meta    map[metadataCacheKey]deviceMetadata
}

func NewManager() *Manager {
	m := &Manager{volumes: mountedVolumes, space: volumeSpace, copy: copyVerified, command: runCommand}
	return m
}

func (m *Manager) init() {
	if m.volumes == nil {
		defaults := NewManager()
		m.volumes, m.space, m.copy, m.command = defaults.volumes, defaults.space, defaults.copy, defaults.command
	}
}

// Discover returns detected external Kindle volumes only.
func (m *Manager) Discover() ([]model.Device, error) {
	m.init()
	vols, err := m.volumes()
	if err != nil {
		return nil, err
	}
	out := make([]model.Device, 0)
	for _, v := range vols {
		if !v.External || !looksLikeKindle(v) {
			continue
		}
		free, total, e := m.space(v.Root)
		if e != nil {
			continue
		}
		out = append(out, model.Device{ID: v.ID, Name: displayName(v), Root: v.Root, FreeBytes: free, TotalBytes: total})
	}
	return out, nil
}

// SelectVolume validates an explicit user-selected mount. External USB volumes
// with a Documents directory are accepted even when their Kindle markers are unknown.
func (m *Manager) SelectVolume(root string) (model.Device, error) {
	m.init()
	root, err := filepath.Abs(root)
	if err != nil {
		return model.Device{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		if os.IsNotExist(err) {
			return model.Device{}, errors.New("Kindle disconnected; reconnect it and try again")
		}
		return model.Device{}, fmt.Errorf("resolve selected volume: %w", err)
	}
	vols, err := m.volumes()
	if err != nil {
		return model.Device{}, err
	}
	for _, v := range vols {
		vr, e := filepath.EvalSymlinks(v.Root)
		if e != nil {
			continue
		}
		if filepath.Clean(vr) != filepath.Clean(root) {
			continue
		}
		if !v.External {
			return model.Device{}, errors.New("selected volume is internal; choose an external USB volume")
		}
		if !looksLikeKindle(v) && !isDirNoSymlink(filepath.Join(root, "documents")) && !isDirNoSymlink(filepath.Join(root, "Documents")) {
			return model.Device{}, errors.New("selected external volume has no Kindle markers or Documents folder")
		}
		free, total, e := m.space(root)
		if e != nil {
			return model.Device{}, e
		}
		return model.Device{ID: v.ID, Name: displayName(v), Root: root, FreeBytes: free, TotalBytes: total}, nil
	}
	return model.Device{}, errors.New("selected path is not a mounted external volume")
}

func (m *Manager) selectExpected(expected model.Device) (model.Device, error) {
	if expected.ID == "" || expected.Root == "" {
		return model.Device{}, errors.New("expected device identity is required")
	}
	d, err := m.SelectVolume(expected.Root)
	if err != nil {
		return model.Device{}, err
	}
	if d.ID != expected.ID {
		return model.Device{}, errors.New("device identity changed since selection")
	}
	return d, nil
}

// List lists supported ebook files in the volume's Documents directory.
func (m *Manager) List(expected model.Device) ([]model.DeviceBook, error) {
	d, err := m.selectExpected(expected)
	if err != nil {
		return nil, err
	}
	docs, err := documentsPath(d.Root)
	if err != nil {
		return nil, err
	}
	return listBooks(docs, d.Root, d.ID, m)
}

func listBooks(docs, root, volumeID string, m *Manager) ([]model.DeviceBook, error) {
	books := make([]model.DeviceBook, 0)
	seenMetadata := make(map[metadataCacheKey]struct{})
	err := filepath.WalkDir(docs, func(p string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".sdr") {
			return filepath.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !ebookExt(entry.Name()) {
			return nil
		}
		st, e := entry.Info()
		if e != nil || !st.Mode().IsRegular() {
			return nil
		}
		modifiedAt, addedAt := deviceFileTimes(st)
		rel, e := filepath.Rel(root, p)
		if e == nil {
			meta := deviceMetadata{title: strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))}
			if m != nil {
				meta = m.readMetadata(volumeID, filepath.ToSlash(rel), p, st)
				seenMetadata[metadataCacheKey{volumeID: volumeID, path: filepath.ToSlash(rel), size: st.Size(), mtime: st.ModTime().UnixNano()}] = struct{}{}
			}
			books = append(books, model.DeviceBook{
				Path: filepath.ToSlash(rel), Name: entry.Name(), Size: st.Size(), Title: meta.title,
				Authors: meta.authors, Language: meta.language, Subjects: meta.subjects, CoverURL: meta.coverURL,
				Publisher: meta.publisher, Published: meta.published, Identifiers: meta.identifiers, Description: meta.description,
				ModifiedAt: modifiedAt, AddedAt: addedAt,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if m != nil {
		m.pruneMetadata(volumeID, seenMetadata)
	}
	return books, nil
}

func (m *Manager) pruneMetadata(volumeID string, seen map[metadataCacheKey]struct{}) {
	m.metaMu.Lock()
	defer m.metaMu.Unlock()
	for key := range m.meta {
		if key.volumeID == volumeID {
			if _, ok := seen[key]; !ok {
				delete(m.meta, key)
			}
		}
	}
}

// Send copies an ebook to Documents using a temporary sibling, verifies the
// bytes, rechecks the mounted volume identity, then commits the final path.
func (m *Manager) Send(ctx context.Context, expected model.Device, source, filename string) (string, error) {
	m.init()
	d, err := m.selectExpected(expected)
	if err != nil {
		return "", err
	}
	if !ebookExt(filename) || filepath.Base(filename) != filename || filename == "." || filename == ".." {
		return "", errors.New("filename must be a simple EPUB or MOBI filename")
	}
	src, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("source must be a regular file")
	}
	free, _, err := m.space(d.Root)
	if err != nil {
		return "", err
	}
	if uint64(st.Size()) > free {
		return "", fmt.Errorf("not enough free space: need %d bytes, have %d", st.Size(), free)
	}
	docs, err := documentsPath(d.Root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(docs, filename)
	if _, err = os.Lstat(target); err == nil {
		return "", fmt.Errorf("refusing to overwrite existing device file %q", filename)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	tmp, err := os.CreateTemp(docs, ".ebook-transfer-*.tmp")
	if err != nil {
		return "", err
	}
	temp := tmp.Name()
	tmp.Close()
	defer os.Remove(temp)
	digest, err := m.copy(ctx, source, temp)
	if err != nil {
		return "", err
	}
	verified, err := hashFile(temp)
	if err != nil {
		return "", err
	}
	if verified != digest {
		return "", errors.New("copied file hash verification failed")
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	current, err := m.findVolume(d.Root)
	if err != nil {
		return "", err
	}
	if current.ID != expected.ID {
		return "", errors.New("device identity changed during transfer")
	}
	if err = commitTransfer(ctx, temp, target, digest); err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("refusing to overwrite existing device file %q", filename)
		}
		return "", err
	}
	return filepath.ToSlash(filepath.Join("documents", filename)), nil
}

func commitTransfer(ctx context.Context, temp, target, expectedHash string) error {
	return commitTransferWith(ctx, temp, target, expectedHash, func() error {
		return exclusiveRename(temp, target)
	})
}

func commitTransferWith(ctx context.Context, temp, target, expectedHash string, exclusiveRename func() error) error {
	err := exclusiveRename()
	if err == nil || !errors.Is(err, errors.ErrUnsupported) {
		return err
	}
	// Some removable FAT drivers lack RENAME_EXCL. Open the final name
	// exclusively and copy the already verified temp file into it. This keeps
	// no-clobber behavior on those volumes, though the final name is visible
	// while the fallback copy runs.
	return copyToExclusiveTarget(ctx, temp, target, expectedHash)
}

func copyToExclusiveTarget(ctx context.Context, source, target, expectedHash string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	owned, err := out.Stat()
	if err != nil {
		out.Close()
		os.Remove(target)
		return err
	}
	complete := false
	defer func() {
		if !complete {
			out.Close()
			current, se := os.Lstat(target)
			if se == nil && os.SameFile(owned, current) {
				_ = os.Remove(target)
			}
		}
	}()
	h := sha256.New()
	buf := make([]byte, 128*1024)
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		n, readErr := in.Read(buf)
		if n > 0 {
			if _, err = out.Write(buf[:n]); err != nil {
				return err
			}
			_, _ = h.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != expectedHash {
		return errors.New("fallback transfer hash verification failed")
	}
	complete = true
	return nil
}

// Remove only removes a direct child ebook file in Documents, never sidecars.
func (m *Manager) Remove(expected model.Device, relativePath string) error {
	d, err := m.selectExpected(expected)
	if err != nil {
		return err
	}
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if filepath.IsAbs(clean) || clean == "." || !ebookExt(filepath.Base(clean)) {
		return errors.New("removal is limited to ebook files inside Documents")
	}
	docs, err := documentsPath(d.Root)
	if err != nil {
		return err
	}
	parts := strings.Split(clean, string(filepath.Separator))
	if len(parts) < 2 || !strings.EqualFold(parts[0], filepath.Base(docs)) {
		return errors.New("removal path is outside Documents")
	}
	inside := filepath.Join(parts[1:]...)
	p := filepath.Join(docs, inside)
	// Check every ancestor without following symlinks before removing.
	cur := docs
	for _, part := range parts[1 : len(parts)-1] {
		cur = filepath.Join(cur, part)
		st, e := os.Lstat(cur)
		if e != nil {
			return e
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return errors.New("refusing to traverse a symlink or non-directory")
		}
	}
	st, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to remove non-regular or symlink file")
	}
	current, err := m.findVolume(d.Root)
	if err != nil {
		return err
	}
	if current.ID != expected.ID {
		return errors.New("device identity changed before removal")
	}
	return os.Remove(p)
}

// Eject safely removes a validated mounted external volume.
func (m *Manager) Eject(ctx context.Context, expected model.Device) error {
	m.init()
	d, err := m.selectExpected(expected)
	if err != nil {
		return err
	}
	current, err := m.findVolume(d.Root)
	if err != nil {
		return err
	}
	if current.ID != expected.ID {
		return errors.New("device identity changed before eject")
	}
	if err = ejectVolume(ctx, current, m.command); err != nil {
		return fmt.Errorf("eject %s: %w", d.Name, err)
	}
	return nil
}

func (m *Manager) findVolume(root string) (volume, error) {
	vols, err := m.volumes()
	if err != nil {
		return volume{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		if os.IsNotExist(err) {
			return volume{}, errors.New("Kindle disconnected; reconnect it and try again")
		}
		return volume{}, err
	}
	for _, v := range vols {
		vr, e := filepath.EvalSymlinks(v.Root)
		if e == nil && filepath.Clean(vr) == filepath.Clean(root) && v.External {
			return v, nil
		}
	}
	return volume{}, errors.New("device was disconnected or is no longer an external mounted volume")
}
func displayName(v volume) string {
	if v.Name != "" {
		return v.Name
	}
	return filepath.Base(v.Root)
}
func looksLikeKindle(v volume) bool {
	n := strings.ToLower(v.Name)
	if strings.Contains(n, "kindle") || strings.Contains(n, "amazon") {
		return true
	}
	marker := filepath.Join(v.Root, "documents/.active-content-data")
	if st, e := os.Lstat(marker); e == nil && st.Mode().IsRegular() && st.Mode()&os.ModeSymlink == 0 {
		return true
	}
	versionPath := filepath.Join(v.Root, "system/version.txt")
	st, err := os.Lstat(versionPath)
	if err == nil && st.Mode().IsRegular() && st.Mode()&os.ModeSymlink == 0 {
		f, e := os.Open(versionPath)
		if e == nil {
			defer f.Close()
			b, _ := io.ReadAll(io.LimitReader(f, 4096))
			return strings.Contains(strings.ToLower(string(b)), "kindle")
		}
	}
	return false
}
func isDirNoSymlink(p string) bool {
	s, e := os.Lstat(p)
	return e == nil && s.IsDir() && s.Mode()&os.ModeSymlink == 0
}
func documentsPath(root string) (string, error) {
	for _, n := range []string{"documents", "Documents"} {
		p := filepath.Join(root, n)
		if isDirNoSymlink(p) {
			return p, nil
		}
		if _, e := os.Lstat(p); e == nil {
			return "", errors.New("Documents path is not a real directory")
		}
	}
	return "", errors.New("device Documents directory is missing")
}
func ebookExt(n string) bool {
	switch strings.ToLower(filepath.Ext(n)) {
	case ".mobi", ".prc", ".azw", ".azw3", ".epub":
		return true
	}
	return false
}

func copyVerified(ctx context.Context, source, target string) (string, error) {
	in, e := os.Open(source)
	if e != nil {
		return "", e
	}
	defer in.Close()
	out, e := os.OpenFile(target, os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return "", e
	}
	h := sha256.New()
	buf := make([]byte, 128*1024)
	for {
		if e = ctx.Err(); e != nil {
			out.Close()
			return "", e
		}
		n, re := in.Read(buf)
		if n > 0 {
			if _, e = out.Write(buf[:n]); e != nil {
				out.Close()
				return "", e
			}
			h.Write(buf[:n])
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			out.Close()
			return "", re
		}
	}
	if e = out.Sync(); e != nil {
		out.Close()
		return "", e
	}
	if e = out.Close(); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func hashFile(p string) (string, error) {
	f, e := os.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, e := cmd.Output()
	if len(out) > maxCommandOutput {
		return nil, errors.New("command output exceeded limit")
	}
	if e != nil {
		return out, fmt.Errorf("%s: %w", name, e)
	}
	return out, nil
}

func first(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
func readPlistStrings(b []byte) map[string]string {
	out := map[string]string{}
	d := xml.NewDecoder(strings.NewReader(string(b)))
	key := ""
	for {
		t, e := d.Token()
		if e != nil {
			break
		}
		switch x := t.(type) {
		case xml.StartElement:
			if x.Name.Local == "key" {
				if d.DecodeElement(&key, &x) != nil {
					return out
				}
			} else if key != "" && (x.Name.Local == "string" || x.Name.Local == "true" || x.Name.Local == "false") {
				val := x.Name.Local
				if val == "string" {
					d.DecodeElement(&val, &x)
				}
				out[key] = val
				key = ""
			}
		}
	}
	return out
}
