package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReaderLocationPersistsPerAssetAndKeepsLegacyProgress(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	firstSource := filepath.Join(t.TempDir(), "first.epub")
	fixtureEPUB(t, firstSource, "First Book")
	book, _, err := s.ImportEPUB(firstSource)
	if err != nil {
		t.Fatal(err)
	}
	epubFile, _, err := s.BookFileSourceByFormat(book.ID, "epub")
	if err != nil {
		t.Fatal(err)
	}
	textSource := filepath.Join(t.TempDir(), "first.txt")
	if err = os.WriteFile(textSource, []byte("a second rendition"), 0o600); err != nil {
		t.Fatal(err)
	}
	textFile, _, err := s.AttachBookFile(book.ID, textSource, false)
	if err != nil {
		t.Fatal(err)
	}
	secondSource := filepath.Join(t.TempDir(), "second.epub")
	fixtureEPUB(t, secondSource, "Second Book")
	otherBook, _, err := s.ImportEPUB(secondSource)
	if err != nil {
		t.Fatal(err)
	}
	otherFile, _, err := s.BookFileSourceByFormat(otherBook.ID, "epub")
	if err != nil {
		t.Fatal(err)
	}
	if got, found, err := s.GetReaderLocation(book.ID, epubFile.ID); err != nil || found || got.BookID != "" {
		t.Fatalf("initial location = %#v, found=%v err=%v", got, found, err)
	}
	if err = s.SaveReadingProgress(book.ID, 4, 0.375); err != nil {
		t.Fatal(err)
	}
	firstLocator := json.RawMessage(`{"kind":"cfi","value":"epubcfi(/6/4!/4/2)"}`)
	secondLocator := json.RawMessage(`{"kind":"offset","value":815}`)
	if err = s.SaveReaderLocation(book.ID, epubFile.ID, 1, firstLocator); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveReaderLocation(book.ID, textFile.ID, 2, secondLocator); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveReaderLocation(book.ID, otherFile.ID, 1, firstLocator); err == nil {
		t.Fatal("accepted asset owned by another book")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, want := range []struct {
		assetID string
		version int
		locator string
	}{{epubFile.ID, 1, string(firstLocator)}, {textFile.ID, 2, string(secondLocator)}} {
		got, found, err := s.GetReaderLocation(book.ID, want.assetID)
		if err != nil || !found || got.BookID != book.ID || got.AssetID != want.assetID || got.SchemaVersion != want.version || string(got.Locator) != want.locator || got.UpdatedAt == "" {
			t.Fatalf("location for %q = %#v found=%v err=%v", want.assetID, got, found, err)
		}
	}
	progress, err := s.ReadingProgress(book.ID)
	if err != nil || progress.ChapterIndex != 4 || progress.Position != 0.375 {
		t.Fatalf("legacy progress changed: %#v err=%v", progress, err)
	}
}

func TestSaveReaderLocationRejectsInvalidValuesAndAssets(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	source := filepath.Join(t.TempDir(), "book.epub")
	fixtureEPUB(t, source, "Book")
	book, _, err := s.ImportEPUB(source)
	if err != nil {
		t.Fatal(err)
	}
	asset, _, err := s.BookFileSourceByFormat(book.ID, "epub")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		bookID  string
		assetID string
		version int
		locator json.RawMessage
	}{
		{name: "invalid book", bookID: "../bad", assetID: asset.ID, version: 1, locator: json.RawMessage(`{}`)},
		{name: "empty asset", bookID: book.ID, version: 1, locator: json.RawMessage(`{}`)},
		{name: "unknown asset", bookID: book.ID, assetID: "missing", version: 1, locator: json.RawMessage(`{}`)},
		{name: "zero version", bookID: book.ID, assetID: asset.ID, version: 0, locator: json.RawMessage(`{}`)},
		{name: "negative version", bookID: book.ID, assetID: asset.ID, version: -1, locator: json.RawMessage(`{}`)},
		{name: "malformed json", bookID: book.ID, assetID: asset.ID, version: 1, locator: json.RawMessage(`{"position":`)},
		{name: "empty json", bookID: book.ID, assetID: asset.ID, version: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := s.SaveReaderLocation(test.bookID, test.assetID, test.version, test.locator); err == nil {
				t.Fatal("invalid location was accepted")
			}
		})
	}
	if _, found, err := s.GetReaderLocation(book.ID, asset.ID); err != nil || found {
		t.Fatalf("invalid writes persisted: found=%v err=%v", found, err)
	}
	if _, found, err := s.GetReaderLocation(book.ID, "missing"); err != nil || found {
		t.Fatalf("missing location = found %v err %v", found, err)
	}
	if _, _, err := s.GetReaderLocation("../bad", asset.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid book lookup err = %v", err)
	}
}
