package library

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"attune/internal/epub"
	"attune/internal/model"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/text/language"
)

var ErrNotFound = errors.New("library item not found")

type Store struct {
	root string
	db   *sql.DB
	mu   sync.Mutex
}

func Open(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("library root is empty")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", filepath.Join(abs, "library.sqlite")+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateDevicePreferences(db); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{root: abs, db: db}
	if err = s.backfillBookFiles(); err != nil {
		db.Close()
		return nil, err
	}
	if err = s.backfillSubjects(); err != nil {
		db.Close()
		return nil, err
	}
	if err = s.backfillMetadata(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS books (
		id TEXT PRIMARY KEY, title TEXT NOT NULL, authors TEXT NOT NULL, language TEXT NOT NULL DEFAULT '', subjects TEXT NOT NULL DEFAULT '[]',
		sha256 TEXT NOT NULL UNIQUE, cover_path TEXT NOT NULL DEFAULT '', original_path TEXT NOT NULL,
		added_at TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 1, import_batch_id TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS conversions (
		book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE, cache_key TEXT NOT NULL, path TEXT NOT NULL,
		PRIMARY KEY(book_id, cache_key)
	);
	CREATE TABLE IF NOT EXISTS devices (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, root TEXT NOT NULL, free_bytes INTEGER NOT NULL, total_bytes INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS device_books (
		device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE, path TEXT NOT NULL,
		book_id TEXT NOT NULL DEFAULT '', cache_key TEXT NOT NULL DEFAULT '', PRIMARY KEY(device_id, path)
	);
	CREATE TABLE IF NOT EXISTS reading_progress (
		book_id TEXT PRIMARY KEY REFERENCES books(id) ON DELETE CASCADE,
		chapter_index INTEGER NOT NULL DEFAULT 0,
		position REAL NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS reader_bookmarks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
		chapter_index INTEGER NOT NULL,
		position REAL NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS book_files (
		id TEXT PRIMARY KEY, book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
		format TEXT NOT NULL, filename TEXT NOT NULL, relative_path TEXT NOT NULL,
		sha256 TEXT NOT NULL UNIQUE, size INTEGER NOT NULL, preferred INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS book_files_book_format ON book_files(book_id, format, preferred DESC, created_at);
	CREATE TABLE IF NOT EXISTS reader_locations (
		book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
		asset_id TEXT NOT NULL REFERENCES book_files(id) ON DELETE CASCADE,
		schema_version INTEGER NOT NULL,
		locator_json TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY(book_id, asset_id)
	);
	CREATE TABLE IF NOT EXISTS calibre_sources (
		library_uuid TEXT NOT NULL, book_uuid TEXT NOT NULL,
		book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
		raw_metadata_json TEXT NOT NULL DEFAULT '{}', custom_metadata_json TEXT NOT NULL DEFAULT '{}',
		reading_state_json TEXT NOT NULL DEFAULT '{}', source_modified_at TEXT NOT NULL DEFAULT '',
		imported_at TEXT NOT NULL,
		PRIMARY KEY(library_uuid, book_uuid)
	);
	CREATE INDEX IF NOT EXISTS reader_bookmarks_book_created ON reader_bookmarks(book_id, created_at DESC);`)
	if err != nil {
		return err
	}
	if err = ensureColumn(db, "books", "subjects", `TEXT NOT NULL DEFAULT '[]'`); err != nil {
		return err
	}
	if err = ensureColumn(db, "books", "subjects_refreshed", `INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err = ensureColumn(db, "books", "metadata_json", `TEXT NOT NULL DEFAULT '{}'`); err != nil {
		return err
	}
	if err = ensureColumn(db, "books", "custom_metadata_json", `TEXT NOT NULL DEFAULT '{}'`); err != nil {
		return err
	}
	if err = ensureColumn(db, "books", "source_metadata_json", `TEXT NOT NULL DEFAULT '{}'`); err != nil {
		return err
	}
	if err = ensureColumn(db, "books", "reading_state_json", `TEXT NOT NULL DEFAULT '{}'`); err != nil {
		return err
	}
	if err = ensureColumn(db, "books", "metadata_refreshed", `INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	return ensureColumn(db, "books", "import_batch_id", `TEXT NOT NULL DEFAULT ''`)
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}

// Backfill only subjects: edited titles/authors and all EPUB files remain untouched.
func (s *Store) backfillSubjects() error {
	rows, err := s.db.Query(`SELECT id,original_path FROM books WHERE subjects_refreshed=0`)
	if err != nil {
		return err
	}
	type source struct{ id, rel string }
	var pending []source
	for rows.Next() {
		var x source
		if err = rows.Scan(&x.id, &x.rel); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, x)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, x := range pending {
		if !validBookID(x.id) || filepath.ToSlash(x.rel) != filepath.ToSlash(filepath.Join("books", x.id, "original.epub")) {
			continue
		}
		p := filepath.Join(s.root, filepath.FromSlash(x.rel))
		if validateManagedFile(s.root, x.id, p) != nil {
			continue
		}
		meta, e := epub.Parse(p)
		if e != nil {
			continue
		}
		encoded, _ := json.Marshal(cleanSubjects(meta.Subjects))
		if _, err = s.db.Exec(`UPDATE books SET subjects=?,subjects_refreshed=1 WHERE id=? AND subjects_refreshed=0`, string(encoded), x.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) backfillMetadata() error {
	rows, err := s.db.Query(`SELECT id,original_path FROM books WHERE metadata_refreshed=0`)
	if err != nil {
		return err
	}
	type source struct{ id, rel string }
	var pending []source
	for rows.Next() {
		var x source
		if err = rows.Scan(&x.id, &x.rel); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, x)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, x := range pending {
		if !validBookID(x.id) || filepath.ToSlash(x.rel) != filepath.ToSlash(filepath.Join("books", x.id, "original.epub")) {
			continue
		}
		p := filepath.Join(s.root, filepath.FromSlash(x.rel))
		if validateManagedFile(s.root, x.id, p) != nil {
			continue
		}
		meta, e := epub.Parse(p)
		if e != nil {
			continue
		}
		encoded, _ := json.Marshal(extrasFromEPUB(meta))
		if _, err = s.db.Exec(`UPDATE books SET metadata_json=?,metadata_refreshed=1 WHERE id=? AND metadata_refreshed=0`, string(encoded), x.id); err != nil {
			return err
		}
	}
	return nil
}

// backfillBookFiles indexes the existing managed EPUBs in place. The migration
// never renames or recopies user library files.
func (s *Store) backfillBookFiles() error {
	rows, err := s.db.Query(`SELECT b.id,b.original_path,b.sha256 FROM books b
		WHERE b.original_path<>'' AND NOT EXISTS (SELECT 1 FROM book_files f WHERE f.book_id=b.id)`)
	if err != nil {
		return err
	}
	type legacyFile struct{ bookID, relativePath, hash string }
	var pending []legacyFile
	for rows.Next() {
		var item legacyFile
		if err = rows.Scan(&item.bookID, &item.relativePath, &item.hash); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, item := range pending {
		if !validBookID(item.bookID) || filepath.ToSlash(item.relativePath) != filepath.ToSlash(filepath.Join("books", item.bookID, "original.epub")) {
			continue
		}
		path := filepath.Join(s.root, filepath.FromSlash(item.relativePath))
		if err = validateManagedFile(s.root, item.bookID, path); err != nil {
			continue
		}
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		id, idErr := makeID()
		if idErr != nil {
			return idErr
		}
		_, err = s.db.Exec(`INSERT OR IGNORE INTO book_files(id,book_id,format,filename,relative_path,sha256,size,preferred,created_at)
			VALUES(?,?,?,?,?,?,?,?,?)`, id, item.bookID, "epub", "original.epub", item.relativePath, item.hash, info.Size(), 1, info.ModTime().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
	}
	return nil
}

type bookExtras struct {
	Publisher   string            `json:"publisher,omitempty"`
	Published   string            `json:"published,omitempty"`
	Identifiers map[string]string `json:"identifiers,omitempty"`
	Description string            `json:"description,omitempty"`
	Series      string            `json:"series,omitempty"`
	SeriesIndex string            `json:"seriesIndex,omitempty"`
	AuthorSort  []string          `json:"authorSort,omitempty"`
	TitleSort   string            `json:"titleSort,omitempty"`
	PageCount   int               `json:"pageCount,omitempty"`
}

func extrasFromEPUB(m epub.Metadata) bookExtras {
	return bookExtras{m.Publisher, m.Published, m.Identifiers, m.Description, m.Series, m.SeriesIndex, m.AuthorSort, m.TitleSort, m.PageCount}
}

func (s *Store) Close() error { return s.db.Close() }

// CopyTo creates a standalone library copy in an existing empty directory.
// Imported books are retained; the regenerable conversion cache is omitted.
func (s *Store) CopyTo(destination string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(destination)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("library copy destination is not empty")
	}
	dbPath := filepath.Join(destination, "library.sqlite")
	if _, err = s.db.Exec(`VACUUM INTO ?`, dbPath); err != nil {
		return err
	}
	copyDB, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	if err != nil {
		return err
	}
	_, err = copyDB.Exec(`DELETE FROM conversions`)
	closeErr := copyDB.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	booksRoot := filepath.Join(s.root, "books")
	if _, err = os.Lstat(booksRoot); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return copyManagedTree(booksRoot, filepath.Join(destination, "books"))
}

func copyManagedTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
				return errors.New("invalid managed books directory")
			}
			return os.Mkdir(destination, 0o700)
		}
		target := filepath.Join(destination, rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("managed books contain a symbolic link")
		}
		if entry.IsDir() {
			return os.Mkdir(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return errors.New("managed books contain a non-regular file")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		inErr := in.Close()
		outErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inErr != nil {
			return inErr
		}
		if outErr != nil {
			return outErr
		}
		return os.Chtimes(target, info.ModTime(), info.ModTime())
	})
}

func (s *Store) ListBooks() ([]model.Book, error) {
	books := []model.Book{}
	byBook := make(map[string][]model.BookFile)
	fileRows, err := s.db.Query(`SELECT id,book_id,format,filename,relative_path,sha256,size,preferred FROM book_files ORDER BY book_id,format,preferred DESC,created_at`)
	if err != nil {
		return nil, err
	}
	for fileRows.Next() {
		var f model.BookFile
		var preferred int
		if err := fileRows.Scan(&f.ID, &f.BookID, &f.Format, &f.Filename, &f.RelativePath, &f.SHA256, &f.Size, &preferred); err != nil {
			fileRows.Close()
			return nil, err
		}
		f.Preferred = preferred != 0
		applyFormatCapabilities(&f)
		byBook[f.BookID] = append(byBook[f.BookID], f)
	}
	if err = fileRows.Err(); err != nil {
		fileRows.Close()
		return nil, err
	}
	if err = fileRows.Close(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id,title,authors,language,subjects,metadata_json,custom_metadata_json,sha256,added_at,revision,import_batch_id FROM books ORDER BY added_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		b, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		b.CoverURL = "/covers/" + b.ID
		b.Files = append([]model.BookFile{}, byBook[b.ID]...)
		for _, file := range b.Files {
			if file.Preferred {
				b.PreferredFileID = file.ID
				break
			}
		}
		s.populateFileStats(&b)
		books = append(books, b)
	}
	return books, rows.Err()
}

func (s *Store) ImportEPUB(source string) (model.Book, bool, error) {
	return s.ImportEPUBInBatch(source, "")
}

// NewImportBatchID creates a durable identity for one ImportPaths operation.
func NewImportBatchID() (string, error) { return makeID() }

func (s *Store) ImportEPUBInBatch(source, batchID string) (model.Book, bool, error) {
	return s.importEPUB(source, batchID, nil, nil)
}

// ImportCalibreBook imports a Calibre book while preserving its library
// metadata and optional cover. The EPUB remains an exact copy of the source.
func (s *Store) ImportCalibreBook(source string, metadata model.Book, cover []byte, batchID string) (model.Book, bool, error) {
	return s.importEPUB(source, batchID, &metadata, &importedCover{data: cover})
}

type importedCover struct {
	data []byte
	ext  string
}

func (s *Store) importEPUB(source, batchID string, importedMetadata *model.Book, importedCover *importedCover) (model.Book, bool, error) {
	file, err := os.Open(source)
	if err != nil {
		return model.Book{}, false, err
	}
	h := sha256.New()
	_, err = io.Copy(h, file)
	closeErr := file.Close()
	if err != nil {
		return model.Book{}, false, err
	}
	if closeErr != nil {
		return model.Book{}, false, closeErr
	}
	hash := hex.EncodeToString(h.Sum(nil))
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, err := s.getByHash(hash); err == nil {
		return b, true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return model.Book{}, false, err
	}
	if importedCover != nil && len(importedCover.data) > 0 {
		importedCover.ext, _, err = ValidateCover(importedCover.data)
		if err != nil {
			return model.Book{}, false, err
		}
	}
	id, err := makeID()
	if err != nil {
		return model.Book{}, false, err
	}
	bookDir := filepath.Join(s.root, "books", id)
	booksRoot := filepath.Join(s.root, "books")
	if err = os.MkdirAll(booksRoot, 0o700); err != nil {
		return model.Book{}, false, err
	}
	if err = requireDirectory(booksRoot); err != nil {
		return model.Book{}, false, err
	}
	tmp, err := os.MkdirTemp(booksRoot, ".import-")
	if err != nil {
		return model.Book{}, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(tmp)
		}
	}()
	original := filepath.Join(tmp, "original.epub")
	copiedHash, err := copyFile(source, original)
	if err != nil {
		return model.Book{}, false, err
	}
	if copiedHash != hash {
		return model.Book{}, false, errors.New("source EPUB changed during import")
	}
	meta, err := epub.Parse(original)
	if err != nil {
		return model.Book{}, false, err
	}
	metadata := model.Book{
		Title: meta.Title, Authors: meta.Authors, Language: meta.Language,
		Subjects: cleanSubjects(meta.Subjects),
	}
	extra := extrasFromEPUB(meta)
	coverData, coverExt := meta.Cover, meta.CoverExt
	if importedMetadata != nil {
		metadata = *importedMetadata
		metadata.Title = strings.TrimSpace(metadata.Title)
		if metadata.Title == "" {
			metadata.Title = meta.Title
		}
		metadata.Authors = cleanAuthors(metadata.Authors)
		if len(metadata.Authors) == 0 {
			metadata.Authors = meta.Authors
		}
		metadata.Language = strings.TrimSpace(metadata.Language)
		metadata.Subjects = cleanSubjects(metadata.Subjects)
		extra = bookExtras{
			Publisher: metadata.Publisher, Published: metadata.Published,
			Identifiers: metadata.Identifiers, Description: metadata.Description,
			Series: metadata.Series, SeriesIndex: metadata.SeriesIndex,
			AuthorSort: metadata.AuthorSort, TitleSort: metadata.TitleSort,
			PageCount: metadata.PageCount,
		}
	}
	if importedCover != nil && len(importedCover.data) > 0 {
		coverData, coverExt = importedCover.data, importedCover.ext
	}
	coverRel := ""
	if len(coverData) > 0 {
		coverName := "cover" + coverExt
		if importedCover != nil && len(importedCover.data) > 0 {
			coverName = "cover.override.calibre" + coverExt
		}
		if err = os.WriteFile(filepath.Join(tmp, coverName), coverData, 0o600); err != nil {
			return model.Book{}, false, err
		}
		coverRel = filepath.ToSlash(filepath.Join("books", id, coverName))
	}
	if err = os.Rename(tmp, bookDir); err != nil {
		return model.Book{}, false, err
	}
	// A failed insert removes the newly owned files; the unique hash handles concurrent imports too.
	authors, _ := json.Marshal(metadata.Authors)
	b := model.Book{ID: id, Title: metadata.Title, Authors: metadata.Authors, Language: metadata.Language, SHA256: hash, CoverURL: "/covers/" + id, AddedAt: time.Now().UTC().Format(time.RFC3339Nano), ImportBatchID: batchID, Revision: 1}
	b.Subjects = metadata.Subjects
	applyExtras(&b, extra)
	subjects, _ := json.Marshal(b.Subjects)
	extraJSON, _ := json.Marshal(extra)
	_, err = s.db.Exec(`INSERT INTO books(id,title,authors,language,subjects,subjects_refreshed,metadata_json,metadata_refreshed,sha256,cover_path,original_path,added_at,revision,import_batch_id) VALUES(?,?,?,?,?,1,?,1,?,?,?,?,?,?)`, id, b.Title, string(authors), b.Language, string(subjects), string(extraJSON), hash, coverRel, filepath.ToSlash(filepath.Join("books", id, "original.epub")), b.AddedAt, b.Revision, b.ImportBatchID)
	if err != nil {
		_ = os.RemoveAll(bookDir)
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			if existing, e := s.getByHash(hash); e == nil {
				return existing, true, nil
			}
		}
		return model.Book{}, false, err
	}
	fileID, idErr := makeID()
	if idErr != nil {
		_, _ = s.db.Exec(`DELETE FROM books WHERE id=?`, id)
		_ = os.RemoveAll(bookDir)
		return model.Book{}, false, idErr
	}
	if _, err = s.db.Exec(`INSERT INTO book_files(id,book_id,format,filename,relative_path,sha256,size,preferred,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		fileID, id, "epub", filepath.Base(source), filepath.ToSlash(filepath.Join("books", id, "original.epub")), hash, fileSize(bookDir, "original.epub"), 1, b.AddedAt); err != nil {
		_, _ = s.db.Exec(`DELETE FROM books WHERE id=?`, id)
		_ = os.RemoveAll(bookDir)
		return model.Book{}, false, err
	}
	b.Files = []model.BookFile{{ID: fileID, BookID: id, Format: "epub", Filename: filepath.Base(source), RelativePath: filepath.ToSlash(filepath.Join("books", id, "original.epub")), SHA256: hash, Size: fileSize(bookDir, "original.epub"), Preferred: true, CanRead: true, CanTransfer: true}}
	b.PreferredFileID = fileID
	committed = true
	return b, false, nil
}

func (s *Store) UpdateBook(id, title string, authors []string) (model.Book, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return model.Book{}, errors.New("title is required")
	}
	authors = cleanAuthors(authors)
	encoded, err := json.Marshal(authors)
	if err != nil {
		return model.Book{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return model.Book{}, err
	}
	defer tx.Rollback()
	var oldTitle, oldAuthorsJSON, metadataJSON string
	err = tx.QueryRow(`SELECT title,authors,metadata_json FROM books WHERE id=?`, id).Scan(&oldTitle, &oldAuthorsJSON, &metadataJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Book{}, ErrNotFound
	}
	if err != nil {
		return model.Book{}, err
	}
	var oldAuthors []string
	if err = json.Unmarshal([]byte(oldAuthorsJSON), &oldAuthors); err != nil {
		return model.Book{}, err
	}
	var extras bookExtras
	if err = json.Unmarshal([]byte(metadataJSON), &extras); err != nil {
		return model.Book{}, err
	}
	if title != oldTitle {
		extras.TitleSort = ""
	}
	if !slices.Equal(authors, oldAuthors) {
		extras.AuthorSort = nil
	}
	updatedExtrasJSON, err := json.Marshal(extras)
	if err != nil {
		return model.Book{}, err
	}
	_, err = tx.Exec(`UPDATE books SET title=?,authors=?,metadata_json=?,revision=revision+1 WHERE id=?`, title, string(encoded), string(updatedExtrasJSON), id)
	if err != nil {
		return model.Book{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.Book{}, err
	}
	return s.get(id)
}

func (s *Store) UpdateBookMetadata(id string, edit model.BookMetadataEdit) (model.Book, error) {
	edit, err := normalizeBookMetadataEdit(edit)
	if err != nil {
		return model.Book{}, err
	}
	authorsJSON, err := json.Marshal(edit.Authors)
	if err != nil {
		return model.Book{}, err
	}
	subjectsJSON, err := json.Marshal(edit.Subjects)
	if err != nil {
		return model.Book{}, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return model.Book{}, err
	}
	defer tx.Rollback()
	var oldTitle, oldAuthorsJSON, metadataJSON string
	err = tx.QueryRow(`SELECT title,authors,metadata_json FROM books WHERE id=?`, id).Scan(&oldTitle, &oldAuthorsJSON, &metadataJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Book{}, ErrNotFound
	}
	if err != nil {
		return model.Book{}, err
	}
	var oldAuthors []string
	if err = json.Unmarshal([]byte(oldAuthorsJSON), &oldAuthors); err != nil {
		return model.Book{}, err
	}
	var extras bookExtras
	if err = json.Unmarshal([]byte(metadataJSON), &extras); err != nil {
		return model.Book{}, err
	}
	if edit.Title != oldTitle {
		extras.TitleSort = ""
	}
	if !slices.Equal(edit.Authors, oldAuthors) {
		extras.AuthorSort = nil
	}
	extras.Publisher = edit.Publisher
	extras.Published = edit.Published
	extras.Series = edit.Series
	extras.SeriesIndex = edit.SeriesIndex
	extras.Description = edit.Description
	updatedExtrasJSON, err := json.Marshal(extras)
	if err != nil {
		return model.Book{}, err
	}
	res, err := tx.Exec(`UPDATE books SET title=?,authors=?,language=?,subjects=?,metadata_json=?,revision=revision+1 WHERE id=?`, edit.Title, string(authorsJSON), edit.Language, string(subjectsJSON), string(updatedExtrasJSON), id)
	if err != nil {
		return model.Book{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.Book{}, err
	}
	if n == 0 {
		return model.Book{}, ErrNotFound
	}
	if err = tx.Commit(); err != nil {
		return model.Book{}, err
	}
	return s.get(id)
}

func normalizeBookMetadataEdit(edit model.BookMetadataEdit) (model.BookMetadataEdit, error) {
	edit.Title = strings.TrimSpace(edit.Title)
	if edit.Title == "" || len(edit.Title) > 500 {
		return edit, errors.New("title is required and must be at most 500 bytes")
	}
	edit.Authors = cleanAuthors(edit.Authors)
	if len(edit.Authors) > 50 {
		return edit, errors.New("at most 50 authors are allowed")
	}
	for _, author := range edit.Authors {
		if len(author) > 500 {
			return edit, errors.New("author names must be at most 500 bytes")
		}
	}
	edit.Publisher = strings.TrimSpace(edit.Publisher)
	edit.Language = strings.TrimSpace(edit.Language)
	edit.Published = strings.TrimSpace(edit.Published)
	edit.Series = strings.TrimSpace(edit.Series)
	edit.SeriesIndex = strings.TrimSpace(edit.SeriesIndex)
	edit.Description = strings.TrimSpace(edit.Description)
	for _, field := range []struct {
		value string
		max   int
	}{{edit.Publisher, 500}, {edit.Language, 35}, {edit.Published, 100}, {edit.Series, 500}, {edit.SeriesIndex, 64}, {edit.Description, 65536}} {
		if len(field.value) > field.max {
			return edit, errors.New("metadata field exceeds its size limit")
		}
	}
	if edit.Language != "" {
		tag, err := language.Parse(edit.Language)
		if err != nil {
			return edit, errors.New("language must be a valid BCP-47 language tag")
		}
		edit.Language = tag.String()
	}
	if edit.Published != "" {
		valid := false
		for _, layout := range []string{"2006", "2006-01", "2006-01-02", time.RFC3339Nano, "2006-01-02T15:04:05"} {
			if _, err := time.Parse(layout, edit.Published); err == nil {
				valid = true
				break
			}
		}
		if !valid {
			return edit, errors.New("published date must be a year, year-month, date, or ISO timestamp")
		}
	}
	if edit.Series == "" {
		edit.SeriesIndex = ""
	}
	if edit.SeriesIndex != "" {
		index, err := strconv.ParseFloat(edit.SeriesIndex, 64)
		if err != nil || math.IsNaN(index) || math.IsInf(index, 0) || index < 0 {
			return edit, errors.New("series index must be a non-negative number")
		}
	}
	edit.Subjects = cleanSubjects(edit.Subjects)
	if len(edit.Subjects) > 100 {
		return edit, errors.New("at most 100 subjects are allowed")
	}
	for _, subject := range edit.Subjects {
		if len(subject) > 200 {
			return edit, errors.New("subjects must be at most 200 bytes")
		}
	}
	return edit, nil
}

func (s *Store) DeleteBook(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validBookID(id) {
		return ErrNotFound
	}
	var rel string
	err := s.db.QueryRow(`SELECT original_path FROM books WHERE id=?`, id).Scan(&rel)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if rel != "" && !strings.HasPrefix(filepath.ToSlash(rel), filepath.ToSlash(filepath.Join("books", id))+"/") {
		return errors.New("invalid managed source path")
	}
	booksRoot := filepath.Join(s.root, "books")
	if err = requireDirectory(booksRoot); err != nil {
		return err
	}
	dir := filepath.Join(booksRoot, id)
	if st, e := os.Lstat(dir); e == nil && (!st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
		return errors.New("invalid managed book directory")
	} else if e != nil && !os.IsNotExist(e) {
		return e
	}
	if _, err = s.db.Exec(`DELETE FROM books WHERE id=?`, id); err != nil {
		return err
	}
	// All book assets live in the managed books/<id> directory.
	if e := os.RemoveAll(dir); e != nil {
		return e
	}
	return nil
}

func (s *Store) BookSource(id string) (model.Book, string, error) {
	if !validBookID(id) {
		return model.Book{}, "", ErrNotFound
	}
	var b model.Book
	var rel, authors, subjects, extraJSON, customJSON string
	err := s.db.QueryRow(`SELECT id,title,authors,language,subjects,metadata_json,custom_metadata_json,sha256,added_at,revision,import_batch_id,original_path FROM books WHERE id=?`, id).Scan(&b.ID, &b.Title, &authors, &b.Language, &subjects, &extraJSON, &customJSON, &b.SHA256, &b.AddedAt, &b.Revision, &b.ImportBatchID, &rel)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Book{}, "", ErrNotFound
	}
	if err != nil {
		return model.Book{}, "", err
	}
	if err = json.Unmarshal([]byte(authors), &b.Authors); err != nil {
		return model.Book{}, "", err
	}
	if err = json.Unmarshal([]byte(subjects), &b.Subjects); err != nil {
		return model.Book{}, "", err
	}
	b.CustomMetadata = json.RawMessage(customJSON)
	b.SHA256 = publicHash(b.SHA256)
	var extras bookExtras
	if err = json.Unmarshal([]byte(extraJSON), &extras); err != nil {
		return model.Book{}, "", err
	}
	applyExtras(&b, extras)
	b.CoverURL = "/covers/" + b.ID
	b.Files, err = s.BookFiles(id)
	if err != nil {
		return model.Book{}, "", err
	}
	for _, f := range b.Files {
		if f.Preferred {
			b.PreferredFileID = f.ID
			break
		}
	}
	file, p, err := s.BookFileSourceByFormat(id, "epub")
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return model.Book{}, "", errors.New("this book has no EPUB file")
		}
		return model.Book{}, "", err
	}
	if rel != "" && filepath.ToSlash(rel) != filepath.ToSlash(file.RelativePath) {
		return model.Book{}, "", errors.New("invalid managed source path")
	}
	if !validBookFileRelative(id, file.RelativePath) {
		return model.Book{}, "", errors.New("invalid managed source path")
	}
	s.populateFileStats(&b)
	return b, p, nil
}

func (s *Store) ReadingProgress(id string) (model.ReadingProgress, error) {
	if !validBookID(id) {
		return model.ReadingProgress{}, ErrNotFound
	}
	var progress model.ReadingProgress
	err := s.db.QueryRow(`SELECT book_id,chapter_index,position,updated_at FROM reading_progress WHERE book_id=?`, id).
		Scan(&progress.BookID, &progress.ChapterIndex, &progress.Position, &progress.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ReadingProgress{BookID: id}, nil
	}
	return progress, err
}

func (s *Store) SaveReadingProgress(id string, chapter int, position float64) error {
	if !validBookID(id) || chapter < 0 || math.IsNaN(position) || math.IsInf(position, 0) {
		return errors.New("invalid reading position")
	}
	position = math.Max(0, math.Min(1, position))
	_, err := s.db.Exec(`INSERT INTO reading_progress(book_id,chapter_index,position,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(book_id) DO UPDATE SET chapter_index=excluded.chapter_index,position=excluded.position,updated_at=excluded.updated_at`,
		id, chapter, position, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) ReadingBookmarks(id string) ([]model.ReadingBookmark, error) {
	if !validBookID(id) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(`SELECT id,book_id,chapter_index,position,created_at FROM reader_bookmarks WHERE book_id=? ORDER BY created_at DESC,id DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bookmarks := make([]model.ReadingBookmark, 0)
	for rows.Next() {
		var bookmark model.ReadingBookmark
		if err := rows.Scan(&bookmark.ID, &bookmark.BookID, &bookmark.ChapterIndex, &bookmark.Position, &bookmark.CreatedAt); err != nil {
			return nil, err
		}
		bookmarks = append(bookmarks, bookmark)
	}
	return bookmarks, rows.Err()
}

func (s *Store) AddReadingBookmark(id string, chapter int, position float64) (model.ReadingBookmark, error) {
	if !validBookID(id) || chapter < 0 || math.IsNaN(position) || math.IsInf(position, 0) {
		return model.ReadingBookmark{}, errors.New("invalid reading bookmark")
	}
	position = math.Max(0, math.Min(1, position))
	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.Exec(`INSERT INTO reader_bookmarks(book_id,chapter_index,position,created_at) VALUES(?,?,?,?)`, id, chapter, position, createdAt)
	if err != nil {
		return model.ReadingBookmark{}, err
	}
	bookmarkID, err := result.LastInsertId()
	if err != nil {
		return model.ReadingBookmark{}, err
	}
	return model.ReadingBookmark{ID: bookmarkID, BookID: id, ChapterIndex: chapter, Position: position, CreatedAt: createdAt}, nil
}

func (s *Store) DeleteReadingBookmark(id string, bookmarkID int64) error {
	if !validBookID(id) || bookmarkID < 1 {
		return ErrNotFound
	}
	result, err := s.db.Exec(`DELETE FROM reader_bookmarks WHERE book_id=? AND id=?`, id, bookmarkID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrNotFound
	}
	return err
}
func (s *Store) CoverPath(id string) (string, error) {
	if !validBookID(id) {
		return "", ErrNotFound
	}
	var rel string
	err := s.db.QueryRow(`SELECT cover_path FROM books WHERE id=?`, id).Scan(&rel)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if rel == "" {
		return "", ErrNotFound
	}
	cleanRel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	bookRel := filepath.ToSlash(filepath.Join("books", id))
	if filepath.ToSlash(filepath.Dir(cleanRel)) != bookRel || !strings.HasPrefix(filepath.Base(cleanRel), "cover.") {
		return "", errors.New("invalid managed cover path")
	}
	p := filepath.Join(s.root, filepath.FromSlash(cleanRel))
	if err = validateManagedFile(s.root, id, p); err != nil {
		return "", err
	}
	return p, nil
}

// ReplaceBookCover validates and atomically installs a JPEG or PNG cover. The
// original EPUB is immutable; changing the revision invalidates conversion keys.
func (s *Store) ReplaceBookCover(id string, data []byte) (model.Book, error) {
	ext, _, err := ValidateCover(data)
	if err != nil {
		return model.Book{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validBookID(id) {
		return model.Book{}, ErrNotFound
	}
	var oldRel string
	if err = s.db.QueryRow(`SELECT cover_path FROM books WHERE id=?`, id).Scan(&oldRel); errors.Is(err, sql.ErrNoRows) {
		return model.Book{}, ErrNotFound
	} else if err != nil {
		return model.Book{}, err
	}
	booksRoot := filepath.Join(s.root, "books")
	if err = requireDirectory(booksRoot); err != nil {
		return model.Book{}, err
	}
	dir := filepath.Join(booksRoot, id)
	if err = requireDirectory(dir); err != nil {
		return model.Book{}, err
	}
	nameID, err := makeID()
	if err != nil {
		return model.Book{}, err
	}
	name := "cover.override." + nameID + ext
	tmp, err := os.CreateTemp(dir, ".cover-upload-")
	if err != nil {
		return model.Book{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0o600); err == nil {
		var n int
		n, err = tmp.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return model.Book{}, err
	}
	if err = os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return model.Book{}, err
	}
	newRel := filepath.ToSlash(filepath.Join("books", id, name))
	tx, err := s.db.Begin()
	if err != nil {
		_ = os.Remove(filepath.Join(dir, name))
		return model.Book{}, err
	}
	res, err := tx.Exec(`UPDATE books SET cover_path=?,revision=revision+1 WHERE id=?`, newRel, id)
	if err == nil {
		var n int64
		n, err = res.RowsAffected()
		if err == nil && n == 0 {
			err = ErrNotFound
		}
	}
	if err == nil {
		err = tx.Commit()
	} else {
		_ = tx.Rollback()
	}
	if err != nil {
		_ = os.Remove(filepath.Join(dir, name))
		return model.Book{}, err
	}
	if oldRel != "" {
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(oldRel)))
		if filepath.ToSlash(filepath.Dir(clean)) == filepath.ToSlash(filepath.Join("books", id)) && strings.HasPrefix(filepath.Base(clean), "cover.") {
			_ = os.Remove(filepath.Join(s.root, filepath.FromSlash(clean)))
		}
	}
	return s.get(id)
}

// ValidateCover checks content, decodability, and bounded decoded dimensions.
func ValidateCover(data []byte) (ext, mime string, err error) {
	if len(data) == 0 || len(data) > 32<<20 {
		return "", "", errors.New("cover image must be between 1 byte and 32 MiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", "", fmt.Errorf("invalid cover image: %w", err)
	}
	if format == "jpeg" {
		ext, mime = ".jpg", "image/jpeg"
	} else if format == "png" {
		ext, mime = ".png", "image/png"
	} else {
		return "", "", errors.New("cover must be a JPEG or PNG image")
	}
	if detected := http.DetectContentType(data[:min(len(data), 512)]); detected != mime {
		return "", "", errors.New("cover content does not match JPEG or PNG data")
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		return "", "", errors.New("cover dimensions must be at most 40 megapixels")
	}
	if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		return "", "", fmt.Errorf("invalid cover image: %w", err)
	}
	return ext, mime, nil
}

// ExportEPUB makes a temporary conversion source with the managed cover applied.
func (s *Store) ExportEPUB(id, destination string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, source, err := s.BookSource(id)
	if err != nil {
		return err
	}
	cover, coverErr := s.CoverPath(id)
	if errors.Is(coverErr, ErrNotFound) {
		return copyFileContents(source, destination)
	}
	if coverErr != nil {
		return coverErr
	}
	if !strings.HasPrefix(filepath.Base(cover), "cover.override.") {
		return copyFileContents(source, destination)
	}
	data, err := os.ReadFile(cover)
	if err == nil && len(data) > 32<<20 {
		err = errors.New("managed cover exceeds size limit")
	}
	if err != nil {
		return err
	}
	return epub.ReplaceCover(source, destination, data, filepath.Ext(cover))
}

func copyFileContents(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (s *Store) GetConversion(key string) (string, error) {
	var p string
	err := s.db.QueryRow(`SELECT path FROM conversions WHERE cache_key=? ORDER BY book_id LIMIT 1`, key).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return p, err
}
func (s *Store) SaveConversion(bookID, key, p string) error {
	if _, err := s.db.Exec(`INSERT INTO conversions(book_id,cache_key,path) VALUES(?,?,?) ON CONFLICT(book_id,cache_key) DO UPDATE SET path=excluded.path`, bookID, key, p); err != nil {
		return err
	}
	return nil
}
func (s *Store) RecordDevice(d model.Device) error {
	_, err := s.db.Exec(`INSERT INTO devices(id,name,root,free_bytes,total_bytes) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,root=excluded.root,free_bytes=excluded.free_bytes,total_bytes=excluded.total_bytes`, d.ID, d.Name, d.Root, d.FreeBytes, d.TotalBytes)
	return err
}
func (s *Store) RecordTransfer(deviceID, bookID, p, key string) error {
	_, err := s.db.Exec(`INSERT INTO device_books(device_id,path,book_id,cache_key) VALUES(?,?,?,?) ON CONFLICT(device_id,path) DO UPDATE SET book_id=excluded.book_id,cache_key=excluded.cache_key`, deviceID, p, bookID, key)
	return err
}
func (s *Store) TransferKey(deviceID, p string) (string, error) {
	var k string
	err := s.db.QueryRow(`SELECT cache_key FROM device_books WHERE device_id=? AND path=?`, deviceID, p).Scan(&k)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return k, err
}
func (s *Store) ForgetTransfer(deviceID, p string) error {
	_, err := s.db.Exec(`DELETE FROM device_books WHERE device_id=? AND path=?`, deviceID, p)
	return err
}

type rowScanner interface{ Scan(...any) error }

func scanBook(r rowScanner) (model.Book, error) {
	var b model.Book
	var authors, subjects, extraJSON, customJSON string
	err := r.Scan(&b.ID, &b.Title, &authors, &b.Language, &subjects, &extraJSON, &customJSON, &b.SHA256, &b.AddedAt, &b.Revision, &b.ImportBatchID)
	if err != nil {
		return b, err
	}
	b.SHA256 = publicHash(b.SHA256)
	if err = json.Unmarshal([]byte(authors), &b.Authors); err != nil {
		return b, err
	}
	if err = json.Unmarshal([]byte(subjects), &b.Subjects); err != nil {
		return b, err
	}
	b.CustomMetadata = json.RawMessage(customJSON)
	var extras bookExtras
	if err = json.Unmarshal([]byte(extraJSON), &extras); err != nil {
		return b, err
	}
	applyExtras(&b, extras)
	return b, nil
}

func publicHash(hash string) string {
	if strings.HasPrefix(hash, "metadata:") {
		return ""
	}
	return hash
}

func applyExtras(b *model.Book, x bookExtras) {
	b.Publisher = x.Publisher
	b.Published = x.Published
	b.Identifiers = x.Identifiers
	b.Description = x.Description
	b.Series = x.Series
	b.SeriesIndex = x.SeriesIndex
	b.AuthorSort = x.AuthorSort
	b.TitleSort = x.TitleSort
	b.PageCount = x.PageCount
}

func (s *Store) populateFileStats(b *model.Book) {
	if !validBookID(b.ID) {
		return
	}
	var rel string
	if b.Files != nil {
		for _, file := range b.Files {
			if file.Preferred {
				rel = file.RelativePath
				break
			}
		}
		if rel == "" && len(b.Files) > 0 {
			rel = b.Files[0].RelativePath
		}
	} else {
		_ = s.db.QueryRow(`SELECT relative_path FROM book_files WHERE book_id=? ORDER BY preferred DESC,CASE format WHEN 'epub' THEN 0 ELSE 1 END,created_at LIMIT 1`, b.ID).Scan(&rel)
	}
	if rel == "" {
		rel = filepath.ToSlash(filepath.Join("books", b.ID, "original.epub"))
	}
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if validateManagedFile(s.root, b.ID, p) != nil {
		return
	}
	st, err := os.Stat(p)
	if err == nil {
		b.FileSize = st.Size()
		b.ModifiedAt = st.ModTime().UTC().Format(time.RFC3339Nano)
	}
}
func (s *Store) get(id string) (model.Book, error) {
	row := s.db.QueryRow(`SELECT id,title,authors,language,subjects,metadata_json,custom_metadata_json,sha256,added_at,revision,import_batch_id FROM books WHERE id=?`, id)
	b, err := scanBook(row)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	b.CoverURL = "/covers/" + b.ID
	s.populateFileStats(&b)
	return b, err
}
func (s *Store) getByHash(hash string) (model.Book, error) {
	row := s.db.QueryRow(`SELECT id,title,authors,language,subjects,metadata_json,custom_metadata_json,sha256,added_at,revision,import_batch_id FROM books WHERE sha256=?`, hash)
	b, err := scanBook(row)
	if errors.Is(err, sql.ErrNoRows) {
		return b, sql.ErrNoRows
	}
	b.CoverURL = "/covers/" + b.ID
	s.populateFileStats(&b)
	return b, err
}
func makeID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return hex.EncodeToString(b[:]), nil
}
func copyFile(src, dst string) (string, error) {
	in, e := os.Open(src)
	if e != nil {
		return "", e
	}
	defer in.Close()
	out, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if e != nil {
		return "", e
	}
	h := sha256.New()
	_, e = io.Copy(io.MultiWriter(out, h), in)
	ce := out.Close()
	if e != nil {
		_ = os.Remove(dst)
		return "", e
	}
	if ce != nil {
		_ = os.Remove(dst)
		return "", ce
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func cleanAuthors(in []string) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}
func cleanSubjects(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[strings.ToLower(s)] {
			out = append(out, s)
			seen[strings.ToLower(s)] = true
		}
	}
	return out
}
func within(root, p string) bool {
	rel, e := filepath.Rel(root, p)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validBookID(id string) bool {
	if len(id) != 32 || id != strings.ToLower(id) {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func requireDirectory(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("managed books path is not a real directory")
	}
	return nil
}

func validateManagedFile(root, id, path string) error {
	if !within(root, path) {
		return errors.New("invalid managed file path")
	}
	booksRoot := filepath.Join(root, "books")
	if err := requireDirectory(booksRoot); err != nil {
		return err
	}
	if err := requireDirectory(filepath.Join(booksRoot, id)); err != nil {
		return err
	}
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("managed file is not a regular file")
	}
	return nil
}
