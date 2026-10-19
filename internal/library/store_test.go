package library

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"attune/internal/epub"
	"attune/internal/model"
)

func jpegCover(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 36))
	for y := 0; y < 36; y++ {
		for x := 0; x < 24; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 5), B: 70, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fixtureEPUB(t *testing.T, filename, title string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entries := map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/package.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"OPS/package.opf":        `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>` + title + `</dc:title><dc:creator>Author One</dc:creator><dc:creator>Author Two</dc:creator><dc:publisher>Fixture Publisher</dc:publisher><dc:date>2023-02</dc:date><dc:identifier scheme="ISBN">9780000000002</dc:identifier><dc:description>Fixture description</dc:description><dc:language>en</dc:language><dc:subject>Fiction</dc:subject><dc:subject>Mystery</dc:subject><meta name="calibre:series" content="Fixture Series"/><meta name="calibre:series_index" content="4"/><meta name="calibre:pages" content="123"/><meta name="cover" content="cover"/></metadata><manifest><item id="cover" href="cover.png" media-type="image/png" properties="cover-image"/></manifest><spine/></package>`,
		"OPS/cover.png":          "cover-bytes",
	}
	for name, content := range entries {
		entry, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = entry.Write([]byte(content)); e != nil {
			t.Fatal(e)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestImportEditDuplicateAndRestart(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "book.epub")
	fixtureEPUB(t, source, "Test Book")
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	b, dup, err := s.ImportEPUB(source)
	if err != nil || dup {
		t.Fatalf("ImportEPUB = duplicate %v, error %v", dup, err)
	}
	if b.Title != "Test Book" || len(b.Authors) != 2 || b.Language != "en" || len(b.Subjects) != 2 || b.Subjects[0] != "Fiction" {
		t.Fatalf("unexpected metadata: %#v", b)
	}
	got, sourcePath, err := s.BookSource(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != b.ID {
		t.Fatalf("book source mismatch: %#v", got)
	}
	if _, err = os.Stat(sourcePath); err != nil {
		t.Fatal(err)
	}
	if storedHash, e := fileHash(sourcePath); e != nil || storedHash != b.SHA256 {
		t.Fatalf("managed original hash = %q, %v; want %q", storedHash, e, b.SHA256)
	}
	cover, err := s.CoverPath(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if data, e := os.ReadFile(cover); e != nil || string(data) != "cover-bytes" {
		t.Fatalf("cover read: %q, %v", data, e)
	}
	edited, err := s.UpdateBook(b.ID, "Edited", []string{"A New Author"})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Revision != 2 || edited.Title != "Edited" {
		t.Fatalf("update failed: %#v", edited)
	}
	if _, dup, err = s.ImportEPUB(source); err != nil || !dup {
		t.Fatalf("duplicate import = %v, %v", dup, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	books, err := s.ListBooks()
	if err != nil || len(books) != 1 || books[0].Title != "Edited" || books[0].Revision != 2 {
		t.Fatalf("restart list = %#v, %v", books, err)
	}
}

func TestReplaceCoverPersistsAndExportsWithoutChangingOriginal(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "book.epub")
	fixtureEPUB(t, source, "Test Book")
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.ImportEPUB(source)
	if err != nil {
		t.Fatal(err)
	}
	originalPath := filepath.Join(root, "books", b.ID, "original.epub")
	originalHash, err := fileHash(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	unchangedExport := filepath.Join(t.TempDir(), "unchanged.epub")
	if err = s.ExportEPUB(b.ID, unchangedExport); err != nil {
		t.Fatal(err)
	}
	if got, err := fileHash(unchangedExport); err != nil || got != originalHash {
		t.Fatalf("unmodified cover export differs from original: hash=%q err=%v", got, err)
	}
	newCover := jpegCover(t)
	updated, err := s.ReplaceBookCover(b.ID, newCover)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != b.Revision+1 || updated.CoverURL != b.CoverURL {
		t.Fatalf("cover update = %#v", updated)
	}
	coverPath, err := s.CoverPath(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(coverPath), "cover.override.") {
		t.Fatalf("override path = %q", coverPath)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	books, err := s.ListBooks()
	if err != nil || len(books) != 1 || books[0].Revision != updated.Revision {
		t.Fatalf("reopened books = %#v, %v", books, err)
	}
	output := filepath.Join(t.TempDir(), "export.epub")
	if err = s.ExportEPUB(b.ID, output); err != nil {
		t.Fatal(err)
	}
	metadata, err := epub.Parse(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(metadata.Cover, newCover) || metadata.CoverExt != ".jpg" {
		t.Fatalf("exported cover ext=%q bytes=%d", metadata.CoverExt, len(metadata.Cover))
	}
	if got, err := fileHash(originalPath); err != nil || got != originalHash {
		t.Fatalf("immutable original changed: hash=%q err=%v", got, err)
	}
}

func TestValidateCoverRejectsCorruptAndOversizedData(t *testing.T) {
	tooManyPixels := []byte{0xff, 0xd8, 0xff, 0xc0, 0x00, 0x11, 0x08, 0x19, 0x00, 0x19, 0x00, 0x03, 0x01, 0x11, 0x00, 0x02, 0x11, 0x00, 0x03, 0x11, 0x00}
	for name, data := range map[string][]byte{
		"corrupt":         []byte("not an image"),
		"oversized":       bytes.Repeat([]byte{0}, (32<<20)+1),
		"too_many_pixels": tooManyPixels,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ValidateCover(data); err == nil {
				t.Fatal("invalid cover was accepted")
			}
		})
	}
	if ext, mime, err := ValidateCover(jpegCover(t)); err != nil || ext != ".jpg" || mime != "image/jpeg" {
		t.Fatalf("valid JPEG = %q %q %v", ext, mime, err)
	}
}

func TestSubjectsMigrationBackfillsWithoutChangingUserMetadataOrEPUB(t *testing.T) {
	root := t.TempDir()
	id := "00112233445566778899aabbccddeeff"
	bookDir := filepath.Join(root, "books", id)
	if err := os.MkdirAll(bookDir, 0o700); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(bookDir, "original.epub")
	fixtureEPUB(t, original, "Source Title")
	before, err := fileHash(original)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(root, "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE books (id TEXT PRIMARY KEY,title TEXT NOT NULL,authors TEXT NOT NULL,language TEXT NOT NULL DEFAULT '',sha256 TEXT NOT NULL UNIQUE,cover_path TEXT NOT NULL DEFAULT '',original_path TEXT NOT NULL,added_at TEXT NOT NULL,revision INTEGER NOT NULL DEFAULT 1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO books(id,title,authors,language,sha256,original_path,added_at,revision) VALUES(?,?,?,?,?,?,?,?)`, id, "User Edited Title", `["User Author"]`, "en", before, filepath.ToSlash(filepath.Join("books", id, "original.epub")), "2026-01-01T00:00:00Z", 4)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	books, err := s.ListBooks()
	if err != nil || len(books) != 1 {
		t.Fatalf("books %#v err=%v", books, err)
	}
	b := books[0]
	if b.Title != "User Edited Title" || len(b.Authors) != 1 || b.Authors[0] != "User Author" || b.Revision != 4 {
		t.Fatalf("migration overwrote user fields: %#v", b)
	}
	if b.ImportBatchID != "" {
		t.Fatalf("legacy book batch = %q, want empty", b.ImportBatchID)
	}
	if len(b.Subjects) != 2 || b.Subjects[0] != "Fiction" || b.Subjects[1] != "Mystery" {
		t.Fatalf("backfilled subjects: %#v", b.Subjects)
	}
	if b.Publisher != "Fixture Publisher" || b.Published != "2023-02" || b.Description != "Fixture description" || b.Series != "Fixture Series" || b.SeriesIndex != "4" || b.PageCount != 123 || b.Identifiers["ISBN"] != "9780000000002" {
		t.Fatalf("backfilled extras: %#v", b)
	}
	if b.FileSize <= 0 || b.ModifiedAt == "" {
		t.Fatalf("missing original stats: %#v", b)
	}
	if after, e := fileHash(original); e != nil || after != before {
		t.Fatalf("migration modified source EPUB: %q %v", after, e)
	}
}

func TestImportBadArchiveDoesNotLeaveFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "bad.epub")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entry, _ := w.Create("../escape")
	_, _ = entry.Write([]byte("bad"))
	_ = w.Close()
	_ = f.Close()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, _, err = s.ImportEPUB(source); err == nil {
		t.Fatal("expected traversal rejection")
	}
	books, err := s.ListBooks()
	if err != nil || len(books) != 0 {
		t.Fatalf("bad import persisted: %#v, %v", books, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "books"))
	if err == nil && len(entries) != 0 {
		t.Fatalf("failed import left files: %#v", entries)
	}
}

func TestCacheAndDeviceTracking(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if value, err := s.GetConversion("missing"); err != nil || value != "" {
		t.Fatalf("missing cache = %q, %v", value, err)
	}
	if key, err := s.TransferKey("device", "book.mobi"); err != nil || key != "" {
		t.Fatalf("missing transfer = %q, %v", key, err)
	}
	device := model.Device{ID: "device", Name: "Kindle", Root: "/Volumes/Kindle", FreeBytes: 10, TotalBytes: 20}
	if err = s.RecordDevice(device); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordTransfer(device.ID, "book-id", "documents/book.mobi", "cache-key"); err != nil {
		t.Fatal(err)
	}
	if key, err := s.TransferKey(device.ID, "documents/book.mobi"); err != nil || key != "cache-key" {
		t.Fatalf("transfer key = %q, %v", key, err)
	}
	if err = s.ForgetTransfer(device.ID, "documents/book.mobi"); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveConversion("missing-book", "key", "/cache/book.mobi"); err == nil {
		t.Fatal("expected foreign key failure for unknown book")
	}
	// The cache is associated with an imported book so its composite key is durable.
	source := filepath.Join(t.TempDir(), "book.epub")
	fixtureEPUB(t, source, "Book")
	b, _, err := s.ImportEPUB(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveConversion(b.ID, "key", "/cache/book.mobi"); err != nil {
		t.Fatal(err)
	}
	if p, err := s.GetConversion("key"); err != nil || p != "/cache/book.mobi" {
		t.Fatalf("conversion = %q, %v", p, err)
	}
}

func TestMissingBookSentinel(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.DeleteBook("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteBook error = %v", err)
	}
}

func TestManagedPathsRejectDatabaseTamperingAndSymlinks(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	source := filepath.Join(t.TempDir(), "book.epub")
	fixtureEPUB(t, source, "Book")
	b, _, err := s.ImportEPUB(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE books SET original_path=? WHERE id=?`, "conversions/outside.epub", b.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.BookSource(b.ID); err == nil {
		t.Fatal("tampered original path was accepted")
	}
	if err = s.DeleteBook(b.ID); err == nil {
		t.Fatal("book with tampered owned path was deleted")
	}

	if _, err = s.db.Exec(`UPDATE books SET original_path=? WHERE id=?`, filepath.ToSlash(filepath.Join("books", b.ID, "original.epub")), b.ID); err != nil {
		t.Fatal(err)
	}
	cover, err := s.CoverPath(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err = os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(cover); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, cover); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err = s.CoverPath(b.ID); err == nil {
		t.Fatal("cover symlink was accepted")
	}
	if _, _, err = s.BookSource(b.ID); err != nil {
		t.Fatalf("unrelated original was rejected: %v", err)
	}
}

func TestDeleteBookRejectsUnsafeID(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = os.MkdirAll(filepath.Join(root, "conversions"), 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "conversions", "keep.mobi")
	if err = os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteBook("../conversions"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unsafe ID returned %v", err)
	}
	if _, err = os.Stat(sentinel); err != nil {
		t.Fatalf("unsafe ID removed unrelated managed data: %v", err)
	}
}
