package library

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"attune/internal/model"
	_ "github.com/mattn/go-sqlite3"
)

func TestDevicePreferencesMigratePersistAndClear(t *testing.T) {
	root := t.TempDir()
	// Simulate an older library database that has device tracking but not the
	// preference table introduced by this migration.
	db, err := sql.Open("sqlite3", filepath.Join(root, "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE devices (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, root TEXT NOT NULL,
		free_bytes INTEGER NOT NULL, total_bytes INTEGER NOT NULL
	)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if folders, formats, found, err := s.LoadDevicePreferences("device-1"); err != nil || found || folders != nil || formats != nil {
		t.Fatalf("missing preferences = %v, %v, %v, %v", folders, formats, found, err)
	}
	if err = s.RecordDevice(testPreferenceDevice()); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveDevicePreferences("device-1", []string{"Books/To Read", "Documents"}, []string{"EPUB", ".PDF", "epub"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	folders, formats, found, err := s.LoadDevicePreferences("device-1")
	if err != nil || !found {
		t.Fatalf("LoadDevicePreferences found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(folders, []string{"Books/To Read", "Documents"}) {
		t.Fatalf("folders = %v", folders)
	}
	if !reflect.DeepEqual(formats, []string{"epub", "pdf"}) {
		t.Fatalf("formats = %v", formats)
	}
	if err = s.ClearDevicePreferences("device-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, found, err = s.LoadDevicePreferences("device-1"); err != nil || found {
		t.Fatalf("preferences remained after clear: found=%v err=%v", found, err)
	}
}

func TestSaveDevicePreferencesValidatesDeviceAndPaths(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.RecordDevice(testPreferenceDevice()); err != nil {
		t.Fatal(err)
	}
	for _, folders := range [][]string{{"../outside"}, {"Books/../../outside"}, {"/absolute/path"}, {`C:\Books`}} {
		if err = s.SaveDevicePreferences("device-1", folders, []string{"epub"}); err == nil {
			t.Errorf("unsafe folder path accepted: %q", folders)
		}
	}
	if err = s.SaveDevicePreferences("device-1", []string{"Books"}, []string{"unknown-format"}); err == nil {
		t.Fatal("unsupported format accepted")
	}
	if err = s.SaveDevicePreferences("missing-device", []string{"Books"}, []string{"epub"}); err == nil {
		t.Fatal("preferences saved for an unrecorded device")
	}
}

func testPreferenceDevice() model.Device {
	return model.Device{ID: "device-1", Name: "Reader", Root: "/media/reader"}
}
