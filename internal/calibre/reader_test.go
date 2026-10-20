package calibre

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func makeLibrary(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Author", "Book (1)"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "作者", "本 (2)"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Author", "Missing (3)"), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
		PRAGMA user_version=28;
		CREATE TABLE books(id INTEGER PRIMARY KEY, title TEXT, timestamp TEXT, pubdate TEXT, author_sort TEXT, sort TEXT, series_index REAL, path TEXT, has_cover INTEGER, last_modified TEXT);
		CREATE TABLE data(id INTEGER PRIMARY KEY, book INTEGER, format TEXT, uncompressed_size INTEGER, name TEXT);
		CREATE TABLE authors(id INTEGER PRIMARY KEY, name TEXT);
		CREATE TABLE books_authors_link(id INTEGER PRIMARY KEY, book INTEGER, author INTEGER);
		CREATE TABLE tags(id INTEGER PRIMARY KEY, name TEXT);
		CREATE TABLE books_tags_link(id INTEGER PRIMARY KEY, book INTEGER, tag INTEGER);
		CREATE TABLE languages(id INTEGER PRIMARY KEY, lang_code TEXT);
		CREATE TABLE books_languages_link(id INTEGER PRIMARY KEY, book INTEGER, lang_code INTEGER, item_order INTEGER);
		CREATE TABLE publishers(id INTEGER PRIMARY KEY, name TEXT);
		CREATE TABLE books_publishers_link(id INTEGER PRIMARY KEY, book INTEGER, publisher INTEGER);
		CREATE TABLE series(id INTEGER PRIMARY KEY, name TEXT);
		CREATE TABLE books_series_link(id INTEGER PRIMARY KEY, book INTEGER, series INTEGER);
		CREATE TABLE identifiers(id INTEGER PRIMARY KEY, book INTEGER, type TEXT, val TEXT);
		CREATE TABLE comments(id INTEGER PRIMARY KEY, book INTEGER, text TEXT);
		CREATE TABLE books_pages_link(book INTEGER PRIMARY KEY, pages INTEGER);
		CREATE TABLE custom_column_1(book INTEGER PRIMARY KEY, value TEXT);
		INSERT INTO books VALUES(1,'Title from DB','2024-01-02 03:04:05+00:00','2020-05-06 00:00:00+00:00','Given, Family','DB Sort',2.5,'Author/Book (1)',1,'2024-01-03 00:00:00+00:00');
		INSERT INTO books VALUES(2,'書名','2024-02-03 00:00:00+00:00',NULL,NULL,NULL,1,'作者/本 (2)',0,'2024-02-04 00:00:00+00:00');
		INSERT INTO books VALUES(3,'Missing EPUB','2024-03-03 00:00:00+00:00',NULL,NULL,NULL,1,'Author/Missing (3)',0,'2024-03-04 00:00:00+00:00');
		INSERT INTO books VALUES(4,'Bad path','2024-04-03 00:00:00+00:00',NULL,NULL,NULL,1,'../../outside',0,'2024-04-04 00:00:00+00:00');
		INSERT INTO authors VALUES(1,'Family, Given'); INSERT INTO authors VALUES(2,'Coauthor');
		INSERT INTO books_authors_link VALUES(1,1,1); INSERT INTO books_authors_link VALUES(2,1,2); INSERT INTO books_authors_link VALUES(3,2,1);
		INSERT INTO tags VALUES(1,'Unicode — Tag'); INSERT INTO books_tags_link VALUES(1,1,1);
		INSERT INTO languages VALUES(1,'eng'); INSERT INTO languages VALUES(2,'jpn');
		INSERT INTO books_languages_link VALUES(1,1,1,0); INSERT INTO books_languages_link VALUES(2,1,2,1);
		INSERT INTO publishers VALUES(1,'Publisher'); INSERT INTO books_publishers_link VALUES(1,1,1);
		INSERT INTO series VALUES(1,'Series'); INSERT INTO books_series_link VALUES(1,1,1);
		INSERT INTO identifiers VALUES(1,1,'isbn','9781234567890');
		INSERT INTO comments VALUES(1,1,'<p>DB description</p>');
		INSERT INTO books_pages_link VALUES(1,321);
		INSERT INTO custom_column_1 VALUES(1,'keep me');
		INSERT INTO data VALUES(1,1,'EPUB',123,'Title - Author');
		INSERT INTO data VALUES(2,1,'PDF',456,'Title - Author');
		INSERT INTO data VALUES(3,2,'PDF',789,'本 - 作者');
		INSERT INTO data VALUES(4,3,'EPUB',100,'Missing EPUB');
		INSERT INTO data VALUES(5,4,'EPUB',100,'Bad path');
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		ALTER TABLE books ADD COLUMN uuid TEXT;
		UPDATE books SET uuid='book-uuid-1' WHERE id=1;
		CREATE TABLE library_id(id INTEGER PRIMARY KEY, uuid TEXT);
		INSERT INTO library_id VALUES(1,'library-uuid');
		CREATE TABLE custom_columns(id INTEGER PRIMARY KEY, label TEXT, name TEXT, datatype TEXT);
		INSERT INTO custom_columns VALUES(1,'Shelf note','shelf_note','text');
		CREATE TABLE last_read_positions(book INTEGER, format TEXT, user TEXT, device TEXT, cfi TEXT, epoch REAL, pos_frac REAL);
		INSERT INTO last_read_positions VALUES(1,'EPUB','viewer','device-1','epubcfi(/6/2)',1700000000,0.25);
		CREATE TABLE annotations(book INTEGER, format TEXT, user_type TEXT, user TEXT, timestamp REAL, annot_id TEXT, annot_type TEXT, annot_data TEXT, searchable_text TEXT);
		INSERT INTO annotations VALUES(1,'EPUB','local','user-1',1700000001,'annotation-1','highlight','{"cfi":"epubcfi(/6/4)"}','highlight text');
		INSERT INTO annotations VALUES(1,'EPUB','local','user-1',1700000002,'annotation-2','bookmark','{"cfi":"epubcfi(/6/6)"}','');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Author", "Book (1)", "Title - Author.epub"), []byte("epub source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Author", "Book (1)", "cover.jpg"), []byte("cover"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "作者", "本 (2)", "本 - 作者.pdf"), []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestReadLibraryMetadataAndFormats(t *testing.T) {
	root := makeLibrary(t)
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	books, err := ReadLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 4 {
		t.Fatalf("got %d records, want all 4", len(books))
	}
	first := books[0]
	if first.ID != "1" || first.Metadata.Title != "Title from DB" {
		t.Fatalf("unexpected DB record identity/title: %#v", first)
	}
	if !reflect.DeepEqual(first.Metadata.Authors, []string{"Family, Given", "Coauthor"}) {
		t.Fatalf("authors not kept in Calibre link order: %#v", first.Metadata.Authors)
	}
	if !reflect.DeepEqual(first.Metadata.Subjects, []string{"Unicode — Tag"}) || first.Metadata.Language != "eng" {
		t.Fatalf("tags/language not read: %#v", first.Metadata)
	}
	if first.Metadata.Publisher != "Publisher" || first.Metadata.Series != "Series" || first.Metadata.SeriesIndex != "2.5" {
		t.Fatalf("publisher/series fields missing: %#v", first.Metadata)
	}
	if first.Metadata.Identifiers["isbn"] != "9781234567890" || first.Metadata.Description != "<p>DB description</p>" {
		t.Fatalf("identifier/description missing: %#v", first.Metadata)
	}
	if first.Metadata.TitleSort != "DB Sort" || !reflect.DeepEqual(first.Metadata.AuthorSort, []string{"Given, Family"}) {
		t.Fatalf("sort metadata missing: %#v", first.Metadata)
	}
	if !strings.Contains(first.Metadata.AddedAt, "2024-01-02T03:04:05Z") || first.Metadata.Published != "2020-05-06T00:00:00Z" {
		t.Fatalf("dates were not normalized: %#v", first.Metadata)
	}
	if !reflect.DeepEqual(first.Formats, []string{"EPUB", "PDF"}) || first.EPUBPath != filepath.Join(canonicalRoot, "Author", "Book (1)", "Title - Author.epub") {
		t.Fatalf("EPUB/format lookup failed: %#v", first)
	}
	if len(first.Assets) != 2 || first.Assets[0].Format != "EPUB" || first.Assets[0].Path != first.EPUBPath || first.Assets[0].Name != "Title - Author.epub" || first.Assets[0].Size != 123 {
		t.Fatalf("per-format assets were not exposed: %#v", first.Assets)
	}
	if first.Assets[1].Format != "PDF" || first.Assets[1].Path != "" || first.Assets[1].Issue == "" || first.EPUBPath == "" {
		t.Fatalf("missing PDF should be isolated from the available EPUB: %#v", first.Assets)
	}
	if first.LibraryUUID != "library-uuid" || first.UUID != "book-uuid-1" {
		t.Fatalf("library/book UUIDs missing: library=%q book=%q", first.LibraryUUID, first.UUID)
	}
	var raw map[string]any
	if err := json.Unmarshal(first.RawMetadata, &raw); err != nil || raw["uuid"] != "book-uuid-1" {
		t.Fatalf("raw metadata not retained as JSON: value=%s err=%v", first.RawMetadata, err)
	}
	var custom []map[string]any
	if err := json.Unmarshal(first.CustomValues, &custom); err != nil || len(custom) != 1 {
		t.Fatalf("custom column values missing: value=%s err=%v", first.CustomValues, err)
	}
	var state map[string][]map[string]any
	if err := json.Unmarshal(first.ReadingState, &state); err != nil || len(state["last_read_positions"]) != 1 || len(state["annotations"]) != 2 {
		t.Fatalf("Calibre reading state not retained: value=%s err=%v", first.ReadingState, err)
	}
	if first.CoverPath != filepath.Join(canonicalRoot, "Author", "Book (1)", "cover.jpg") {
		t.Fatalf("cover lookup failed: %q", first.CoverPath)
	}
	if books[1].Metadata.Title != "書名" || books[1].EPUBPath != "" || !reflect.DeepEqual(books[1].Formats, []string{"PDF"}) {
		t.Fatalf("metadata-only non-EPUB entry was lost: %#v", books[1])
	}
	if books[2].EPUBPath != "" || !strings.Contains(books[2].Issue, "recorded EPUB is unavailable") {
		t.Fatalf("missing EPUB should be reported: %#v", books[2])
	}
	if books[3].EPUBPath != "" || !strings.Contains(books[3].Issue, "escapes the library") {
		t.Fatalf("escaping path should be rejected: %#v", books[3])
	}
}

func TestReadLibraryDoesNotModifyCalibreDatabase(t *testing.T) {
	root := makeLibrary(t)
	path := filepath.Join(root, "metadata.db")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ReadLibrary(root); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("ReadLibrary modified metadata.db")
	}
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path)+"?mode=ro&_query_only=true")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO custom_column_1 VALUES(2,'no')`); err == nil {
		t.Fatal("read-only connection unexpectedly accepted a write")
	}
	var custom string
	if err := db.QueryRow(`SELECT value FROM custom_column_1 WHERE book=1`).Scan(&custom); err != nil || custom != "keep me" {
		t.Fatalf("custom Calibre data changed: value=%q err=%v", custom, err)
	}
}

func TestReadLibraryWithoutOptionalTables(t *testing.T) {
	root := makeLibrary(t)
	db, err := sql.Open("sqlite3", filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"library_id", "custom_columns", "last_read_positions", "annotations"} {
		if _, err := db.Exec(`DROP TABLE "` + table + `"`); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()
	books, err := ReadLibrary(root)
	if err != nil {
		t.Fatalf("optional Calibre tables should not be required: %v", err)
	}
	if books[0].LibraryUUID != "" || len(books[0].CustomValues) != 0 || len(books[0].ReadingState) != 0 {
		t.Fatalf("absent optional data should remain empty: %#v", books[0])
	}
}

func TestReadLibraryRejectsNonCalibreDatabase(t *testing.T) {
	root := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE something_else(id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	_, err = ReadLibrary(root)
	if err == nil || !strings.Contains(err.Error(), `required table "books" is missing`) {
		t.Fatalf("expected actionable schema error, got %v", err)
	}
}

func TestReadLibraryRejectsSymlinkOutsideLibrary(t *testing.T) {
	root := makeLibrary(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "Book"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE books SET path='linked/Book' WHERE id=4`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	books, err := ReadLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(books[3].Issue, "resolves outside the library") {
		t.Fatalf("symlink escape should be rejected: %#v", books[3])
	}
}

func TestReadGenuineCalibreFixture(t *testing.T) {
	root := os.Getenv("ATTUNE_CALIBRE_FIXTURE_ROOT")
	if root == "" {
		t.Skip("set ATTUNE_CALIBRE_FIXTURE_ROOT to a disposable Calibre fixture")
	}
	books, err := ReadLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("got %d fixture records, want 2", len(books))
	}
	if books[0].Metadata.Title != "The Library of Échos" || books[0].EPUBPath == "" {
		t.Fatalf("Calibre database metadata/EPUB lookup failed: %#v", books[0])
	}
	if books[1].EPUBPath != "" || books[1].Metadata.Title != "No EPUB entry" {
		t.Fatalf("metadata-only Calibre entry was not retained: %#v", books[1])
	}
}
