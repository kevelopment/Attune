package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"attune/internal/library"
	"attune/internal/model"
)

func relocationTestApp(t *testing.T) (*App, string) {
	t.Helper()
	a := testApp(t)
	a.configPath = filepath.Join(t.TempDir(), "settings", "settings.json")
	oldRoot := a.root
	t.Cleanup(func() {
		a.mu.Lock()
		store := a.store
		a.mu.Unlock()
		if store != nil {
			_ = store.Close()
		}
	})
	return a, oldRoot
}

func TestRelocateLibraryPreservesBooksAndPersistsLocation(t *testing.T) {
	a, oldRoot := relocationTestApp(t)
	source := filepath.Join(t.TempDir(), "book.epub")
	writeTestEPUB(t, source, "Relocated Book")
	if result, err := a.ImportPaths([]string{source}); err != nil || result.Imported != 1 {
		t.Fatalf("import = %#v, %v", result, err)
	}
	books, err := a.store.ListBooks()
	if err != nil || len(books) != 1 {
		t.Fatalf("books = %#v, %v", books, err)
	}
	bookID := books[0].ID
	_, originalSourcePath, err := a.store.BookSource(bookID)
	if err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(originalSourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.store.UpdateBookMetadata(bookID, model.BookMetadataEdit{
		Title: "Edited Relocated Book", Authors: []string{"Editor"}, Publisher: "Edited Publisher",
		Language: "fr", Published: "2024", Subjects: []string{"Edited Subject"},
		Series: "Edited Series", SeriesIndex: "7", Description: "Edited description",
	}); err != nil {
		t.Fatal(err)
	}
	if err = a.store.SaveConversion(bookID, "regenerable", filepath.Join(oldRoot, "conversions", "old.mobi")); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(resolvedParent, "Attune Library")
	envRoot := filepath.Join(t.TempDir(), "development-library")
	a.configPath = envRoot + ".settings.json"
	got, err := a.relocateLibrary(want)
	if err != nil || got != want {
		t.Fatalf("relocate = %q, %v; want %q", got, err, want)
	}
	if _, err = os.Stat(filepath.Join(oldRoot, "library.sqlite")); err != nil {
		t.Fatalf("original library was not preserved: %v", err)
	}
	if a.root != want {
		t.Fatalf("active root = %q, want %q", a.root, want)
	}
	if saved, found, err := loadLibraryLocation(a.configPath); err != nil || !found || saved != want {
		t.Fatalf("saved location = %q, %v, %v", saved, found, err)
	}
	if _, err = a.store.GetConversion("regenerable"); err != nil {
		t.Fatal(err)
	} else if path, _ := a.store.GetConversion("regenerable"); path != "" {
		t.Fatalf("conversion cache was copied: %q", path)
	}
	book, sourcePath, err := a.store.BookSource(bookID)
	if err != nil || book.Title != "Edited Relocated Book" || book.Publisher != "Edited Publisher" || book.Language != "fr" || book.SeriesIndex != "7" || book.Description != "Edited description" || len(book.Authors) != 1 || book.Authors[0] != "Editor" {
		t.Fatalf("book after relocation = %#v, %v", book, err)
	}
	if _, err = os.Stat(sourcePath); err != nil {
		t.Fatalf("managed original missing: %v", err)
	}
	if relocatedInfo, err := os.Stat(sourcePath); err != nil || !relocatedInfo.ModTime().Equal(originalInfo.ModTime()) {
		t.Fatalf("managed original modification time changed: got %v, want %v (err %v)", relocatedInfo, originalInfo.ModTime(), err)
	}
	cover, err := a.store.CoverPath(bookID)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(cover); err != nil || len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("cover after relocation = %q, %v", data, err)
	}
	if err = a.store.Close(); err != nil {
		t.Fatal(err)
	}
	a.store, err = library.Open(want)
	if err != nil {
		t.Fatal(err)
	}
	books, err = a.store.ListBooks()
	if err != nil || len(books) != 1 || books[0].Title != "Edited Relocated Book" || books[0].Publisher != "Edited Publisher" {
		t.Fatalf("reopened books = %#v, %v", books, err)
	}
	t.Setenv("EBOOK_LIBRARY_HOME", envRoot)
	started := NewApp()
	started.Startup(context.Background())
	if started.initErr != nil {
		t.Fatalf("startup with persisted library location: %v", started.initErr)
	}
	if started.root != want {
		t.Fatalf("startup root = %q, want persisted %q", started.root, want)
	}
	started.Shutdown(context.Background())
}

func TestRelocateLibraryRejectsUnsafeDestinationWithoutChanges(t *testing.T) {
	a, oldRoot := relocationTestApp(t)
	parent := t.TempDir()
	nonempty := filepath.Join(parent, "nonempty")
	if err := os.Mkdir(nonempty, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(nonempty, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, destination := range map[string]string{
		"non-empty": nonempty,
		"nested":    filepath.Join(oldRoot, "nested"),
		"same":      oldRoot,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := a.relocateLibrary(destination); err == nil {
				t.Fatalf("relocate to %q unexpectedly succeeded", destination)
			}
			if _, err := os.Stat(filepath.Join(oldRoot, "library.sqlite")); err != nil {
				t.Fatalf("source library changed: %v", err)
			}
		})
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
		t.Fatalf("non-empty destination changed: %q, %v", data, err)
	}
}

func TestRelocateLibraryRejectsBusyAndRollsBackConfigFailure(t *testing.T) {
	a, oldRoot := relocationTestApp(t)
	parent := t.TempDir()
	a.busy = true
	if _, err := a.relocateLibrary(filepath.Join(parent, "busy")); err == nil {
		t.Fatal("relocation while busy unexpectedly succeeded")
	}
	a.busy = false
	settingsDirectory := filepath.Join(t.TempDir(), "settings.json")
	if err := os.Mkdir(settingsDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	a.configPath = settingsDirectory
	destination := filepath.Join(parent, "Attune Library")
	if _, err := a.relocateLibrary(destination); err == nil {
		t.Fatal("relocation with unwritable settings path unexpectedly succeeded")
	}
	if a.root != oldRoot {
		t.Fatalf("root changed after failed relocation: %q", a.root)
	}
	if _, err := os.Stat(filepath.Join(oldRoot, "library.sqlite")); err != nil {
		t.Fatalf("source library missing after failed relocation: %v", err)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed destination should be removed, stat error=%v", err)
	}
}

func TestConfiguredMissingLibraryFailsWithoutCreatingIt(t *testing.T) {
	envRoot := filepath.Join(t.TempDir(), "development-library")
	configured := filepath.Join(t.TempDir(), "missing-library")
	settingsPath := envRoot + ".settings.json"
	if err := saveLibraryLocation(settingsPath, configured); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EBOOK_LIBRARY_HOME", envRoot)
	a := NewApp()
	a.Startup(context.Background())
	if a.initErr == nil {
		t.Fatal("startup accepted configured library without library.sqlite")
	}
	if _, err := os.Stat(configured); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing configured library was created: %v", err)
	}
}

func TestMalformedLibrarySettingsFailsStartup(t *testing.T) {
	envRoot := filepath.Join(t.TempDir(), "development-library")
	if err := os.WriteFile(envRoot+".settings.json", []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EBOOK_LIBRARY_HOME", envRoot)
	a := NewApp()
	a.Startup(context.Background())
	if a.initErr == nil {
		t.Fatal("startup accepted malformed library settings")
	}
	if _, err := os.Stat(envRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("malformed settings created a library: %v", err)
	}
}

func TestLibrarySettingsPathIsolatedForDevelopment(t *testing.T) {
	envRoot := filepath.Join(t.TempDir(), "dev-library")
	got, err := librarySettingsPath(envRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got != envRoot+".settings.json" {
		t.Fatalf("development settings path = %q", got)
	}
}
