package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCalibreImportRestoresAnnotationsIdempotently(t *testing.T) {
	a := testApp(t)
	root, _ := makeImportCalibreLibrary(t)
	first, err := a.importCalibreLibrary(root)
	if err != nil || len(first.BookIDs) != 2 {
		t.Fatalf("first Calibre import = %#v, %v", first, err)
	}
	book, err := a.store.GetBook(first.BookIDs[0])
	if err != nil || len(book.Files) == 0 {
		t.Fatalf("imported book files = %#v, %v", book.Files, err)
	}
	var epubID string
	for _, file := range book.Files {
		if file.Format == "epub" {
			epubID = file.ID
		}
	}
	if epubID == "" {
		t.Fatal("imported Calibre book has no EPUB asset")
	}
	annotations, err := a.store.GetReaderAnnotations(book.ID, epubID)
	if err != nil || len(annotations) != 2 {
		t.Fatalf("first imported annotations = %#v, %v", annotations, err)
	}
	var highlight *ReaderAnnotation
	for i := range annotations {
		if annotations[i].Kind == "highlight" {
			highlight = &annotations[i]
		}
	}
	if highlight == nil || highlight.AssetID != epubID || highlight.CFI != "epubcfi(/6/4)" || highlight.Status != "unplaced" {
		t.Fatalf("imported highlight = %#v", highlight)
	}
	annotationID := highlight.ID
	second, err := a.importCalibreLibrary(root)
	if err != nil || second.Duplicates != 2 {
		t.Fatalf("reimport = %#v, %v", second, err)
	}
	annotations, err = a.store.GetReaderAnnotations(book.ID, epubID)
	if err != nil || len(annotations) != 2 {
		t.Fatalf("reimport duplicated or lost annotations = %#v, %v", annotations, err)
	}
	found := false
	for _, annotation := range annotations {
		if annotation.ID == annotationID {
			found = true
		}
	}
	if !found {
		t.Fatalf("reimport changed annotation identity: %#v", annotations)
	}
	location, foundLocation, err := a.store.GetReaderLocation(book.ID, epubID)
	if err != nil || !foundLocation || !strings.Contains(string(location.Locator), `"value":"epubcfi(/6/2)"`) || !strings.Contains(string(location.Locator), `"progress":0.25`) {
		t.Fatalf("Calibre position was not mapped to the EPUB reader location: %#v found=%v err=%v", location, foundLocation, err)
	}
	newerLocator := json.RawMessage(`{"version":1,"kind":"cfi","value":"epubcfi(/6/8)","progress":0.5}`)
	if err := a.store.SaveReaderLocation(book.ID, epubID, 1, newerLocator); err != nil {
		t.Fatal(err)
	}
	if _, err := a.importCalibreLibrary(root); err != nil {
		t.Fatal(err)
	}
	location, foundLocation, err = a.store.GetReaderLocation(book.ID, epubID)
	if err != nil || !foundLocation || string(location.Locator) != string(newerLocator) {
		t.Fatalf("Calibre reimport replaced a newer Attune reader location: %#v found=%v err=%v", location, foundLocation, err)
	}
}
