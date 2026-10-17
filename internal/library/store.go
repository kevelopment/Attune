package library

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
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
	s := &Store{root: abs, db: db}
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
		added_at TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 1
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
	);`)
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
	return ensureColumn(db, "books", "metadata_refreshed", `INTEGER NOT NULL DEFAULT 0`)
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

func (s *Store) ListBooks() ([]model.Book, error) {
	rows, err := s.db.Query(`SELECT id,title,authors,language,subjects,metadata_json,sha256,added_at,revision FROM books ORDER BY added_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	books := []model.Book{}
	for rows.Next() {
		b, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		b.CoverURL = "/covers/" + b.ID
		s.populateFileStats(&b)
		books = append(books, b)
	}
	return books, rows.Err()
}

func (s *Store) ImportEPUB(source string) (model.Book, bool, error) {
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
	coverRel := ""
	if len(meta.Cover) > 0 {
		coverName := "cover" + meta.CoverExt
		if err = os.WriteFile(filepath.Join(tmp, coverName), meta.Cover, 0o600); err != nil {
			return model.Book{}, false, err
		}
		coverRel = filepath.ToSlash(filepath.Join("books", id, coverName))
	}
	if err = os.Rename(tmp, bookDir); err != nil {
		return model.Book{}, false, err
	}
	// A failed insert removes the newly owned files; the unique hash handles concurrent imports too.
	authors, _ := json.Marshal(meta.Authors)
	b := model.Book{ID: id, Title: meta.Title, Authors: meta.Authors, Language: meta.Language, SHA256: hash, CoverURL: "/covers/" + id, AddedAt: time.Now().UTC().Format(time.RFC3339Nano), Revision: 1}
	b.Subjects = cleanSubjects(meta.Subjects)
	applyExtras(&b, extrasFromEPUB(meta))
	subjects, _ := json.Marshal(b.Subjects)
	extraJSON, _ := json.Marshal(extrasFromEPUB(meta))
	_, err = s.db.Exec(`INSERT INTO books(id,title,authors,language,subjects,subjects_refreshed,metadata_json,metadata_refreshed,sha256,cover_path,original_path,added_at,revision) VALUES(?,?,?,?,?,1,?,1,?,?,?,?,?)`, id, b.Title, string(authors), b.Language, string(subjects), string(extraJSON), hash, coverRel, filepath.ToSlash(filepath.Join("books", id, "original.epub")), b.AddedAt, b.Revision)
	if err != nil {
		_ = os.RemoveAll(bookDir)
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			if existing, e := s.getByHash(hash); e == nil {
				return existing, true, nil
			}
		}
		return model.Book{}, false, err
	}
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
	expected := filepath.ToSlash(filepath.Join("books", id, "original.epub"))
	if filepath.ToSlash(rel) != expected {
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
	var rel, authors, subjects, extraJSON string
	err := s.db.QueryRow(`SELECT id,title,authors,language,subjects,metadata_json,sha256,added_at,revision,original_path FROM books WHERE id=?`, id).Scan(&b.ID, &b.Title, &authors, &b.Language, &subjects, &extraJSON, &b.SHA256, &b.AddedAt, &b.Revision, &rel)
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
	var extras bookExtras
	if err = json.Unmarshal([]byte(extraJSON), &extras); err != nil {
		return model.Book{}, "", err
	}
	applyExtras(&b, extras)
	b.CoverURL = "/covers/" + b.ID
	expected := filepath.ToSlash(filepath.Join("books", id, "original.epub"))
	if filepath.ToSlash(rel) != expected {
		return model.Book{}, "", errors.New("invalid managed source path")
	}
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	if err = validateManagedFile(s.root, id, p); err != nil {
		return model.Book{}, "", err
	}
	s.populateFileStats(&b)
	return b, p, nil
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
	var authors, subjects, extraJSON string
	err := r.Scan(&b.ID, &b.Title, &authors, &b.Language, &subjects, &extraJSON, &b.SHA256, &b.AddedAt, &b.Revision)
	if err != nil {
		return b, err
	}
	if err = json.Unmarshal([]byte(authors), &b.Authors); err != nil {
		return b, err
	}
	if err = json.Unmarshal([]byte(subjects), &b.Subjects); err != nil {
		return b, err
	}
	var extras bookExtras
	if err = json.Unmarshal([]byte(extraJSON), &extras); err != nil {
		return b, err
	}
	applyExtras(&b, extras)
	return b, nil
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
	p := filepath.Join(s.root, "books", b.ID, "original.epub")
	if validateManagedFile(s.root, b.ID, p) != nil {
		return
	}
	st, err := os.Stat(p)
	if err != nil {
		return
	}
	b.FileSize = st.Size()
	b.ModifiedAt = st.ModTime().UTC().Format(time.RFC3339Nano)
}
func (s *Store) get(id string) (model.Book, error) {
	row := s.db.QueryRow(`SELECT id,title,authors,language,subjects,metadata_json,sha256,added_at,revision FROM books WHERE id=?`, id)
	b, err := scanBook(row)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	b.CoverURL = "/covers/" + b.ID
	s.populateFileStats(&b)
	return b, err
}
func (s *Store) getByHash(hash string) (model.Book, error) {
	row := s.db.QueryRow(`SELECT id,title,authors,language,subjects,metadata_json,sha256,added_at,revision FROM books WHERE sha256=?`, hash)
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
