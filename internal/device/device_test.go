package device

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"attune/internal/model"
)

func testManager(root string, id string, free uint64) *Manager {
	v := volume{Root: root, ID: id, Name: "USB Disk", External: true}
	return &Manager{volumes: func() ([]volume, error) { return []volume{v}, nil }, space: func(string) (uint64, uint64, error) { return free, free, nil }, copy: copyVerified}
}

func selected(root, id string) model.Device { return model.Device{Root: root, ID: id} }

func TestSelectAndDiscoverRequireMountedExternalVolume(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "documents"), 0755)
	m := testManager(root, "usb-1", 100)
	if ds, err := m.Discover(); err != nil || len(ds) != 0 {
		t.Fatalf("generic Documents volume was auto-detected: %+v %v", ds, err)
	}
	if err := os.Mkdir(filepath.Join(root, "system"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "system", "version.txt"), []byte("generic version 1"), 0600); err != nil {
		t.Fatal(err)
	}
	if ds, err := m.Discover(); err != nil || len(ds) != 0 {
		t.Fatalf("generic version marker was auto-detected: %+v %v", ds, err)
	}
	if err := os.WriteFile(filepath.Join(root, "system", "version.txt"), []byte("Kindle 2.5.8"), 0600); err != nil {
		t.Fatal(err)
	}
	if ds, err := m.Discover(); err != nil || len(ds) != 1 {
		t.Fatalf("Kindle version marker not detected: %+v %v", ds, err)
	}
	if _, err := m.SelectVolume(root); err != nil {
		t.Fatalf("explicit external Documents volume should be selectable: %v", err)
	}
	if _, err := m.SelectVolume(t.TempDir()); err == nil {
		t.Fatal("unmounted folder selected")
	}
}

func TestDiscoverKnownNonKindleAndFilterByProfileFormats(t *testing.T) {
	root := t.TempDir()
	books := filepath.Join(root, "Books")
	if err := os.Mkdir(books, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"story.epub": "epub", "manual.pdf": "pdf", "other.cbr": "comic", "notes.txt": "text",
	} {
		if err := os.WriteFile(filepath.Join(books, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	v := volume{Root: root, ID: "kobo-1", Name: "Kobo Clara", External: true}
	m := testManager(root, v.ID, 1<<20)
	m.volumes = func() ([]volume, error) { return []volume{v}, nil }
	discovered, err := m.Discover()
	if err != nil || len(discovered) != 1 {
		t.Fatalf("Discover() = %+v, %v", discovered, err)
	}
	device := discovered[0]
	if device.ProfileID != "kobo" || device.Transport != "usb-storage" || device.RequiresExplicitSelection {
		t.Fatalf("unexpected discovered Kobo profile: %+v", device)
	}
	booksFound, err := m.List(device)
	if err != nil {
		t.Fatal(err)
	}
	if len(booksFound) != 2 {
		t.Fatalf("Kobo profile should list EPUB/PDF only, got %+v", booksFound)
	}
	for _, book := range booksFound {
		if book.Name != "story.epub" && book.Name != "manual.pdf" {
			t.Errorf("unsupported Kobo transfer format listed: %s", book.Name)
		}
	}
	source := filepath.Join(t.TempDir(), "book.mobi")
	if err := os.WriteFile(source, []byte("mobi"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Send(context.Background(), device, source, "book.mobi"); err == nil {
		t.Fatal("unsupported MOBI sent to Kobo profile")
	}
}

func TestUnknownMountedVolumeRequiresManualSelectionAndUsesGenericFolders(t *testing.T) {
	root := t.TempDir()
	booksDir := filepath.Join(root, "Books")
	docsDir := filepath.Join(root, "Documents")
	if err := os.Mkdir(booksDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	for path, contents := range map[string]string{
		filepath.Join(booksDir, "novel.fb2"):  "fb2",
		filepath.Join(docsDir, "guide.pdf"):   "pdf",
		filepath.Join(docsDir, "archive.cbr"): "cbr",
	} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	v := volume{Root: root, ID: "usb-unknown", Name: "Removable disk", External: true}
	m := testManager(root, v.ID, 1<<20)
	m.volumes = func() ([]volume, error) { return []volume{v}, nil }
	if devices, err := m.Discover(); err != nil || len(devices) != 0 {
		t.Fatalf("unknown storage should not be auto-discovered: %+v, %v", devices, err)
	}
	device, err := m.SelectVolume(root)
	if err != nil {
		t.Fatal(err)
	}
	if device.ProfileID != "generic" || !device.RequiresExplicitSelection || device.Transport != "usb-storage" {
		t.Fatalf("unexpected generic selected-volume profile: %+v", device)
	}
	booksFound, err := m.List(device)
	if err != nil {
		t.Fatal(err)
	}
	if len(booksFound) != 3 {
		t.Fatalf("generic profile should list transferable formats in candidate folders, got %+v", booksFound)
	}
	source := filepath.Join(t.TempDir(), "book.txt")
	if err := os.WriteFile(source, []byte("text"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Send(context.Background(), device, source, "new book.txt"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(booksDir, "new book.txt")); err != nil || string(got) != "text" {
		t.Fatalf("generic transfer did not use preferred Books folder: %q, %v", got, err)
	}
}

func TestSelectedUnknownVolumeCanCreateBooksFolder(t *testing.T) {
	root := t.TempDir()
	v := volume{Root: root, ID: "usb-empty", Name: "Generic disk", External: true}
	m := testManager(root, v.ID, 1<<20)
	m.volumes = func() ([]volume, error) { return []volume{v}, nil }
	device, err := m.SelectVolume(root)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "book.epub")
	if err := os.WriteFile(source, []byte("epub"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Send(context.Background(), device, source, "book.epub"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Books", "book.epub")); err != nil {
		t.Fatalf("Books candidate folder was not created: %v", err)
	}
}

func TestNestedBookFolderPreferencesListSendAndRemove(t *testing.T) {
	root := t.TempDir()
	volumeInfo := volume{Root: root, ID: "nested-usb", Name: "Generic disk", External: true}
	m := testManager(root, volumeInfo.ID, 1<<20)
	m.volumes = func() ([]volume, error) { return []volume{volumeInfo}, nil }
	device, err := m.SelectVolume(root)
	if err != nil {
		t.Fatal(err)
	}
	device.BookFolders = []string{"Library/Fiction"}
	device.TransferFormats = []string{"epub"}
	bookFolder := filepath.Join(root, "Library", "Fiction")
	if err := os.MkdirAll(bookFolder, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bookFolder, "existing.epub"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bookFolder, "ignored.pdf"), []byte("pdf"), 0600); err != nil {
		t.Fatal(err)
	}
	listed, err := m.List(device)
	if err != nil || len(listed) != 1 || listed[0].Path != "Library/Fiction/existing.epub" {
		t.Fatalf("nested folder listing = %+v, %v", listed, err)
	}
	source := filepath.Join(t.TempDir(), "new.epub")
	if err := os.WriteFile(source, []byte("new book"), 0600); err != nil {
		t.Fatal(err)
	}
	rel, err := m.Send(context.Background(), device, source, "new.epub")
	if err != nil || rel != "Library/Fiction/new.epub" {
		t.Fatalf("nested transfer path = %q, %v", rel, err)
	}
	if err := m.Remove(device, rel); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(bookFolder, "new.epub")); !os.IsNotExist(err) {
		t.Fatalf("nested transfer still exists after removal: %v", err)
	}
	if err := m.Remove(device, "Library/../outside.epub"); err == nil {
		t.Fatal("removal accepted traversal path")
	}
}

func TestNestedBookFoldersRejectSymlinksAndUnsafeAncestors(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	volumeInfo := volume{Root: root, ID: "unsafe-usb", Name: "Generic disk", External: true}
	m := testManager(root, volumeInfo.ID, 1<<20)
	m.volumes = func() ([]volume, error) { return []volume{volumeInfo}, nil }
	device, err := m.SelectVolume(root)
	if err != nil {
		t.Fatal(err)
	}
	device.BookFolders = []string{"linked/books"}
	if _, err := m.List(device); err == nil {
		t.Fatal("listing traversed a symlinked book-folder ancestor")
	}
	source := filepath.Join(t.TempDir(), "book.epub")
	if err := os.WriteFile(source, []byte("book"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Send(context.Background(), device, source, "book.epub"); err == nil {
		t.Fatal("transfer created content through a symlinked book-folder ancestor")
	}
	device.BookFolders = []string{"../outside"}
	if _, err := m.List(device); err == nil {
		t.Fatal("listing accepted a traversal folder preference")
	}
}

func TestMissingDeviceRootReturnsDisconnectedError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Kindle")
	m := testManager(root, "usb-1", 100)
	want := "Kindle disconnected; reconnect it and try again"

	if _, err := m.SelectVolume(root); err == nil || err.Error() != want {
		t.Fatalf("SelectVolume error = %v, want %q", err, want)
	}
	if _, err := m.selectExpected(selected(root, "usb-1")); err == nil || err.Error() != want {
		t.Fatalf("selectExpected error = %v, want %q", err, want)
	}
	if _, err := m.findVolume(root); err == nil || err.Error() != want {
		t.Fatalf("findVolume error = %v, want %q", err, want)
	}
}

func TestSendCopiesVerifiesAndRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "documents")
	os.Mkdir(docs, 0755)
	source := filepath.Join(t.TempDir(), "book.epub")
	os.WriteFile(source, []byte("book bytes"), 0600)
	m := testManager(root, "usb-1", 1<<20)
	rel, err := m.Send(context.Background(), selected(root, "usb-1"), source, "My Book.mobi")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "documents/My Book.mobi" {
		t.Fatal(rel)
	}
	b, err := os.ReadFile(filepath.Join(docs, "My Book.mobi"))
	if err != nil || string(b) != "book bytes" {
		t.Fatalf("bad transfer: %q %v", b, err)
	}
	if _, err = m.Send(context.Background(), selected(root, "usb-1"), source, "My Book.mobi"); err == nil {
		t.Fatal("existing target overwritten")
	}
	if err = os.Remove(filepath.Join(docs, "My Book.mobi")); err != nil {
		t.Fatal(err)
	}
	m.copy = func(ctx context.Context, source, temp string) (string, error) {
		digest, e := copyVerified(ctx, source, temp)
		if e == nil {
			e = os.WriteFile(filepath.Join(docs, "My Book.mobi"), []byte("created concurrently"), 0600)
		}
		return digest, e
	}
	if _, err = m.Send(context.Background(), selected(root, "usb-1"), source, "My Book.mobi"); err == nil {
		t.Fatal("concurrent target was overwritten")
	}
	if got, _ := os.ReadFile(filepath.Join(docs, "My Book.mobi")); string(got) != "created concurrently" {
		t.Fatalf("concurrent file changed: %q", got)
	}
	if _, err = m.Send(context.Background(), selected(root, "usb-1"), source, "../evil.mobi"); err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestSendChecksSpaceDisconnectAndCancellation(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "documents"), 0755)
	source := filepath.Join(t.TempDir(), "book.epub")
	os.WriteFile(source, []byte("12345"), 0600)
	m := testManager(root, "usb-1", 2)
	if _, err := m.Send(context.Background(), selected(root, "usb-1"), source, "b.mobi"); err == nil {
		t.Fatal("insufficient space accepted")
	}
	m = testManager(root, "usb-1", 100)
	m.copy = func(ctx context.Context, _, _ string) (string, error) {
		return "", errors.New("simulated copy failure")
	}
	if _, err := m.Send(context.Background(), selected(root, "usb-1"), source, "b.mobi"); err == nil {
		t.Fatal("copy failure ignored")
	}
	m = testManager(root, "usb-1", 100)
	calls := 0
	m.volumes = func() ([]volume, error) {
		calls++
		id := "usb-1"
		if calls > 1 {
			id = "usb-2"
		}
		return []volume{{Root: root, ID: id, Name: "USB Disk", External: true}}, nil
	}
	if _, err := m.Send(context.Background(), selected(root, "usb-1"), source, "b.mobi"); err == nil {
		t.Fatal("changed volume identity accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m = testManager(root, "usb-1", 100)
	if _, err := m.Send(ctx, selected(root, "usb-1"), source, "b.mobi"); err == nil {
		t.Fatal("cancelled transfer succeeded")
	}
}

func TestSendRejectsReplacementBeforeTransfer(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "documents")
	if err := os.Mkdir(docs, 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "book.epub")
	if err := os.WriteFile(source, []byte("book"), 0600); err != nil {
		t.Fatal(err)
	}
	m := testManager(root, "usb-2", 100)
	if _, err := m.Send(context.Background(), selected(root, "usb-1"), source, "book.mobi"); err == nil {
		t.Fatal("replacement device accepted")
	}
	if _, err := os.Lstat(filepath.Join(docs, "book.mobi")); !os.IsNotExist(err) {
		t.Fatalf("replacement received file: %v", err)
	}
}

func TestRemovePreservesAnnotationsAndRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "documents")
	os.Mkdir(docs, 0755)
	os.WriteFile(filepath.Join(docs, "book.mobi"), []byte("ebook"), 0600)
	os.WriteFile(filepath.Join(docs, "older.prc"), []byte("prc"), 0600)
	os.WriteFile(filepath.Join(docs, "My Clippings.txt"), []byte("annotations"), 0600)
	fileLink := filepath.Join(docs, "linked.epub")
	fileLinkErr := os.Symlink("book.mobi", fileLink)
	m := testManager(root, "usb-1", 100)
	os.MkdirAll(filepath.Join(docs, "Author", "Book"), 0755)
	os.WriteFile(filepath.Join(docs, "Author", "Book", "nested.prc"), []byte("nested"), 0600)
	os.WriteFile(filepath.Join(docs, "Author", "Book", "nested.mbp"), []byte("sidecar"), 0600)
	os.WriteFile(filepath.Join(docs, "._Book.mobi"), []byte("AppleDouble metadata"), 0600)
	os.WriteFile(filepath.Join(docs, ".hidden.epub"), []byte("hidden book"), 0600)
	os.MkdirAll(filepath.Join(docs, ".metadata"), 0755)
	os.WriteFile(filepath.Join(docs, ".metadata", "hidden.mobi"), []byte("hidden metadata"), 0600)
	os.WriteFile(filepath.Join(docs, "Author", "Book", ".hidden.epub"), []byte("hidden nested book"), 0600)
	os.MkdirAll(filepath.Join(docs, "Book.sdr"), 0755)
	os.WriteFile(filepath.Join(docs, "Book.sdr", "duplicate.azw3"), []byte("companion file"), 0600)
	books, err := m.List(selected(root, "usb-1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 3 {
		t.Fatalf("expected MOBI, PRC, and nested PRC books, got %+v", books)
	}
	for _, hidden := range []string{"._Book.mobi", ".hidden.epub", ".metadata/hidden.mobi", "Author/Book/.hidden.epub"} {
		if _, err := os.Stat(filepath.Join(docs, hidden)); err != nil {
			t.Fatalf("listing changed hidden metadata %q: %v", hidden, err)
		}
	}
	if err := m.Remove(selected(root, "usb-1"), "documents/book.mobi"); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(selected(root, "usb-1"), "documents/older.prc"); err != nil {
		t.Fatalf("PRC was not removable: %v", err)
	}
	if err := m.Remove(selected(root, "usb-1"), "documents/Author/Book/nested.prc"); err != nil {
		t.Fatalf("nested PRC was not removable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(docs, "Author", "Book", "nested.mbp")); err != nil {
		t.Fatal("sidecar was removed", err)
	}
	if _, err := os.Stat(filepath.Join(docs, "My Clippings.txt")); err != nil {
		t.Fatal("annotations removed", err)
	}
	if fileLinkErr == nil {
		if err := m.Remove(selected(root, "usb-1"), "documents/linked.epub"); err == nil {
			t.Fatal("symlink removed")
		}
	}
	if err := m.Remove(selected(root, "usb-1"), "documents/My Clippings.txt"); err == nil {
		t.Fatal("annotation file removed")
	}
	if err := os.MkdirAll(filepath.Join(docs, "safe"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(docs, "safe"), filepath.Join(docs, "linked-dir")); err == nil {
		if err := m.Remove(selected(root, "usb-1"), "documents/linked-dir/book.mobi"); err == nil {
			t.Fatal("path through symlink directory accepted")
		}
	}
	// Replacing the volume after path validation must prevent deletion.
	if err := os.WriteFile(filepath.Join(docs, "Author", "Book", "keep.mobi"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	m.volumes = func() ([]volume, error) {
		calls++
		id := "usb-1"
		if calls > 1 {
			id = "usb-2"
		}
		return []volume{{Root: root, ID: id, External: true}}, nil
	}
	if err := m.Remove(selected(root, "usb-1"), "documents/Author/Book/keep.mobi"); err == nil {
		t.Fatal("changed device identity accepted")
	}
	if _, err := os.Stat(filepath.Join(docs, "Author", "Book", "keep.mobi")); err != nil {
		t.Fatal("file removed after identity change", err)
	}
}

func TestListPropagatesWalkErrors(t *testing.T) {
	root := t.TempDir()
	if _, err := listBooks(filepath.Join(root, "missing-documents"), root, "test-volume", &Manager{}); err == nil {
		t.Fatal("directory walk failure was reported as an empty device library")
	}
}

func TestCommitTransferFallsBackWhenFilesystemLacksExclusiveRename(t *testing.T) {
	dir := t.TempDir()
	temp := filepath.Join(dir, ".transfer.tmp")
	target := filepath.Join(dir, "book.mobi")
	if err := os.WriteFile(temp, []byte("verified ebook"), 0600); err != nil {
		t.Fatal(err)
	}
	unsupported := func() error { return errors.ErrUnsupported }
	expected, err := hashFile(temp)
	if err != nil {
		t.Fatal(err)
	}
	if err := commitTransferWith(context.Background(), temp, target, expected, unsupported); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "verified ebook" {
		t.Fatalf("fallback rename result %q: %v", b, err)
	}

	if err := os.WriteFile(temp, []byte("new book"), 0600); err != nil {
		t.Fatal(err)
	}
	concurrent := func() error {
		if e := os.WriteFile(target, []byte("created during commit"), 0600); e != nil {
			return e
		}
		return errors.ErrUnsupported
	}
	if err := commitTransferWith(context.Background(), temp, target, expected, concurrent); !os.IsExist(err) {
		t.Fatalf("concurrent target should be preserved, got %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "created during commit" {
		t.Fatalf("fallback overwrote destination: %q", b)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := copyToExclusiveTarget(cancelled, temp, target, expected); err == nil {
		t.Fatal("cancelled fallback copy succeeded")
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("failed fallback left a partial final file: %v", err)
	}
}
