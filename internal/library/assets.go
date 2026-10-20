package library

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"attune/internal/formats"
	"attune/internal/model"
)

// AttachBookFile adds an exact copy of a source file to an existing book.
// Content hashes deduplicate across the whole library; format variants are
// otherwise retained side by side. A preferred file is selected per format.
func (s *Store) AttachBookFile(bookID, source string, preferred bool) (model.BookFile, bool, error) {
	if !validBookID(bookID) {
		return model.BookFile{}, false, ErrNotFound
	}
	format := formats.Normalize(filepath.Ext(source))
	if format == "" {
		return model.BookFile{}, false, errors.New("file has no supported format extension")
	}
	info, err := os.Stat(source)
	if err != nil {
		return model.BookFile{}, false, err
	}
	if !info.Mode().IsRegular() {
		return model.BookFile{}, false, errors.New("book asset must be a regular file")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err = s.get(bookID); err != nil {
		return model.BookFile{}, false, err
	}
	booksRoot := filepath.Join(s.root, "books")
	if err = os.MkdirAll(booksRoot, 0o700); err != nil {
		return model.BookFile{}, false, err
	}
	if err = requireDirectory(booksRoot); err != nil {
		return model.BookFile{}, false, err
	}
	bookDir := filepath.Join(booksRoot, bookID)
	if err = os.MkdirAll(bookDir, 0o700); err != nil {
		return model.BookFile{}, false, err
	}
	if err = requireDirectory(bookDir); err != nil {
		return model.BookFile{}, false, err
	}
	tmp, err := os.CreateTemp(bookDir, ".asset-import-")
	if err != nil {
		return model.BookFile{}, false, err
	}
	tmpPath := tmp.Name()
	if err = tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return model.BookFile{}, false, err
	}
	if err = os.Remove(tmpPath); err != nil {
		return model.BookFile{}, false, err
	}
	defer os.Remove(tmpPath)
	hash, err := copyFile(source, tmpPath)
	if err != nil {
		return model.BookFile{}, false, err
	}
	copyInfo, err := os.Stat(tmpPath)
	if err != nil {
		return model.BookFile{}, false, err
	}
	if existing, err := s.fileByHash(hash); err == nil {
		return existing, true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return model.BookFile{}, false, err
	}
	if !preferred {
		var count int
		if err = s.db.QueryRow(`SELECT COUNT(*) FROM book_files WHERE book_id=? AND format=?`, bookID, format).Scan(&count); err != nil {
			return model.BookFile{}, false, err
		}
		preferred = count == 0
	}
	id, err := makeID()
	if err != nil {
		return model.BookFile{}, false, err
	}
	assetName := "asset-" + id + "." + format
	target := filepath.Join(bookDir, assetName)
	if err = os.Rename(tmpPath, target); err != nil {
		return model.BookFile{}, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(target)
		}
	}()
	rel := filepath.ToSlash(filepath.Join("books", bookID, assetName))
	created := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.Begin()
	if err != nil {
		return model.BookFile{}, false, err
	}
	defer tx.Rollback()
	if preferred {
		if _, err = tx.Exec(`UPDATE book_files SET preferred=0 WHERE book_id=? AND format=?`, bookID, format); err != nil {
			return model.BookFile{}, false, err
		}
	}
	if _, err = tx.Exec(`INSERT INTO book_files(id,book_id,format,filename,relative_path,sha256,size,preferred,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		id, bookID, format, filepath.Base(source), rel, hash, copyInfo.Size(), boolInt(preferred), created); err != nil {
		return model.BookFile{}, false, err
	}
	// Keep legacy EPUB consumers working while new readers use BookFileSource.
	if format == "epub" {
		if _, err = tx.Exec(`UPDATE books SET sha256=?,original_path=? WHERE id=?`, hash, rel, bookID); err != nil {
			return model.BookFile{}, false, err
		}
	} else {
		if _, err = tx.Exec(`UPDATE books SET sha256=? WHERE id=? AND sha256 LIKE 'metadata:%'`, hash, bookID); err != nil {
			return model.BookFile{}, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return model.BookFile{}, false, err
	}
	committed = true
	f := model.BookFile{ID: id, BookID: bookID, Format: format, Filename: filepath.Base(source), RelativePath: rel, SHA256: hash, Size: copyInfo.Size(), Preferred: preferred}
	applyFormatCapabilities(&f)
	return f, false, nil
}

// ImportFileInBatch imports a directly selected ebook. EPUB keeps the richer
// metadata parser; other formats become a simple library record and retain an
// exact copy for transfer or external opening.
func (s *Store) ImportFileInBatch(source, batchID string) (model.Book, bool, error) {
	format := formats.Normalize(filepath.Ext(source))
	if format == "" || (!formats.CanRead(format) && !formats.CanTransfer(format)) {
		return model.Book{}, false, errors.New("file format is not supported for direct import")
	}
	if format == "epub" {
		return s.ImportEPUBInBatch(source, batchID)
	}
	title := strings.TrimSpace(strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)))
	if title == "" {
		title = "Untitled book"
	}
	book, err := s.CreateCalibreBook(model.Book{Title: title, Authors: []string{"Unknown"}}, nil, nil, nil, nil, batchID)
	if err != nil {
		return model.Book{}, false, err
	}
	file, duplicate, err := s.AttachBookFile(book.ID, source, true)
	if err != nil {
		_ = s.DeleteBook(book.ID)
		return model.Book{}, false, err
	}
	if duplicate {
		_ = s.DeleteBook(book.ID)
		book, err = s.get(file.BookID)
		return book, true, err
	}
	book.Files = []model.BookFile{file}
	book.PreferredFileID = file.ID
	book.SHA256 = file.SHA256
	book.FileSize = file.Size
	return book, false, nil
}

// CreateCalibreBook preserves a Calibre record even if it has no local files.
// Normalized metadata stays editable while source payloads remain available
// for future readers and reimports.
func (s *Store) CreateCalibreBook(metadata model.Book, customMetadata, rawMetadata, readingState json.RawMessage, cover []byte, batchID string) (model.Book, error) {
	metadata.Title = strings.TrimSpace(metadata.Title)
	if metadata.Title == "" {
		metadata.Title = "Untitled book"
	}
	metadata.Authors = cleanAuthors(metadata.Authors)
	metadata.Subjects = cleanSubjects(metadata.Subjects)
	if len(cover) > 0 {
		if _, _, err := ValidateCover(cover); err != nil {
			return model.Book{}, err
		}
	}
	customJSON := normalizedJSON(customMetadata)
	rawJSON := normalizedJSON(rawMetadata)
	stateJSON := normalizedJSON(readingState)
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := makeID()
	if err != nil {
		return model.Book{}, err
	}
	bookDir := filepath.Join(s.root, "books", id)
	if err = os.MkdirAll(bookDir, 0o700); err != nil {
		return model.Book{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(bookDir)
		}
	}()
	coverRel := ""
	if len(cover) > 0 {
		ext, _, validationErr := ValidateCover(cover)
		if validationErr != nil {
			return model.Book{}, validationErr
		}
		coverName := "cover.calibre" + ext
		if err = os.WriteFile(filepath.Join(bookDir, coverName), cover, 0o600); err != nil {
			return model.Book{}, err
		}
		coverRel = filepath.ToSlash(filepath.Join("books", id, coverName))
	}
	addedAt := time.Now().UTC().Format(time.RFC3339Nano)
	authors, _ := json.Marshal(metadata.Authors)
	subjects, _ := json.Marshal(metadata.Subjects)
	extra := bookExtras{Publisher: metadata.Publisher, Published: metadata.Published, Identifiers: metadata.Identifiers, Description: metadata.Description, Series: metadata.Series, SeriesIndex: metadata.SeriesIndex, AuthorSort: metadata.AuthorSort, TitleSort: metadata.TitleSort, PageCount: metadata.PageCount}
	extraJSON, _ := json.Marshal(extra)
	_, err = s.db.Exec(`INSERT INTO books(id,title,authors,language,subjects,subjects_refreshed,metadata_json,metadata_refreshed,custom_metadata_json,source_metadata_json,reading_state_json,sha256,cover_path,original_path,added_at,revision,import_batch_id)
		VALUES(?,?,?,?,?,1,?,1,?,?,?, ?,?,'',?,?,?)`, id, metadata.Title, string(authors), metadata.Language, string(subjects), string(extraJSON), string(customJSON), string(rawJSON), string(stateJSON), "metadata:"+id, coverRel, addedAt, 1, batchID)
	if err != nil {
		return model.Book{}, err
	}
	committed = true
	b := metadata
	b.ID, b.SHA256, b.AddedAt, b.ImportBatchID, b.Revision = id, "", addedAt, batchID, 1
	b.CoverURL = "/covers/" + id
	b.CustomMetadata = append(json.RawMessage(nil), customJSON...)
	return b, nil
}

// LinkCalibreSource stores the stable source identity and raw imported payload.
// It intentionally does not replace Attune's editable normalized metadata.
func (s *Store) LinkCalibreSource(libraryUUID, bookUUID, bookID string, rawMetadata, customMetadata, readingState json.RawMessage, sourceModifiedAt string) error {
	if strings.TrimSpace(libraryUUID) == "" || strings.TrimSpace(bookUUID) == "" || !validBookID(bookID) {
		return errors.New("Calibre source identity and Attune book are required")
	}
	_, err := s.db.Exec(`INSERT INTO calibre_sources(library_uuid,book_uuid,book_id,raw_metadata_json,custom_metadata_json,reading_state_json,source_modified_at,imported_at)
		VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(library_uuid,book_uuid) DO UPDATE SET book_id=excluded.book_id,raw_metadata_json=excluded.raw_metadata_json,custom_metadata_json=excluded.custom_metadata_json,reading_state_json=excluded.reading_state_json,source_modified_at=excluded.source_modified_at,imported_at=excluded.imported_at`,
		libraryUUID, bookUUID, bookID, string(normalizedJSON(rawMetadata)), string(normalizedJSON(customMetadata)), string(normalizedJSON(readingState)), sourceModifiedAt, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) CalibreBookID(libraryUUID, bookUUID string) (string, bool, error) {
	var id string
	err := s.db.QueryRow(`SELECT book_id FROM calibre_sources WHERE library_uuid=? AND book_uuid=?`, libraryUUID, bookUUID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

func (s *Store) BookFileSource(bookID, fileID string) (model.BookFile, string, error) {
	if !validBookID(bookID) || fileID == "" {
		return model.BookFile{}, "", ErrNotFound
	}
	var f model.BookFile
	var preferred int
	err := s.db.QueryRow(`SELECT id,book_id,format,filename,relative_path,sha256,size,preferred FROM book_files WHERE book_id=? AND id=?`, bookID, fileID).
		Scan(&f.ID, &f.BookID, &f.Format, &f.Filename, &f.RelativePath, &f.SHA256, &f.Size, &preferred)
	if errors.Is(err, sql.ErrNoRows) {
		return model.BookFile{}, "", ErrNotFound
	}
	if err != nil {
		return model.BookFile{}, "", err
	}
	f.Preferred = preferred != 0
	applyFormatCapabilities(&f)
	if !validBookFileRelative(bookID, f.RelativePath) {
		return model.BookFile{}, "", errors.New("invalid managed book asset path")
	}
	path := filepath.Join(s.root, filepath.FromSlash(f.RelativePath))
	if err = validateManagedFile(s.root, bookID, path); err != nil {
		return model.BookFile{}, "", err
	}
	return f, path, nil
}

func validBookFileRelative(bookID, relative string) bool {
	if !validBookID(bookID) || relative == "" || filepath.IsAbs(filepath.FromSlash(relative)) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	prefix := filepath.ToSlash(filepath.Join("books", bookID)) + "/"
	return clean == relative && strings.HasPrefix(clean, prefix) && len(clean) > len(prefix)
}

func (s *Store) BookFiles(bookID string) ([]model.BookFile, error) {
	if !validBookID(bookID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(`SELECT id,book_id,format,filename,relative_path,sha256,size,preferred FROM book_files WHERE book_id=? ORDER BY format,preferred DESC,created_at`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := make([]model.BookFile, 0)
	for rows.Next() {
		var f model.BookFile
		var preferred int
		if err := rows.Scan(&f.ID, &f.BookID, &f.Format, &f.Filename, &f.RelativePath, &f.SHA256, &f.Size, &preferred); err != nil {
			return nil, err
		}
		f.Preferred = preferred != 0
		applyFormatCapabilities(&f)
		files = append(files, f)
	}
	return files, rows.Err()
}

func (s *Store) BookFileSourceByFormat(bookID, format string) (model.BookFile, string, error) {
	format = formats.Normalize(format)
	if format == "" {
		return model.BookFile{}, "", ErrNotFound
	}
	var id string
	err := s.db.QueryRow(`SELECT id FROM book_files WHERE book_id=? AND format=? ORDER BY preferred DESC,created_at DESC LIMIT 1`, bookID, format).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return model.BookFile{}, "", ErrNotFound
	}
	if err != nil {
		return model.BookFile{}, "", err
	}
	return s.BookFileSource(bookID, id)
}

func (s *Store) FileByHash(hash string) (model.BookFile, bool, error) {
	f, err := s.fileByHash(hash)
	if errors.Is(err, sql.ErrNoRows) {
		return model.BookFile{}, false, nil
	}
	return f, err == nil, err
}

func (s *Store) fileByHash(hash string) (model.BookFile, error) {
	var f model.BookFile
	var preferred int
	err := s.db.QueryRow(`SELECT id,book_id,format,filename,relative_path,sha256,size,preferred FROM book_files WHERE sha256=?`, hash).
		Scan(&f.ID, &f.BookID, &f.Format, &f.Filename, &f.RelativePath, &f.SHA256, &f.Size, &preferred)
	if err != nil {
		return f, err
	}
	f.Preferred = preferred != 0
	applyFormatCapabilities(&f)
	return f, nil
}

func applyFormatCapabilities(f *model.BookFile) {
	f.Format = formats.Normalize(f.Format)
	f.CanRead = formats.CanRead(f.Format)
	f.CanTransfer = formats.CanTransfer(f.Format)
	f.CanConvert = formats.CanConvert(f.Format, "mobi")
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func fileSize(dir, name string) int64 {
	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		return 0
	}
	return info.Size()
}

func normalizedJSON(data json.RawMessage) json.RawMessage {
	if len(data) == 0 || !json.Valid(data) {
		return json.RawMessage(`{}`)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(compact.String())
}
