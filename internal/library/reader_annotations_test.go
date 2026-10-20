package library

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"attune/internal/model"
)

func TestImportCalibreAnnotationsIsIdempotentAndPreservesNewerAttuneState(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	source := filepath.Join(t.TempDir(), "book.epub")
	fixtureEPUB(t, source, "Annotations")
	book, _, err := s.ImportEPUB(source)
	if err != nil {
		t.Fatal(err)
	}
	asset, _, err := s.BookFileSourceByFormat(book.ID, "epub")
	if err != nil {
		t.Fatal(err)
	}
	locator := json.RawMessage(`{"version":1,"kind":"cfi","value":"epubcfi(/6/4)","progress":0.2}`)
	if err := s.SaveReaderLocation(book.ID, asset.ID, 1, locator); err != nil {
		t.Fatal(err)
	}
	initial := CalibreAnnotation{SourceID: "highlight-1", Kind: "highlight", Quote: "quoted text", CFI: "epubcfi(/6/4)", Timestamp: "2026-10-18T12:00:00Z"}
	if err := s.ImportCalibreAnnotations(book.ID, "library-uuid:book-uuid", []CalibreAnnotation{initial}); err != nil {
		t.Fatal(err)
	}
	if err := s.ImportCalibreAnnotations(book.ID, "library-uuid:book-uuid", []CalibreAnnotation{initial}); err != nil {
		t.Fatal(err)
	}
	older := initial
	older.Quote = "stale source value"
	older.Timestamp = "2026-10-17T12:00:00Z"
	if err := s.ImportCalibreAnnotations(book.ID, "library-uuid:book-uuid", []CalibreAnnotation{older}); err != nil {
		t.Fatal(err)
	}
	items, err := s.GetReaderAnnotations(book.ID, asset.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("annotations = %#v, %v", items, err)
	}
	if items[0].AssetID != asset.ID || items[0].Quote != "quoted text" || items[0].Status != "unplaced" {
		t.Fatalf("source annotation was not stably linked: %#v", items[0])
	}
	got, found, err := s.GetReaderLocation(book.ID, asset.ID)
	if err != nil || !found || string(got.Locator) != string(locator) {
		t.Fatalf("Calibre reimport changed Attune reading location: %#v found=%v err=%v", got, found, err)
	}
	newer := initial
	newer.Quote = "newer source value"
	newer.Timestamp = "2026-10-19T12:00:00Z"
	if err := s.ImportCalibreAnnotations(book.ID, "library-uuid:book-uuid", []CalibreAnnotation{newer}); err != nil {
		t.Fatal(err)
	}
	items, err = s.GetReaderAnnotations(book.ID, asset.ID)
	if err != nil || len(items) != 1 || items[0].Quote != "newer source value" {
		t.Fatalf("newer source update not applied once: %#v, %v", items, err)
	}
}

func TestImportCalibreAnnotationWithoutUniqueEPUBRemainsUnplaced(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	book, err := s.CreateCalibreBook(model.Book{Title: "Metadata only", Authors: []string{"Reader"}}, nil, nil, nil, nil, "batch")
	if err != nil {
		t.Fatal(err)
	}
	item := CalibreAnnotation{SourceID: "note-1", Kind: "note", Note: "Keep this", CFI: "epubcfi(/6/4)", Timestamp: "2026-10-18T12:00:00Z"}
	if err := s.ImportCalibreAnnotations(book.ID, "library:book", []CalibreAnnotation{item}); err != nil {
		t.Fatal(err)
	}
	items, err := s.GetReaderAnnotations(book.ID, "some-asset")
	if err != nil || len(items) != 1 {
		t.Fatalf("unresolved annotations = %#v, %v", items, err)
	}
	if items[0].AssetID != "" || items[0].Status != "unplaced" || items[0].Note != "Keep this" {
		t.Fatalf("ambiguous item was not retained as unplaced: %#v", items[0])
	}
}

func TestCalibreProgressFallbackIsPerFormatAndNeverPlacesHighlights(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	epubPath := filepath.Join(t.TempDir(), "book.epub")
	fixtureEPUB(t, epubPath, "Book")
	book, _, err := s.ImportEPUB(epubPath)
	if err != nil {
		t.Fatal(err)
	}
	epub, _, err := s.BookFileSourceByFormat(book.ID, "epub")
	if err != nil {
		t.Fatal(err)
	}
	progress := 0.42
	items := []CalibreAnnotation{
		{SourceID: "position", Format: "epub", Kind: "position", Progress: &progress},
		{SourceID: "highlight", Format: "pdf", Kind: "highlight", Quote: "quoted passage", Progress: &progress},
	}
	if err := s.ImportCalibreAnnotations(book.ID, "library:book", items); err != nil {
		t.Fatal(err)
	}
	location, found, err := s.GetReaderLocation(book.ID, epub.ID)
	if err != nil || !found || !strings.Contains(string(location.Locator), `"kind":"text-offset"`) || !strings.Contains(string(location.Locator), `"progress":0.42`) {
		t.Fatalf("position percentage was not retained as reader progress: %#v, %v, %v", location, found, err)
	}
	annotations, err := s.GetReaderAnnotations(book.ID, epub.ID)
	if err != nil || len(annotations) != 2 {
		t.Fatalf("annotations = %#v, %v", annotations, err)
	}
	for _, annotation := range annotations {
		if annotation.Kind == "highlight" && (annotation.AssetID != "" || annotation.Format != "pdf" || annotation.Status != "unplaced") {
			t.Fatalf("unresolved PDF highlight was attached to the EPUB: %#v", annotation)
		}
	}
}
