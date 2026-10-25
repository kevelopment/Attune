package library

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

func pngCover(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 36))
	for y := 0; y < 36; y++ {
		for x := 0; x < 24; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 5), B: 70, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
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
	}
	entries["OPS/cover.png"] = string(pngCover(t))
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

func fixtureEPUBWithoutAuthor(t *testing.T, filename string) {
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
		"META-INF/container.xml": `<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/package.opf"/></rootfiles></container>`,
		"OPS/package.opf":        `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Untitled without author</dc:title></metadata><manifest/><spine/></package>`,
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

func TestImportEPUBWithoutAuthorStoresSafeArraysAndReadsLegacyNull(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "no-author.epub")
	fixtureEPUBWithoutAuthor(t, source)
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	book, duplicate, err := s.ImportEPUB(source)
	if err != nil || duplicate {
		t.Fatalf("ImportEPUB = duplicate %v, error %v", duplicate, err)
	}
	if book.Authors == nil || len(book.Authors) != 0 {
		t.Fatalf("imported authors = %#v, want a non-nil empty array", book.Authors)
	}
	var storedAuthors string
	if err = s.db.QueryRow(`SELECT authors FROM books WHERE id=?`, book.ID).Scan(&storedAuthors); err != nil {
		t.Fatal(err)
	}
	if storedAuthors != "[]" {
		t.Fatalf("stored authors JSON = %q, want []", storedAuthors)
	}

	// Older releases stored a missing creator list as JSON null. Loading such a
	// record must still return an array because the frontend joins this field.
	if _, err = s.db.Exec(`UPDATE books SET authors='null', subjects='null' WHERE id=?`, book.ID); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.GetBook(book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Authors == nil || len(legacy.Authors) != 0 || legacy.Subjects == nil || len(legacy.Subjects) != 0 {
		t.Fatalf("legacy arrays = authors %#v, subjects %#v; want non-nil empty arrays", legacy.Authors, legacy.Subjects)
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
	if data, e := os.ReadFile(cover); e != nil || !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("cover read: %q, %v", data, e)
	}
	edited, err := s.UpdateBook(b.ID, "Edited", []string{"A New Author"})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Revision != 2 || edited.Title != "Edited" {
		t.Fatalf("update failed: %#v", edited)
	}
	if err = s.SaveReadingProgress(b.ID, 2, 0.375); err != nil {
		t.Fatal(err)
	}
	reading, err := s.ReadingProgress(b.ID)
	if err != nil || reading.ChapterIndex != 2 || reading.Position != 0.375 {
		t.Fatalf("reading progress = %#v, %v", reading, err)
	}
	bookmark, err := s.AddReadingBookmark(b.ID, 2, 0.375)
	if err != nil || bookmark.ID == 0 {
		t.Fatalf("add reading bookmark = %#v, %v", bookmark, err)
	}
	bookmarks, err := s.ReadingBookmarks(b.ID)
	if err != nil || len(bookmarks) != 1 || bookmarks[0] != bookmark {
		t.Fatalf("reading bookmarks = %#v, %v", bookmarks, err)
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
	reading, err = s.ReadingProgress(b.ID)
	if err != nil || reading.ChapterIndex != 2 || reading.Position != 0.375 || reading.UpdatedAt == "" {
		t.Fatalf("reading progress after restart = %#v, %v", reading, err)
	}
	bookmarks, err = s.ReadingBookmarks(b.ID)
	if err != nil || len(bookmarks) != 1 || bookmarks[0].ID != bookmark.ID || bookmarks[0].Position != 0.375 {
		t.Fatalf("reading bookmarks after restart = %#v, %v", bookmarks, err)
	}
	if err = s.DeleteReadingBookmark(b.ID, bookmark.ID); err != nil {
		t.Fatal(err)
	}
	bookmarks, err = s.ReadingBookmarks(b.ID)
	if err != nil || len(bookmarks) != 0 {
		t.Fatalf("reading bookmarks after deletion = %#v, %v", bookmarks, err)
	}
}

func TestImportCalibreBookPreservesMetadataCoverAndOriginal(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "book.epub")
	fixtureEPUB(t, source, "Source Title")
	originalHash, err := fileHash(source)
	if err != nil {
		t.Fatal(err)
	}
	cover := jpegCover(t)
	metadata := model.Book{
		Title: "Calibre Title", Authors: []string{"Jane Doe", "Editor"}, Language: "de",
		Subjects: []string{"Novel", "History"}, Publisher: "Calibre Press", Published: "1998-02",
		Identifiers: map[string]string{"isbn": "9781234567890", "doi": "10.1234/example"},
		Description: "Curated description", Series: "A Series", SeriesIndex: "2.5",
		AuthorSort: []string{"Doe, Jane", "Editor"}, TitleSort: "Calibre Title, The", PageCount: 321,
		AddedAt: "2001-01-01T00:00:00Z", ImportBatchID: "source-batch", Revision: 99,
	}
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b, duplicate, err := s.ImportCalibreBook(source, metadata, cover, "calibre-batch-1")
	if err != nil || duplicate {
		t.Fatalf("ImportCalibreBook = duplicate %v, error %v", duplicate, err)
	}
	if b.Title != metadata.Title || !slices.Equal(b.Authors, metadata.Authors) || b.Language != metadata.Language ||
		!slices.Equal(b.Subjects, metadata.Subjects) || b.Publisher != metadata.Publisher || b.Published != metadata.Published ||
		!reflect.DeepEqual(b.Identifiers, metadata.Identifiers) || b.Description != metadata.Description ||
		b.Series != metadata.Series || b.SeriesIndex != metadata.SeriesIndex ||
		!slices.Equal(b.AuthorSort, metadata.AuthorSort) || b.TitleSort != metadata.TitleSort || b.PageCount != metadata.PageCount {
		t.Fatalf("imported metadata = %#v", b)
	}
	if b.ImportBatchID != "calibre-batch-1" || b.AddedAt == "" || b.AddedAt == metadata.AddedAt || b.Revision != 1 {
		t.Fatalf("local import timeline metadata = %#v", b)
	}
	_, storedSource, err := s.BookSource(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sourceHash, hashErr := fileHash(storedSource); hashErr != nil || sourceHash != originalHash {
		t.Fatalf("stored EPUB hash = %q, %v; want %q", sourceHash, hashErr, originalHash)
	}
	coverPath, err := s.CoverPath(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if data, readErr := os.ReadFile(coverPath); readErr != nil || !bytes.Equal(data, cover) {
		t.Fatalf("stored cover = %d bytes, %v; want %d bytes", len(data), readErr, len(cover))
	}
	export := filepath.Join(t.TempDir(), "export.epub")
	if err = s.ExportEPUB(b.ID, export); err != nil {
		t.Fatal(err)
	}
	exported, err := epub.Parse(export)
	if err != nil {
		t.Fatal(err)
	}
	if exported.CoverExt != ".jpg" || !bytes.Equal(exported.Cover, cover) {
		t.Fatalf("exported Calibre cover ext=%q bytes=%d", exported.CoverExt, len(exported.Cover))
	}
	if sourceHash, hashErr := fileHash(source); hashErr != nil || sourceHash != originalHash {
		t.Fatalf("source EPUB changed: hash = %q, %v; want %q", sourceHash, hashErr, originalHash)
	}
}

func TestImportCalibreBookDuplicateDoesNotReplaceMetadataOrCover(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	source := filepath.Join(t.TempDir(), "book.epub")
	fixtureEPUB(t, source, "Source Title")
	firstCover := jpegCover(t)
	first, duplicate, err := s.ImportCalibreBook(source, model.Book{Title: "First title", Authors: []string{"First author"}, Series: "First series"}, firstCover, "first-batch")
	if err != nil || duplicate {
		t.Fatalf("first import = duplicate %v, error %v", duplicate, err)
	}
	secondCover := jpegCover(t)
	second, duplicate, err := s.ImportCalibreBook(source, model.Book{Title: "Replacement title", Authors: []string{"Other author"}, Series: "Other series"}, secondCover, "second-batch")
	if err != nil || !duplicate {
		t.Fatalf("duplicate import = duplicate %v, error %v", duplicate, err)
	}
	if second.ID != first.ID || second.Title != first.Title || !slices.Equal(second.Authors, first.Authors) || second.Series != first.Series || second.ImportBatchID != first.ImportBatchID {
		t.Fatalf("duplicate changed existing metadata: first %#v, second %#v", first, second)
	}
	coverPath, err := s.CoverPath(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if data, readErr := os.ReadFile(coverPath); readErr != nil || !bytes.Equal(data, firstCover) {
		t.Fatalf("duplicate changed existing cover: %d bytes, %v", len(data), readErr)
	}
}

func TestImportCalibreBookRejectsInvalidCover(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	source := filepath.Join(t.TempDir(), "book.epub")
	fixtureEPUB(t, source, "Source Title")
	if _, _, err = s.ImportCalibreBook(source, model.Book{Title: "Calibre Title"}, []byte("not an image"), "batch"); err == nil {
		t.Fatal("expected invalid cover to be rejected")
	}
	books, err := s.ListBooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 0 {
		t.Fatalf("invalid cover left a partial import: %#v", books)
	}
}

func TestMultiFormatAssetsDeduplicateAndKeepVariants(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first := filepath.Join(t.TempDir(), "Manual.pdf")
	if err = os.WriteFile(first, []byte("pdf edition one"), 0o600); err != nil {
		t.Fatal(err)
	}
	book, duplicate, err := s.ImportFileInBatch(first, "batch-1")
	if err != nil || duplicate {
		t.Fatalf("ImportFileInBatch = duplicate %v, err %v", duplicate, err)
	}
	if len(book.Files) != 1 || book.Files[0].Format != "pdf" || !book.Files[0].CanRead || !book.Files[0].CanTransfer {
		t.Fatalf("PDF asset not indexed: %#v", book.Files)
	}
	_, stored, err := s.BookFileSourceByFormat(book.ID, "PDF")
	if err != nil {
		t.Fatal(err)
	}
	if data, e := os.ReadFile(stored); e != nil || string(data) != "pdf edition one" {
		t.Fatalf("asset copy = %q, %v", data, e)
	}
	second := filepath.Join(t.TempDir(), "Manual revised.pdf")
	if err = os.WriteFile(second, []byte("pdf edition two"), 0o600); err != nil {
		t.Fatal(err)
	}
	variant, duplicate, err := s.AttachBookFile(book.ID, second, false)
	if err != nil || duplicate {
		t.Fatalf("AttachBookFile = duplicate %v, err %v", duplicate, err)
	}
	files, err := s.BookFiles(book.ID)
	if err != nil || len(files) != 2 {
		t.Fatalf("BookFiles = %#v, %v", files, err)
	}
	preferred := 0
	for _, file := range files {
		if file.Preferred {
			preferred++
			if file.ID != book.Files[0].ID {
				t.Fatalf("unexpected preferred file: %#v", file)
			}
		}
	}
	if preferred != 1 || variant.Preferred {
		t.Fatalf("variant preference state wrong: %#v", files)
	}
	duplicatePath := filepath.Join(t.TempDir(), "copy.pdf")
	if err = os.WriteFile(duplicatePath, []byte("pdf edition one"), 0o600); err != nil {
		t.Fatal(err)
	}
	duplicateBook, duplicate, err := s.ImportFileInBatch(duplicatePath, "batch-2")
	if err != nil || !duplicate || duplicateBook.ID != book.ID {
		t.Fatalf("same content not deduplicated: %#v, duplicate=%v, err=%v", duplicateBook, duplicate, err)
	}
}

func TestCreateCalibreBookPreservesSourcePayloadAndMapping(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	book, err := s.CreateCalibreBook(model.Book{Title: "Metadata only", Authors: []string{"Reader"}}, []byte(`{"rating":5}`), []byte(`{"title":"source title"}`), []byte(`{"positions":[1]}`), nil, "batch")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.LinkCalibreSource("library-uuid", "book-uuid", book.ID, []byte(`{"title":"source title"}`), []byte(`{"rating":5}`), []byte(`{"positions":[1]}`), "2026-01-01"); err != nil {
		t.Fatal(err)
	}
	if id, found, e := s.CalibreBookID("library-uuid", "book-uuid"); e != nil || !found || id != book.ID {
		t.Fatalf("Calibre mapping = %q, %v, %v", id, found, e)
	}
	books, err := s.ListBooks()
	var custom map[string]any
	if len(books) > 0 {
		_ = json.Unmarshal(books[0].CustomMetadata, &custom)
	}
	if err != nil || len(books) != 1 || custom["rating"] != float64(5) {
		t.Fatalf("metadata-only Calibre book lost: %#v, %v", books, err)
	}
	var raw, state string
	if err = s.db.QueryRow(`SELECT source_metadata_json,reading_state_json FROM books WHERE id=?`, book.ID).Scan(&raw, &state); err != nil || raw != `{"title":"source title"}` || state != `{"positions":[1]}` {
		t.Fatalf("source payloads not retained: raw=%s state=%s err=%v", raw, state, err)
	}
	if _, _, err = s.BookSource(book.ID); err == nil {
		t.Fatal("metadata-only book should not claim an EPUB source")
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
	coverPath := filepath.Join(bookDir, "cover.jpg")
	coverData := jpegCover(t)
	if err := os.WriteFile(coverPath, coverData, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := fileHash(original)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(root, "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE books (id TEXT PRIMARY KEY,title TEXT NOT NULL,authors TEXT NOT NULL,language TEXT NOT NULL DEFAULT '',sha256 TEXT NOT NULL UNIQUE,cover_path TEXT NOT NULL DEFAULT '',original_path TEXT NOT NULL,added_at TEXT NOT NULL,revision INTEGER NOT NULL DEFAULT 1);
		CREATE TABLE reading_progress (book_id TEXT PRIMARY KEY, chapter_index INTEGER NOT NULL, position REAL NOT NULL, updated_at TEXT NOT NULL);
		CREATE TABLE reader_bookmarks (id INTEGER PRIMARY KEY AUTOINCREMENT, book_id TEXT NOT NULL, chapter_index INTEGER NOT NULL, position REAL NOT NULL, created_at TEXT NOT NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO books(id,title,authors,language,sha256,cover_path,original_path,added_at,revision) VALUES(?,?,?,?,?,?,?,?,?)`, id, "User Edited Title", `["User Author"]`, "en", before, filepath.ToSlash(filepath.Join("books", id, "cover.jpg")), filepath.ToSlash(filepath.Join("books", id, "original.epub")), "2026-01-01T00:00:00Z", 4)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO reading_progress(book_id,chapter_index,position,updated_at) VALUES(?,?,?,?)`, id, 2, 0.42, "2026-01-02T00:00:00Z")
	if err == nil {
		_, err = db.Exec(`INSERT INTO reader_bookmarks(book_id,chapter_index,position,created_at) VALUES(?,?,?,?)`, id, 3, 0.67, "2026-01-03T00:00:00Z")
	}
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
	file, indexedPath, err := s.BookFileSourceByFormat(id, "epub")
	if err != nil || file.Format != "epub" || file.SHA256 != before || indexedPath != original {
		t.Fatalf("legacy file index = %#v path=%q err=%v", file, indexedPath, err)
	}
	progress, err := s.ReadingProgress(id)
	if err != nil || progress.ChapterIndex != 2 || progress.Position != 0.42 {
		t.Fatalf("legacy reading progress = %#v err=%v", progress, err)
	}
	bookmarks, err := s.ReadingBookmarks(id)
	if err != nil || len(bookmarks) != 1 || bookmarks[0].ChapterIndex != 3 || bookmarks[0].Position != 0.67 {
		t.Fatalf("legacy bookmarks = %#v err=%v", bookmarks, err)
	}
	migratedCover, err := s.CoverPath(id)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(migratedCover); err != nil || !bytes.Equal(data, coverData) {
		t.Fatalf("legacy cover changed: err=%v", err)
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
