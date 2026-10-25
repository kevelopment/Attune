package app

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"attune/internal/library"
	"attune/internal/model"
)

func writeTestEPUB(t *testing.T, path, title string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entries := map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/book.opf"/></rootfiles></container>`,
		"OPS/book.opf":           `<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>` + title + `</dc:title><dc:creator id="creator">Writer</dc:creator><dc:publisher>Library Publisher</dc:publisher><dc:date>2022-04</dc:date><dc:identifier scheme="ISBN">9780000000001</dc:identifier><dc:description>Library description</dc:description><dc:language>en</dc:language><meta refines="#creator" property="file-as">Writer, One</meta><meta name="calibre:series" content="Series"/><meta name="calibre:series_index" content="2"/><meta name="calibre:title_sort" content="Title, Library"/><meta name="calibre:pages" content="309"/><meta name="cover" content="cover"/></metadata><manifest><item id="cover" href="cover.png" media-type="image/png" properties="cover-image"/><item id="chapter" href="chapter.xhtml" media-type="application/xhtml+xml"/><item id="style" href="book.css" media-type="text/css"/></manifest><spine><itemref idref="chapter"/></spine></package>`,
		"OPS/chapter.xhtml":      `<html><head><title>Preview Chapter</title><link rel="stylesheet" href="book.css"><script>alert(1)</script></head><body><h1 onclick="bad()">Safe text</h1><a href="https://remote.test/redirect">external</a></body></html>`,
		"OPS/book.css":           `body{color:#222}`,
	}
	entries["OPS/cover.png"] = string(testPNGCover(t))
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

func testPNGCover(t *testing.T) []byte {
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

func writeTestEPUBWithoutAuthor(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entries := map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OPS/book.opf"/></rootfiles></container>`,
		"OPS/book.opf":           `<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Untitled without author</dc:title></metadata><manifest/><spine/></package>`,
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

func TestImportEPUBWithoutAuthorKeepsAuthorsArraySafe(t *testing.T) {
	a := testApp(t)
	defer a.store.Close()
	source := filepath.Join(t.TempDir(), "no-author.epub")
	writeTestEPUBWithoutAuthor(t, source)

	result, err := a.ImportPaths([]string{source})
	if err != nil || result.Imported != 1 || len(result.BookIDs) != 1 {
		t.Fatalf("import = %#v, %v", result, err)
	}
	state, err := a.GetState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Books) != 1 || state.Books[0].Authors == nil || len(state.Books[0].Authors) != 0 {
		t.Fatalf("book authors = %#v, want a non-nil empty array", state.Books)
	}
}

func TestImportPathsContinuesAfterMalformedEPUB(t *testing.T) {
	a := testApp(t)
	defer a.store.Close()
	root := t.TempDir()
	malformed := filepath.Join(root, "broken.epub")
	if err := os.WriteFile(malformed, []byte("not a zip archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	valid := filepath.Join(root, "no-author.epub")
	writeTestEPUBWithoutAuthor(t, valid)

	result, err := a.ImportPaths([]string{malformed, valid})
	if err != nil || result.Imported != 1 || len(result.Errors) != 1 || len(result.BookIDs) != 1 {
		t.Fatalf("import result = %#v, error = %v", result, err)
	}
	state, err := a.GetState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Books) != 1 || state.Books[0].Authors == nil {
		t.Fatalf("library after partial import = %#v", state.Books)
	}
}

func TestImportFileSafelyTurnsPanicsIntoPerFileErrors(t *testing.T) {
	_, duplicate, err := importFileSafely("malformed.epub", func() (model.Book, bool, error) {
		panic("unexpected parser failure")
	})
	if err == nil || duplicate {
		t.Fatalf("panic recovery returned duplicate=%t, error=%v", duplicate, err)
	}
}

func testApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	store, err := library.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	d := model.Device{ID: "fake-kindle", Name: "Kindle", Root: filepath.Join(t.TempDir(), "device"), Transport: "usb-storage", ProfileID: "kindle", BookFolders: []string{"documents", "Documents"}, TransferFormats: []string{"azw3", "mobi", "prc"}, AllowEPUBToMOBI: true}
	return &App{ctx: context.Background(), root: root, store: store, manager: &fakeManager{device: d}, converter: &fakeConverter{}, converterVersion: "test", selected: map[string]model.Device{}, emit: func(string, ...interface{}) {}}
}

func makeImportCalibreLibrary(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	bookDir := filepath.Join(root, "Author", "Book (1)")
	if err := os.MkdirAll(bookDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Author", "Other (2)"), 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(bookDir, "Book - Author.epub")
	writeTestEPUB(t, source, "Calibre metadata title")
	if err := os.WriteFile(filepath.Join(bookDir, "Book - Author.pdf"), []byte("PDF fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE books(id INTEGER PRIMARY KEY, title TEXT, timestamp TEXT, pubdate TEXT, author_sort TEXT, sort TEXT, series_index REAL, path TEXT, has_cover INTEGER, last_modified TEXT);
		CREATE TABLE data(id INTEGER PRIMARY KEY, book INTEGER, format TEXT, uncompressed_size INTEGER, name TEXT);
		INSERT INTO books VALUES(1,'Calibre metadata title','2024-01-02 03:04:05+00:00',NULL,NULL,NULL,1,'Author/Book (1)',0,'2024-01-03 00:00:00+00:00');
		INSERT INTO data VALUES(1,1,'EPUB',123,'Book - Author');
		INSERT INTO data VALUES(2,1,'PDF',123,'Book - Author');
		INSERT INTO books VALUES(2,'Metadata only','2024-01-02 03:04:05+00:00',NULL,NULL,NULL,1,'Author/Other (2)',0,'2024-01-03 00:00:00+00:00');
		ALTER TABLE books ADD COLUMN uuid TEXT;
		UPDATE books SET uuid='calibre-book-1' WHERE id=1;
		UPDATE books SET uuid='calibre-book-2' WHERE id=2;
		CREATE TABLE library_id(id INTEGER PRIMARY KEY, uuid TEXT);
		INSERT INTO library_id VALUES(1,'calibre-library-1');
		CREATE TABLE custom_columns(id INTEGER PRIMARY KEY, label TEXT, name TEXT, datatype TEXT);
		CREATE TABLE custom_column_1(book INTEGER, value TEXT);
		INSERT INTO custom_columns VALUES(1,'Shelf note','shelf_note','text');
		INSERT INTO custom_column_1 VALUES(1,'keep this custom value');
		CREATE TABLE last_read_positions(book INTEGER, format TEXT, user TEXT, device TEXT, cfi TEXT, epoch REAL, pos_frac REAL);
		INSERT INTO last_read_positions VALUES(1,'EPUB','viewer','device-1','epubcfi(/6/2)',1700000000,0.25);
		CREATE TABLE annotations(book INTEGER, format TEXT, user_type TEXT, user TEXT, timestamp REAL, annot_id TEXT, annot_type TEXT, annot_data TEXT, searchable_text TEXT);
		INSERT INTO annotations VALUES(1,'EPUB','local','user-1',1700000001,'annotation-1','highlight','{\"cfi\":\"epubcfi(/6/4)\"}','highlight text');`)
	closeErr := db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	return root, source
}

func TestImportCalibreLibraryCopiesAllFormatsAndMetadataOnlyRecords(t *testing.T) {
	a := testApp(t)
	defer a.store.Close()
	root, source := makeImportCalibreLibrary(t)
	sourceBefore, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	metadataBefore, err := os.ReadFile(filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	var lastProgress model.ImportProgress
	a.emit = func(event string, args ...interface{}) {
		if event == "import:progress" && len(args) > 0 {
			lastProgress = args[0].(model.ImportProgress)
		}
	}
	result, err := a.importCalibreLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 2 || result.Duplicates != 0 || result.Skipped != 0 || len(result.BookIDs) != 2 {
		t.Fatalf("unexpected import result: %#v", result)
	}
	if lastProgress.Stage != "done" || lastProgress.Skipped != 0 || lastProgress.Total != 2 {
		t.Fatalf("unexpected final progress: %#v", lastProgress)
	}
	after, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(sourceBefore, after) {
		t.Fatalf("Calibre EPUB source changed: %v", err)
	}
	metadataAfter, err := os.ReadFile(filepath.Join(root, "metadata.db"))
	if err != nil || !bytes.Equal(metadataBefore, metadataAfter) {
		t.Fatalf("Calibre metadata database changed: %v", err)
	}
	book, _, err := a.store.BookSource(result.BookIDs[0])
	if err != nil || book.Title != "Calibre metadata title" {
		t.Fatalf("imported book metadata missing: %#v, %v", book, err)
	}
	if _, pdfPath, err := a.store.BookFileSourceByFormat(result.BookIDs[0], "PDF"); err != nil {
		t.Fatalf("Calibre PDF file missing: %v", err)
	} else if data, readErr := os.ReadFile(pdfPath); readErr != nil || string(data) != "PDF fixture" {
		t.Fatalf("Calibre PDF was not copied: data=%q err=%v", data, readErr)
	}
	var sourceMetadata, customMetadata, readingState string
	attuneDB, err := sql.Open("sqlite3", filepath.Join(a.root, "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer attuneDB.Close()
	if err = attuneDB.QueryRow(`SELECT source_metadata_json,custom_metadata_json,reading_state_json FROM books WHERE id=?`, result.BookIDs[0]).Scan(&sourceMetadata, &customMetadata, &readingState); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sourceMetadata, "calibre-book-1") || !strings.Contains(customMetadata, "keep this custom value") || !strings.Contains(readingState, "epubcfi(/6/4)") {
		t.Fatalf("Calibre source payloads were not preserved: raw=%s custom=%s state=%s", sourceMetadata, customMetadata, readingState)
	}
	additional := filepath.Join(t.TempDir(), "alternate.txt")
	if err = os.WriteFile(additional, []byte("alternate format"), 0o600); err != nil {
		t.Fatal(err)
	}
	attached, err := a.attachBookFiles(result.BookIDs[0], []string{additional})
	if err != nil || attached.Imported != 1 || len(attached.Errors) != 0 {
		t.Fatalf("attach additional format = %#v, %v", attached, err)
	}
	if _, attachedPath, err := a.store.BookFileSourceByFormat(result.BookIDs[0], "TXT"); err != nil {
		t.Fatalf("attached TXT asset missing: %v", err)
	} else if data, readErr := os.ReadFile(attachedPath); readErr != nil || string(data) != "alternate format" {
		t.Fatalf("attached TXT asset content=%q err=%v", data, readErr)
	}
	pdfSource := filepath.Join(root, "Author", "Book (1)", "Book - Author.pdf")
	if err = os.WriteFile(pdfSource, []byte("changed PDF fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = a.importCalibreLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 0 || result.Duplicates != 2 || result.Skipped != 0 {
		t.Fatalf("reimport should report two linked records: %#v", result)
	}
	files, err := a.store.BookFiles(result.BookIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	pdfFiles, preferredPDF := 0, 0
	for _, file := range files {
		if file.Format == "pdf" {
			pdfFiles++
			if file.Preferred {
				preferredPDF++
			}
		}
	}
	if pdfFiles != 2 || preferredPDF != 1 {
		t.Fatalf("changed Calibre file should be kept as a non-preferred variant: %#v", files)
	}
	if err = os.Remove(pdfSource); err != nil {
		t.Fatal(err)
	}
	result, err = a.importCalibreLibrary(root)
	if err != nil || result.Imported != 0 || result.Duplicates != 2 || len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "pdf") {
		t.Fatalf("missing file should be reported without dropping other assets: %#v err=%v", result, err)
	}
	if _, _, err = a.store.BookFileSourceByFormat(result.BookIDs[0], "EPUB"); err != nil {
		t.Fatalf("available EPUB was lost after missing-PDF reimport: %v", err)
	}
}

type finalizingFirstWriteWriter struct {
	header http.Header
	code   int
	body   bytes.Buffer
	writes int
}

func (w *finalizingFirstWriteWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *finalizingFirstWriteWriter) WriteHeader(code int) { w.code = code }
func (w *finalizingFirstWriteWriter) Write(data []byte) (int, error) {
	if w.writes > 0 {
		return 0, errors.New("response finalized after first write")
	}
	if w.code == 0 {
		w.code = http.StatusOK
	}
	w.writes++
	return w.body.Write(data)
}

func TestCoverHandlerWritesCompleteLargeImageOnce(t *testing.T) {
	a := testApp(t)
	defer a.store.Close()
	source := filepath.Join(t.TempDir(), "large-cover.epub")
	writeTestEPUB(t, source, "Large Cover")
	result, err := a.ImportPaths([]string{source})
	if err != nil || result.Imported != 1 {
		t.Fatalf("import = %#v, %v", result, err)
	}
	books, err := a.store.ListBooks()
	if err != nil || len(books) != 1 {
		t.Fatalf("books = %#v, %v", books, err)
	}
	coverPath, err := a.store.CoverPath(books[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	img := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			img.Set(x, y, color.NRGBA{R: uint8((x*17 + y*31) % 256), G: uint8((x*43 + y*7) % 256), B: uint8((x*11 + y*29) % 256), A: 255})
		}
	}
	var encoded bytes.Buffer
	if err = jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if encoded.Len() <= 32<<10 {
		t.Fatalf("fixture cover is only %d bytes; want >32 KiB", encoded.Len())
	}
	if err = os.WriteFile(coverPath, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	w := &finalizingFirstWriteWriter{}
	a.ServeCovers(w, httptest.NewRequest(http.MethodGet, books[0].CoverURL, nil))
	if w.code != http.StatusOK || w.writes != 1 || !bytes.Equal(w.body.Bytes(), encoded.Bytes()) {
		t.Fatalf("cover response code=%d writes=%d body=%d bytes, want one complete %d-byte write", w.code, w.writes, w.body.Len(), encoded.Len())
	}
	if got := w.Header().Get("Content-Length"); got != strconv.Itoa(encoded.Len()) {
		t.Fatalf("Content-Length=%q, want %d", got, encoded.Len())
	}
}

func TestComicArchiveManifestAndLazyPageRoute(t *testing.T) {
	a := testApp(t)
	defer a.store.Close()
	comic := filepath.Join(t.TempDir(), "sample.cbz")
	file, err := os.Create(comic)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for _, name := range []string{"page10.png", "page2.png", "page1.png", "notes.txt"} {
		entry, createErr := archive.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, err = entry.Write([]byte("image:" + name)); err != nil {
			t.Fatal(err)
		}
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := a.ImportPaths([]string{comic})
	if err != nil || result.Imported != 1 {
		t.Fatalf("import = %#v, %v", result, err)
	}
	book, err := a.store.GetBook(result.BookIDs[0])
	if err != nil || len(book.Files) != 1 {
		t.Fatalf("book = %#v, %v", book, err)
	}
	fileID := book.Files[0].ID
	manifestRequest := httptest.NewRequest(http.MethodGet, "/book-archives/"+book.ID+"/"+fileID+"/manifest", nil)
	manifestResponse := httptest.NewRecorder()
	a.ServeBookArchive(manifestResponse, manifestRequest)
	if manifestResponse.Code != http.StatusOK || !strings.Contains(manifestResponse.Header().Get("Content-Security-Policy"), "script-src 'none'") {
		t.Fatalf("manifest response = %d, %v", manifestResponse.Code, manifestResponse.Header())
	}
	var manifest comicManifest
	if err = json.Unmarshal(manifestResponse.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Pages) != 3 || !strings.HasSuffix(manifest.Pages[0].Filename, "page1.png") || !strings.HasSuffix(manifest.Pages[1].Filename, "page2.png") || !strings.HasSuffix(manifest.Pages[2].Filename, "page10.png") {
		t.Fatalf("comic page order = %#v", manifest.Pages)
	}
	pageResponse := httptest.NewRecorder()
	a.ServeBookArchive(pageResponse, httptest.NewRequest(http.MethodGet, manifest.Pages[1].URL, nil))
	if pageResponse.Code != http.StatusOK || pageResponse.Header().Get("Content-Type") != "image/png" || pageResponse.Body.String() != "image:page2.png" {
		t.Fatalf("page response = %d %q (%v)", pageResponse.Code, pageResponse.Body.String(), pageResponse.Header())
	}
	missing := httptest.NewRecorder()
	a.ServeBookArchive(missing, httptest.NewRequest(http.MethodGet, "/book-archives/"+book.ID+"/"+fileID+"/pages/3", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing page status = %d", missing.Code)
	}
}

func TestServeBookFileStreamsManagedAssetAndRejectsUnsafePath(t *testing.T) {
	a := testApp(t)
	source := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(source, []byte("reader content"), 0o600); err != nil {
		t.Fatal(err)
	}
	book, duplicate, err := a.store.ImportFileInBatch(source, "test-batch")
	if err != nil || duplicate || len(book.Files) != 1 {
		t.Fatalf("import file = %#v, %v, %v", book, duplicate, err)
	}
	file := book.Files[0]
	request := httptest.NewRequest(http.MethodGet, "/book-files/"+url.PathEscape(book.ID)+"/"+url.PathEscape(file.ID), nil)
	response := httptest.NewRecorder()
	a.ServeBookFile(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "reader content" {
		t.Fatalf("book file response %d: %q", response.Code, response.Body.String())
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(response.Header().Get("Content-Security-Policy"), "script-src 'none'") {
		t.Fatalf("missing book-file protections: %#v", response.Header())
	}
	rangeRequest := httptest.NewRequest(http.MethodGet, "/book-files/"+book.ID+"/"+file.ID, nil)
	rangeRequest.Header.Set("Range", "bytes=2-5")
	rangeResponse := httptest.NewRecorder()
	a.ServeBookFile(rangeResponse, rangeRequest)
	if rangeResponse.Code != http.StatusPartialContent || rangeResponse.Body.String() != "ader" {
		t.Fatalf("range response %d: %q", rangeResponse.Code, rangeResponse.Body.String())
	}
	unsafe := httptest.NewRecorder()
	a.ServeBookFile(unsafe, httptest.NewRequest(http.MethodGet, "/book-files/"+book.ID+"/%2e%2e", nil))
	if unsafe.Code != http.StatusNotFound {
		t.Fatalf("unsafe asset path returned %d", unsafe.Code)
	}
}

func TestAppImportEditCacheInvalidationPersistenceAndCover(t *testing.T) {
	a := testApp(t)
	source := filepath.Join(t.TempDir(), "book.epub")
	writeTestEPUB(t, source, "Original")
	result, err := a.ImportPaths([]string{source})
	if err != nil || result.Imported != 1 {
		t.Fatalf("import = %#v, %v", result, err)
	}
	state, err := a.GetState()
	if err != nil || len(state.Books) != 1 {
		t.Fatalf("state = %#v, %v", state, err)
	}
	b := state.Books[0]
	preview, err := a.GetBookPreview(b.ID)
	if err != nil || len(preview.Chapters) != 1 || preview.Chapters[0].Title != "Safe text" {
		t.Fatalf("preview=%#v, err=%v", preview, err)
	}
	chapterResponse := httptest.NewRecorder()
	a.ServePreviewResource(chapterResponse, httptest.NewRequest("GET", preview.Chapters[0].Href, nil))
	chapterBody := chapterResponse.Body.String()
	if chapterResponse.Code != 200 || !strings.Contains(chapterBody, "Safe text") || strings.Contains(chapterBody, "<script") || strings.Contains(chapterBody, "onclick") || strings.Contains(chapterBody, "remote.test") {
		t.Fatalf("unsafe/bad chapter response %d: %s", chapterResponse.Code, chapterBody)
	}
	if !strings.Contains(chapterResponse.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("missing preview CSP: %#v", chapterResponse.Header())
	}
	escapeResponse := httptest.NewRecorder()
	a.ServePreviewResource(escapeResponse, httptest.NewRequest("GET", "/preview/"+b.ID+"/%2e%2e/outside.txt", nil))
	if escapeResponse.Code != 404 {
		t.Fatalf("preview traversal returned %d", escapeResponse.Code)
	}
	before := cacheKey(b, a.converterVersion)
	edited, err := a.UpdateBook(b.ID, "Edited", []string{"New Writer"})
	if err != nil {
		t.Fatal(err)
	}
	after := cacheKey(edited, a.converterVersion)
	if before == after {
		t.Fatal("metadata edit did not invalidate conversion cache key")
	}
	if err = a.store.SaveConversion(edited.ID, after, filepath.Join(a.root, "conversions", "edited.mobi")); err != nil {
		t.Fatal(err)
	}
	coverReq := httptest.NewRequest("GET", edited.CoverURL, nil)
	coverRes := httptest.NewRecorder()
	a.ServeCovers(coverRes, coverReq)
	if coverRes.Code != 200 || coverRes.Header().Get("Content-Type") != "image/png" || !strings.Contains(coverRes.Header().Get("X-Content-Type-Options"), "nosniff") {
		t.Fatalf("cover response: %d %#v %q", coverRes.Code, coverRes.Header(), coverRes.Body.String())
	}
	if err = a.store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := library.Open(a.root)
	if err != nil {
		t.Fatal(err)
	}
	a.store = store
	defer store.Close()
	state, err = a.GetState()
	if err != nil || len(state.Books) != 1 || state.Books[0].Title != "Edited" || state.Books[0].Revision != 2 {
		t.Fatalf("restart state = %#v, %v", state, err)
	}
	if got, err := store.GetConversion(after); err != nil || got != filepath.Join(a.root, "conversions", "edited.mobi") {
		t.Fatalf("persisted cache = %q, %v", got, err)
	}
	bad := httptest.NewRecorder()
	a.ServeCovers(bad, httptest.NewRequest("GET", "/covers/../etc/passwd", nil))
	if bad.Code != 404 {
		t.Fatalf("bad cover path status = %d", bad.Code)
	}
}

func TestUpdateBookMetadataPersistsClearsAndInvalidatesExport(t *testing.T) {
	a := testApp(t)
	source := filepath.Join(t.TempDir(), "metadata.epub")
	writeTestEPUB(t, source, "Source Title")
	result, err := a.ImportPaths([]string{source})
	if err != nil || result.Imported != 1 {
		t.Fatalf("import = %#v, %v", result, err)
	}
	books, err := a.store.ListBooks()
	if err != nil || len(books) != 1 {
		t.Fatalf("books = %#v, %v", books, err)
	}
	originalBook := books[0]
	originalPath := filepath.Join(a.root, "books", originalBook.ID, "original.epub")
	originalHash, err := fileHash(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	oldKey := cacheKey(originalBook, a.converterVersion)
	edit := model.BookMetadataEdit{
		Title: "Edited Title", Authors: []string{"New Author"}, Publisher: "Edited Publisher",
		Language: "fr-ca", Published: "2022-04", Subjects: []string{"History", "History", "Biography"},
		Series: "New Series", SeriesIndex: "2.5", Description: "Edited description",
	}
	updated, err := a.UpdateBookMetadata(originalBook.ID, edit)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != originalBook.Revision+1 || updated.Title != edit.Title || updated.Language != "fr-CA" || updated.Publisher != edit.Publisher || updated.Published != edit.Published || updated.Series != edit.Series || updated.SeriesIndex != edit.SeriesIndex || updated.Description != edit.Description {
		t.Fatalf("updated editable fields: %#v", updated)
	}
	if len(updated.Subjects) != 2 || updated.Subjects[0] != "History" || updated.Subjects[1] != "Biography" {
		t.Fatalf("subjects were not normalized: %#v", updated.Subjects)
	}
	if updated.PageCount != 309 || updated.Identifiers["ISBN"] != "9780000000001" || updated.TitleSort != "" || len(updated.AuthorSort) != 0 {
		t.Fatalf("source metadata or stale sorts mishandled: %#v", updated)
	}
	if cacheKey(updated, a.converterVersion) == oldKey {
		t.Fatal("metadata edit did not invalidate the conversion key")
	}
	if _, err = a.UpdateBookMetadata(updated.ID, model.BookMetadataEdit{Title: updated.Title, Authors: updated.Authors}); err != nil {
		t.Fatalf("clear optional metadata: %v", err)
	}
	cleared, err := a.store.ListBooks()
	if err != nil || len(cleared) != 1 {
		t.Fatalf("list after clear = %#v, %v", cleared, err)
	}
	if cleared[0].Publisher != "" || cleared[0].Published != "" || cleared[0].Language != "" || len(cleared[0].Subjects) != 0 || cleared[0].Series != "" || cleared[0].SeriesIndex != "" || cleared[0].Description != "" {
		t.Fatalf("optional metadata did not clear: %#v", cleared[0])
	}
	if _, err = a.UpdateBookMetadata(updated.ID, model.BookMetadataEdit{Title: "Bad Date", Authors: updated.Authors, Published: "April 2022"}); err == nil {
		t.Fatal("accepted an invalid publication date")
	}
	if _, err = a.UpdateBookMetadata(updated.ID, model.BookMetadataEdit{Authors: updated.Authors}); err == nil {
		t.Fatal("accepted a blank title")
	}
	if got, err := fileHash(originalPath); err != nil || got != originalHash {
		t.Fatalf("metadata edit changed immutable EPUB: hash=%q err=%v", got, err)
	}
	if err = a.store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := library.Open(a.root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a.store = store
	reopened, err := store.ListBooks()
	if err != nil || len(reopened) != 1 || reopened[0].Title != updated.Title || reopened[0].Publisher != "" || reopened[0].Revision != originalBook.Revision+2 {
		t.Fatalf("reopened metadata = %#v, %v", reopened, err)
	}
}

func TestUpdateBookMetadataAllowsUnknownAuthors(t *testing.T) {
	a := testApp(t)
	defer a.store.Close()
	source := filepath.Join(t.TempDir(), "unknown-author.epub")
	writeTestEPUB(t, source, "Known Book")
	result, err := a.ImportPaths([]string{source})
	if err != nil || result.Imported != 1 {
		t.Fatalf("import = %#v, %v", result, err)
	}
	books, err := a.store.ListBooks()
	if err != nil || len(books) != 1 {
		t.Fatalf("books = %#v, %v", books, err)
	}
	originalPath := filepath.Join(a.root, "books", books[0].ID, "original.epub")
	originalHash, err := fileHash(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := a.UpdateBookMetadata(books[0].ID, model.BookMetadataEdit{
		Title: books[0].Title, Publisher: "Publisher Without Known Author",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Authors) != 0 || updated.Publisher != "Publisher Without Known Author" {
		t.Fatalf("unknown-author edit = %#v", updated)
	}
	if got, err := fileHash(originalPath); err != nil || got != originalHash {
		t.Fatalf("metadata edit changed immutable EPUB: hash=%q err=%v", got, err)
	}
}

func TestGetDeviceBooksFillsMetadataGapsForManagedTransfers(t *testing.T) {
	a := testApp(t)
	source := filepath.Join(t.TempDir(), "book.epub")
	writeTestEPUB(t, source, "Library Title")
	result, err := a.ImportPaths([]string{source})
	if err != nil || result.Imported != 1 {
		t.Fatalf("import=%#v err=%v", result, err)
	}
	books, err := a.store.ListBooks()
	if err != nil || len(books) != 1 {
		t.Fatalf("books=%#v err=%v", books, err)
	}
	b := books[0]
	manager := a.manager.(*fakeManager)
	manager.listedBooks = []model.DeviceBook{{Path: "documents/transfer-" + b.ID + "-key.mobi", Name: "transfer-" + b.ID + "-key.mobi", Title: "Device Title", Authors: []string{"Device Author"}, Publisher: "Device Publisher", Identifiers: map[string]string{"asin": "B00DEVICE"}}}
	got, err := a.GetDeviceBooks(manager.device.ID)
	if err != nil || len(got) != 1 {
		t.Fatalf("device books=%#v err=%v", got, err)
	}
	d := got[0]
	if d.BookID != b.ID || d.Title != "Device Title" || d.Publisher != "Device Publisher" || d.Published != "2022-04" || d.Description != "Library description" || d.SeriesIndex != "2" || d.PageCount != 309 || d.TitleSort != "Title, Library" || len(d.AuthorSort) != 1 || d.AuthorSort[0] != "Writer, One" || d.CoverURL == "" {
		t.Fatalf("metadata enrichment: %#v", d)
	}
	if d.Identifiers["asin"] != "B00DEVICE" || d.Identifiers["ISBN"] != "9780000000001" {
		t.Fatalf("identifier merge: %#v", d.Identifiers)
	}
}

func TestImportProgressEventsReportEachFileAndFinalCounts(t *testing.T) {
	a := testApp(t)
	var events []model.ImportProgress
	a.emit = func(name string, data ...interface{}) {
		if name == "import:progress" {
			events = append(events, data[0].(model.ImportProgress))
		}
	}
	dir := t.TempDir()
	first := filepath.Join(dir, "first.epub")
	duplicate := filepath.Join(dir, "duplicate.epub")
	bad := filepath.Join(dir, "bad.epub")
	writeTestEPUB(t, first, "A Book")
	bytes, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(duplicate, bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(bad, []byte("not an EPUB"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := a.ImportPaths([]string{first, duplicate, bad})
	if err != nil || result.Imported != 1 || result.Duplicates != 1 || len(result.BookIDs) != 1 || len(result.Errors) != 1 {
		t.Fatalf("import result = %#v, %v", result, err)
	}
	books, listErr := a.store.ListBooks()
	if listErr != nil || len(books) != 1 || result.BookIDs[0] != books[0].ID {
		t.Fatalf("import book IDs = %#v, books = %#v, err = %v", result.BookIDs, books, listErr)
	}
	if len(events) != 7 {
		t.Fatalf("got %d progress events, want 7: %#v", len(events), events)
	}
	want := []struct {
		current, imported, duplicates, failed int
		filename, stage                       string
	}{
		{0, 0, 0, 0, "first.epub", "importing"},
		{1, 1, 0, 0, "first.epub", "importing"},
		{1, 1, 0, 0, "duplicate.epub", "importing"},
		{2, 1, 1, 0, "duplicate.epub", "importing"},
		{2, 1, 1, 0, "bad.epub", "importing"},
		{3, 1, 1, 1, "bad.epub", "importing"},
		{3, 1, 1, 1, "", "done"},
	}
	for i, expected := range want {
		got := events[i]
		if got.Current != expected.current || got.Total != 3 || got.Imported != expected.imported || got.Duplicates != expected.duplicates || got.Failed != expected.failed || got.Filename != expected.filename || got.Stage != expected.stage {
			t.Errorf("progress[%d] = %#v, want %+v", i, got, expected)
		}
	}
}

func TestImportPathsReturnsUniqueBookIDsInInputOrder(t *testing.T) {
	a := testApp(t)
	defer a.store.Close()
	dir := t.TempDir()
	first := filepath.Join(dir, "first.epub")
	second := filepath.Join(dir, "second.epub")
	firstCopy := filepath.Join(dir, "first-copy.epub")
	bad := filepath.Join(dir, "bad.epub")
	secondCopy := filepath.Join(dir, "second-copy.epub")
	writeTestEPUB(t, first, "First")
	writeTestEPUB(t, second, "Second")
	content, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(firstCopy, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(bad, []byte("not an EPUB"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(secondCopy, content, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := a.ImportPaths([]string{first, second, firstCopy, bad, secondCopy})
	if err != nil || result.Imported != 2 || result.Duplicates != 2 || len(result.Errors) != 1 || len(result.BookIDs) != 2 {
		t.Fatalf("import result = %#v, %v", result, err)
	}
	books, err := a.store.ListBooks()
	if err != nil {
		t.Fatal(err)
	}
	idByTitle := make(map[string]string, len(books))
	for _, book := range books {
		idByTitle[book.Title] = book.ID
	}
	if result.BookIDs[0] != idByTitle["First"] || result.BookIDs[1] != idByTitle["Second"] {
		t.Fatalf("book IDs = %#v, want first then second: %#v", result.BookIDs, idByTitle)
	}
}

func TestImportPathsPersistsBatchPerOperationAndPreservesDuplicates(t *testing.T) {
	a := testApp(t)
	root := a.root
	dir := t.TempDir()
	first := filepath.Join(dir, "first.epub")
	second := filepath.Join(dir, "second.epub")
	third := filepath.Join(dir, "third.epub")
	writeTestEPUB(t, first, "First")
	writeTestEPUB(t, second, "Second")
	writeTestEPUB(t, third, "Third")

	firstResult, err := a.ImportPaths([]string{first, second})
	if err != nil || firstResult.Imported != 2 {
		t.Fatalf("first import = %#v, %v", firstResult, err)
	}
	books, err := a.store.ListBooks()
	if err != nil || len(books) != 2 {
		t.Fatalf("first books = %#v, %v", books, err)
	}
	firstBatch := books[0].ImportBatchID
	if firstBatch == "" || books[1].ImportBatchID != firstBatch {
		t.Fatalf("first operation batches = %q, %q", firstBatch, books[1].ImportBatchID)
	}
	var addedAt string
	for _, book := range books {
		if book.Title == "First" {
			addedAt = book.AddedAt
		}
	}

	thirdResult, err := a.ImportPaths([]string{third})
	if err != nil || thirdResult.Imported != 1 {
		t.Fatalf("second operation = %#v, %v", thirdResult, err)
	}
	books, err = a.store.ListBooks()
	if err != nil {
		t.Fatal(err)
	}
	var thirdBatch string
	for _, book := range books {
		if book.Title == "Third" {
			thirdBatch = book.ImportBatchID
		}
	}
	if thirdBatch == "" || thirdBatch == firstBatch {
		t.Fatalf("separate operation batch = %q, want distinct from %q", thirdBatch, firstBatch)
	}

	secondResult, err := a.ImportPaths([]string{first})
	if err != nil || secondResult.Duplicates != 1 {
		t.Fatalf("duplicate import = %#v, %v", secondResult, err)
	}
	books, err = a.store.ListBooks()
	if err != nil || len(books) != 3 {
		t.Fatalf("books after duplicate = %#v, %v", books, err)
	}
	for _, book := range books {
		if book.Title == "First" && (book.ImportBatchID != firstBatch || book.AddedAt != addedAt) {
			t.Fatalf("duplicate changed original batch/time: %#v", book)
		}
	}

	if err = a.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := library.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	books, err = reopened.ListBooks()
	if err != nil || len(books) != 3 {
		t.Fatalf("reopened books = %#v, %v", books, err)
	}
	for _, book := range books {
		want := firstBatch
		if book.Title == "Third" {
			want = thirdBatch
		}
		if book.ImportBatchID != want {
			t.Fatalf("reopened batch for %q = %q, want %q", book.Title, book.ImportBatchID, want)
		}
	}
}

func TestEmptyImportDoesNotEmitCompletion(t *testing.T) {
	a := testApp(t)
	var progress int
	a.emit = func(name string, _ ...interface{}) {
		if name == "import:progress" {
			progress++
		}
	}
	if _, err := a.ImportPaths(nil); err != nil {
		t.Fatal(err)
	}
	if progress != 0 {
		t.Fatalf("empty import emitted %d progress events", progress)
	}
}

func TestShutdownCancelsQueueAndRejectsNewJobs(t *testing.T) {
	a := testApp(t)
	source := filepath.Join(t.TempDir(), "book.epub")
	writeTestEPUB(t, source, "Queued")
	result, err := a.ImportPaths([]string{source})
	if err != nil || result.Imported != 1 {
		t.Fatalf("import = %#v, %v", result, err)
	}
	b, err := a.store.ListBooks()
	if err != nil {
		t.Fatal(err)
	}
	manager := a.manager.(*fakeManager)
	manager.blockSend = true
	if err = a.SendBooks(manager.device.ID, []string{b[0].ID}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for manager.started.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if manager.started.Load() == 0 {
		t.Fatal("queue did not start")
	}
	a.Shutdown(context.Background())
	if err = a.SendBooks(manager.device.ID, []string{b[0].ID}); err == nil {
		t.Fatal("accepted a transfer during shutdown")
	}
	if !manager.cancelled.Load() {
		t.Fatal("shutdown did not cancel the active transfer")
	}
}

func TestQueueCarriesCapturedDeviceIdentityThroughConversion(t *testing.T) {
	a := testApp(t)
	source := filepath.Join(t.TempDir(), "book.epub")
	writeTestEPUB(t, source, "Queued")
	if result, err := a.ImportPaths([]string{source}); err != nil || result.Imported != 1 {
		t.Fatalf("import = %#v, %v", result, err)
	}
	books, err := a.store.ListBooks()
	if err != nil {
		t.Fatal(err)
	}
	manager := a.manager.(*fakeManager)
	originalID := manager.device.ID
	converter := a.converter.(*fakeConverter)
	converter.convertHook = func() { manager.device.ID = "replacement-usb-id" }
	if err = a.SendBooks(originalID, []string{books[0].ID}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		a.mu.Lock()
		busy := a.busy
		a.mu.Unlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("transfer queue did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if manager.started.Load() != 1 {
		t.Fatalf("send called %d times, want one", manager.started.Load())
	}
	key := cacheKey(books[0], a.converterVersion)
	filename := sanitizeDeviceTitle(books[0].Title) + "-" + books[0].ID + "-" + key[:12] + ".mobi"
	if got, err := a.store.TransferKey(originalID, filepath.Join("documents", filename)); err != nil || got != "" {
		t.Fatalf("replacement device received transfer tracking: %q, %v", got, err)
	}
}

func TestQueueRebuildsOrphanedConversionOutput(t *testing.T) {
	a := testApp(t)
	source := filepath.Join(t.TempDir(), "book.epub")
	writeTestEPUB(t, source, "Queued")
	if result, err := a.ImportPaths([]string{source}); err != nil || result.Imported != 1 {
		t.Fatalf("import = %#v, %v", result, err)
	}
	books, err := a.store.ListBooks()
	if err != nil {
		t.Fatal(err)
	}
	key := cacheKey(books[0], a.converterVersion)
	orphan := filepath.Join(a.root, "conversions", key+".mobi")
	if err = os.MkdirAll(filepath.Dir(orphan), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(orphan, []byte("stale partial file"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := a.manager.(*fakeManager)
	if err = a.SendBooks(manager.device.ID, []string{books[0].ID}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		a.mu.Lock()
		busy := a.busy
		a.mu.Unlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("transfer queue did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if got, err := os.ReadFile(orphan); err != nil || string(got) != "fake mobi bytes" {
		t.Fatalf("orphan not rebuilt: %q, %v", got, err)
	}
	if p, err := a.store.GetConversion(key); err != nil || p != orphan {
		t.Fatalf("cache was not recorded: %q, %v", p, err)
	}
	if a.converter.(*fakeConverter).calls.Load() != 1 {
		t.Fatal("expected one converter invocation")
	}
}

func TestQueueRecordsMatchingUntrackedDeviceFile(t *testing.T) {
	a := testApp(t)
	source := filepath.Join(t.TempDir(), "book.epub")
	writeTestEPUB(t, source, "Queued")
	if result, err := a.ImportPaths([]string{source}); err != nil || result.Imported != 1 {
		t.Fatalf("import = %#v, %v", result, err)
	}
	books, err := a.store.ListBooks()
	if err != nil {
		t.Fatal(err)
	}
	b := books[0]
	key := cacheKey(b, a.converterVersion)
	cached := filepath.Join(a.root, "conversions", key+".mobi")
	if err = os.MkdirAll(filepath.Dir(cached), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cached, []byte("same mobi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = a.store.SaveConversion(b.ID, key, cached); err != nil {
		t.Fatal(err)
	}
	manager := a.manager.(*fakeManager)
	filename := sanitizeDeviceTitle(b.Title) + "-" + b.ID + "-" + key[:12] + ".mobi"
	target := filepath.Join(manager.device.Root, "documents", filename)
	if err = os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, []byte("same mobi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = a.SendBooks(manager.device.ID, []string{b.ID}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		a.mu.Lock()
		busy := a.busy
		a.mu.Unlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("transfer queue did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if manager.started.Load() != 0 {
		t.Fatal("matching device file was redundantly sent")
	}
	if got, err := a.store.TransferKey(manager.device.ID, filepath.ToSlash(filepath.Join("documents", filename))); err != nil || got != key {
		t.Fatalf("recovered transfer tracking = %q, %v", got, err)
	}
}

func TestSanitizeDeviceTitle(t *testing.T) {
	if got := sanitizeDeviceTitle("../A title: with / path"); got != "A-title-with-path" {
		t.Fatalf("sanitized title = %q", got)
	}
	if got := sanitizeDeviceTitle("🙂/<>:\\"); got != "Book" {
		t.Fatalf("empty sanitized title = %q", got)
	}
	long := sanitizeDeviceTitle(strings.Repeat("Long title ", 20))
	if len(long) > 80 || strings.ContainsAny(long, "/\\:") {
		t.Fatalf("unsafe or oversized sanitized title: %q (%d bytes)", long, len(long))
	}
}

func TestGetStateSurvivesDeviceDiscoveryFailure(t *testing.T) {
	a := testApp(t)
	manager := a.manager.(*fakeManager)
	manager.discoverErr = errors.New("diskutil failed")
	state, err := a.GetState()
	if err != nil {
		t.Fatalf("library state failed with device discovery: %v", err)
	}
	if state.Books == nil || len(state.Devices) != 0 {
		t.Fatalf("unexpected degraded state: %#v", state)
	}
	_ = a.store.Close()
}

func TestDevicePreferencesPersistAndRequireUnknownProfileSelection(t *testing.T) {
	a := testApp(t)
	manager := a.manager.(*fakeManager)
	manager.device.ProfileID = "generic"
	manager.device.BookFolders = []string{"Books", "Documents"}
	manager.device.TransferFormats = nil
	manager.device.RequiresExplicitSelection = true

	state, err := a.GetState()
	if err != nil || len(state.Devices) != 1 || !state.Devices[0].RequiresExplicitSelection {
		t.Fatalf("unconfigured generic device state = %#v, %v", state.Devices, err)
	}
	if err = a.SendBooks(manager.device.ID, []string{"not-a-book"}); err == nil || !strings.Contains(err.Error(), "choose a book folder") {
		t.Fatalf("unconfigured generic device send error = %v", err)
	}
	if err = a.SaveDevicePreferences(manager.device.ID, []string{"Books/To Read"}, []string{"epub", "pdf"}); err != nil {
		t.Fatal(err)
	}
	prefs, err := a.GetDevicePreferences(manager.device.ID)
	if err != nil || !prefs.Customized || strings.Join(prefs.Folders, ",") != "Books/To Read" || strings.Join(prefs.Formats, ",") != "epub,pdf" {
		t.Fatalf("saved preferences = %#v, %v", prefs, err)
	}
	state, err = a.GetState()
	if err != nil || len(state.Devices) != 1 {
		t.Fatalf("configured generic device state = %#v, %v", state.Devices, err)
	}
	configured := state.Devices[0]
	if configured.RequiresExplicitSelection || strings.Join(configured.BookFolders, ",") != "Books/To Read" || strings.Join(configured.TransferFormats, ",") != "epub,pdf" {
		t.Fatalf("custom preferences were not applied: %#v", configured)
	}
	if err = a.ResetDevicePreferences(manager.device.ID); err != nil {
		t.Fatal(err)
	}
	state, err = a.GetState()
	if err != nil || len(state.Devices) != 1 || !state.Devices[0].RequiresExplicitSelection {
		t.Fatalf("reset generic device state = %#v, %v", state.Devices, err)
	}
}

func TestGetStateDoesNotTrustDifferentDeviceAtRememberedMount(t *testing.T) {
	a := testApp(t)
	manager := a.manager.(*fakeManager)
	manager.discoverErr = errors.New("discovery unavailable")
	stale := manager.device
	stale.ID = "previous-usb-id"
	a.selected[stale.ID] = stale
	manager.selectedDevice = manager.device
	state, err := a.GetState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Devices) != 0 {
		t.Fatalf("different device inherited manual selection: %#v", state.Devices)
	}
	if _, ok := a.selected[stale.ID]; ok {
		t.Fatal("stale manual selection was retained")
	}
}

type fakeManager struct {
	device         model.Device
	selectedDevice model.Device
	listedBooks    []model.DeviceBook
	blockSend      bool
	started        atomic.Int32
	cancelled      atomic.Bool
	discoverErr    error
}

func (m *fakeManager) Discover() ([]model.Device, error) {
	if m.discoverErr != nil {
		return nil, m.discoverErr
	}
	return []model.Device{m.device}, nil
}
func (m *fakeManager) SelectVolume(root string) (model.Device, error) {
	if m.selectedDevice.ID != "" {
		return m.selectedDevice, nil
	}
	return m.device, nil
}
func (m *fakeManager) List(model.Device) ([]model.DeviceBook, error) {
	return append([]model.DeviceBook(nil), m.listedBooks...), nil
}
func (m *fakeManager) Send(ctx context.Context, d model.Device, source, filename string) (string, error) {
	m.started.Add(1)
	if m.device.ID != d.ID {
		return "", errors.New("device identity changed")
	}
	if m.blockSend {
		<-ctx.Done()
		m.cancelled.Store(errors.Is(ctx.Err(), context.Canceled))
		return "", ctx.Err()
	}
	return filepath.Join("documents", filename), nil
}
func (m *fakeManager) Remove(model.Device, string) error         { return nil }
func (m *fakeManager) Eject(context.Context, model.Device) error { return nil }

type fakeConverter struct {
	calls       atomic.Int32
	convertHook func()
}

func (*fakeConverter) Version(context.Context) (string, error) { return "test", nil }
func (c *fakeConverter) Convert(ctx context.Context, source, output, title string, authors []string) error {
	c.calls.Add(1)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(output, []byte("fake mobi bytes"), 0o600); err != nil {
		return err
	}
	if c.convertHook != nil {
		c.convertHook()
	}
	return nil
}
