// Package calibre reads Calibre libraries without modifying them.
package calibre

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"attune/internal/model"
	_ "github.com/mattn/go-sqlite3"
)

// Book is one Calibre database record and the in-library assets Attune can
// import. EPUBPath is empty when the record has no EPUB format or its file is
// missing. Issue explains a bad/missing path without hiding the record.
type Book struct {
	ID           string
	LibraryUUID  string
	UUID         string
	Metadata     model.Book
	RawMetadata  json.RawMessage
	CustomValues json.RawMessage
	ReadingState json.RawMessage
	Assets       []Asset
	EPUBPath     string
	CoverPath    string
	Formats      []string
	Issue        string
}

// Asset is a format file recorded by Calibre. Path is populated only when the
// source file exists and passes the library-containment checks; Issue preserves
// a per-file failure without hiding other formats on the same book.
type Asset struct {
	Format string
	Path   string
	Name   string
	Size   int64
	Issue  string
}

// ReadLibrary reads metadata.db in SQLite read-only mode. It performs no
// migrations, writes, or Calibre-specific function/trigger registration.
func ReadLibrary(root string) ([]Book, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("Calibre library path is empty")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve Calibre library path: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("open Calibre library %q: %w", absRoot, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("Calibre library %q is not a directory", absRoot)
	}
	dbPath := filepath.Join(absRoot, "metadata.db")
	dbPath, err = safeExistingFile(absRoot, dbPath)
	if err != nil {
		return nil, fmt.Errorf("open Calibre metadata database %q: %w", filepath.Join(absRoot, "metadata.db"), err)
	}

	u := &url.URL{Scheme: "file", Path: sqliteFilePath(dbPath)}
	query := url.Values{}
	query.Set("mode", "ro")
	query.Set("_query_only", "true")
	query.Set("_busy_timeout", "5000")
	u.RawQuery = query.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, fmt.Errorf("open Calibre metadata database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := validateSchema(db); err != nil {
		return nil, fmt.Errorf("unsupported Calibre metadata database: %w", err)
	}
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("start Calibre read snapshot: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT id, title, timestamp, pubdate, author_sort, sort, series_index, path, has_cover, last_modified
		FROM books ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read Calibre books: %w", err)
	}
	defer rows.Close()
	type baseRow struct {
		id                                      int64
		title, authorSort, titleSort, bookPath  sql.NullString
		added, published, seriesIndex, modified sql.NullString
		hasCover                                sql.NullInt64
	}
	bases := make([]baseRow, 0)
	for rows.Next() {
		var row baseRow
		if err = rows.Scan(&row.id, &row.title, &row.added, &row.published, &row.authorSort, &row.titleSort, &row.seriesIndex, &row.bookPath, &row.hasCover, &row.modified); err != nil {
			return nil, fmt.Errorf("read Calibre book row: %w", err)
		}
		bases = append(bases, row)
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil {
		return nil, fmt.Errorf("read Calibre books: %w", rowsErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close Calibre books query: %w", closeErr)
	}
	books := make([]Book, 0, len(bases))
	libraryUUID := readLibraryUUID(tx)
	var libraryIssues []string
	rawBooks, err := readRawBookRows(tx)
	if err != nil {
		rawBooks = map[int64]map[string]any{}
		libraryIssues = append(libraryIssues, fmt.Sprintf("raw metadata unavailable: %v", err))
	}
	customValues, err := readCustomValues(tx)
	if err != nil {
		customValues = map[int64][]map[string]any{}
		libraryIssues = append(libraryIssues, fmt.Sprintf("custom columns unavailable: %v", err))
	}
	readingState, err := readReadingState(tx)
	if err != nil {
		readingState = map[int64]map[string][]map[string]any{}
		libraryIssues = append(libraryIssues, fmt.Sprintf("reading state unavailable: %v", err))
	}
	for _, row := range bases {
		if row.id < 1 {
			continue
		}
		b := Book{ID: strconv.FormatInt(row.id, 10), LibraryUUID: libraryUUID}
		for _, issue := range libraryIssues {
			appendBookIssue(&b, issue)
		}
		if raw, ok := rawBooks[row.id]; ok {
			b.UUID, _ = raw["uuid"].(string)
			b.RawMetadata, _ = json.Marshal(raw)
		}
		if values, ok := customValues[row.id]; ok {
			b.CustomValues, _ = json.Marshal(values)
		}
		if state, ok := readingState[row.id]; ok {
			b.ReadingState, _ = json.Marshal(state)
		}
		b.Metadata = model.Book{
			ID: b.ID, Title: row.title.String, AddedAt: calibreDate(row.added.String),
			Published: calibreDate(row.published.String), TitleSort: row.titleSort.String,
			ModifiedAt: calibreDate(row.modified.String), Revision: 1, CoverURL: "/covers/" + b.ID,
		}
		if row.authorSort.Valid && row.authorSort.String != "" {
			b.Metadata.AuthorSort = splitAuthorSort(row.authorSort.String)
		}
		if row.seriesIndex.Valid {
			b.Metadata.SeriesIndex = row.seriesIndex.String
		}
		// Metadata tables are optional enrichment. A damaged relation or value
		// for one book should not make the rest of the library unavailable.
		if err = readBookMetadata(tx, row.id, &b); err != nil {
			appendBookIssue(&b, fmt.Sprintf("metadata: %v", err))
		}
		if err = readPageCount(tx, row.id, &b); err != nil {
			appendBookIssue(&b, fmt.Sprintf("page count: %v", err))
		}
		if err = readBookAssets(absRoot, tx, row.id, row.bookPath.String, row.hasCover.Int64 != 0, &b); err != nil {
			appendBookIssue(&b, fmt.Sprintf("formats: %v", err))
		}
		books = append(books, b)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("finish Calibre read snapshot: %w", err)
	}
	return books, nil
}

func appendBookIssue(book *Book, issue string) {
	issue = strings.TrimSpace(issue)
	if issue == "" {
		return
	}
	if book.Issue == "" {
		book.Issue = issue
		return
	}
	book.Issue += "; " + issue
}

type queryer interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

func readLibraryUUID(db queryer) string {
	columns, err := tableColumns(db, "library_id")
	if err != nil || !columns["uuid"] {
		return ""
	}
	var value sql.NullString
	if err := db.QueryRow(`SELECT uuid FROM library_id LIMIT 1`).Scan(&value); err != nil {
		return ""
	}
	return value.String
}

func readRawBookRows(db queryer) (map[int64]map[string]any, error) {
	rows, err := db.Query(`SELECT * FROM books ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRowsByBook(rows, "id")
}

func readCustomValues(db queryer) (map[int64][]map[string]any, error) {
	columns, err := tableColumns(db, "custom_columns")
	if err != nil {
		if isMissingTableError(err) {
			return map[int64][]map[string]any{}, nil
		}
		return nil, err
	}
	if !columns["id"] {
		return map[int64][]map[string]any{}, nil
	}
	rows, err := db.Query(`SELECT * FROM custom_columns ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defs, err := collectRows(rows)
	if err != nil {
		return nil, err
	}
	result := make(map[int64][]map[string]any)
	for _, def := range defs {
		id, ok := integerValue(def["id"])
		if !ok || id < 1 {
			continue
		}
		table := fmt.Sprintf("custom_column_%d", id)
		cols, err := tableColumns(db, table)
		if err != nil {
			if isMissingTableError(err) {
				continue
			}
			return nil, err
		}
		if !cols["book"] {
			continue
		}
		quoted := `"` + table + `"`
		valueRows, err := db.Query(`SELECT * FROM ` + quoted + ` ORDER BY book`)
		if err != nil {
			return nil, err
		}
		bookRows, err := collectRowsGroupedByBook(valueRows, "book")
		if err != nil {
			return nil, err
		}
		for bookID, value := range bookRows {
			for _, row := range value {
				result[bookID] = append(result[bookID], map[string]any{"definition": def, "value": row})
			}
		}
	}
	return result, nil
}

func readReadingState(db queryer) (map[int64]map[string][]map[string]any, error) {
	result := make(map[int64]map[string][]map[string]any)
	for _, table := range []string{"last_read_positions", "annotations"} {
		cols, err := tableColumns(db, table)
		if err != nil {
			if isMissingTableError(err) {
				continue
			}
			return nil, err
		}
		if !cols["book"] {
			continue
		}
		rows, err := db.Query(`SELECT * FROM "` + table + `" ORDER BY book`)
		if err != nil {
			return nil, err
		}
		byBook, err := collectRowsGroupedByBook(rows, "book")
		if err != nil {
			return nil, err
		}
		for id, records := range byBook {
			result[id] = ensureState(result[id])
			result[id][table] = append(result[id][table], records...)
		}
	}
	return result, nil
}

func ensureState(state map[string][]map[string]any) map[string][]map[string]any {
	if state == nil {
		return make(map[string][]map[string]any)
	}
	return state
}

func collectRowsByBook(rows *sql.Rows, key string) (map[int64]map[string]any, error) {
	grouped, err := collectRowsGroupedByBook(rows, key)
	if err != nil {
		return nil, err
	}
	result := make(map[int64]map[string]any, len(grouped))
	for id, records := range grouped {
		if len(records) > 0 {
			result[id] = records[0]
		}
	}
	return result, nil
}

func collectRowsGroupedByBook(rows *sql.Rows, key string) (map[int64][]map[string]any, error) {
	defer rows.Close()
	items, err := collectRows(rows)
	if err != nil {
		return nil, err
	}
	result := make(map[int64][]map[string]any, len(items))
	for _, item := range items {
		id, ok := integerValue(item[key])
		if ok {
			result[id] = append(result[id], item)
		}
	}
	return result, nil
}

func collectRows(rows *sql.Rows) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		item := make(map[string]any, len(columns))
		for i, name := range columns {
			item[name] = jsonValue(values[i])
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func jsonValue(value any) any {
	if bytes, ok := value.([]byte); ok {
		return string(bytes)
	}
	return value
}

func integerValue(value any) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func validateSchema(db queryer) error {
	for _, table := range []string{"books", "data"} {
		var found string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("required table %q is missing", table)
		}
		if err != nil {
			return err
		}
	}
	for table, required := range map[string][]string{
		"books": {"id", "title", "timestamp", "pubdate", "author_sort", "sort", "series_index", "path", "has_cover", "last_modified"},
		"data":  {"book", "format", "uncompressed_size", "name"},
	} {
		columns, err := tableColumns(db, table)
		if err != nil {
			return err
		}
		for _, name := range required {
			if !columns[name] {
				return fmt.Errorf("required column %s.%s is missing", table, name)
			}
		}
	}
	return nil
}

func tableColumns(db queryer, table string) (map[string]bool, error) {
	// table is a constant selected by this package, never database input.
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func readBookMetadata(db queryer, id int64, b *Book) error {
	if err := optionalStringRows(db, `SELECT a.name FROM books_authors_link l JOIN authors a ON a.id=l.author WHERE l.book=? ORDER BY l.id`, id, func(v string) {
		b.Metadata.Authors = append(b.Metadata.Authors, v)
	}); err != nil {
		return err
	}
	if err := optionalStringRows(db, `SELECT t.name FROM books_tags_link l JOIN tags t ON t.id=l.tag WHERE l.book=? ORDER BY l.id`, id, func(v string) {
		b.Metadata.Subjects = append(b.Metadata.Subjects, v)
	}); err != nil {
		return err
	}
	if err := optionalStringRows(db, `SELECT l.lang_code FROM books_languages_link bl JOIN languages l ON l.id=bl.lang_code WHERE bl.book=? ORDER BY bl.item_order, bl.id`, id, func(v string) {
		if b.Metadata.Language == "" {
			b.Metadata.Language = v
		}
	}); err != nil {
		return err
	}
	if err := optionalStringRows(db, `SELECT p.name FROM books_publishers_link l JOIN publishers p ON p.id=l.publisher WHERE l.book=? ORDER BY l.id LIMIT 1`, id, func(v string) {
		b.Metadata.Publisher = v
	}); err != nil {
		return err
	}
	if err := optionalStringRows(db, `SELECT s.name FROM books_series_link l JOIN series s ON s.id=l.series WHERE l.book=? ORDER BY l.id LIMIT 1`, id, func(v string) {
		b.Metadata.Series = v
	}); err != nil {
		return err
	}
	rows, err := db.Query(`SELECT type, val FROM identifiers WHERE book=? ORDER BY id`, id)
	if errors.Is(err, sql.ErrNoRows) { // Query generally does not return this; retained for driver compatibility.
		return nil
	}
	if err != nil && !isMissingTableError(err) {
		return err
	}
	if err == nil {
		b.Metadata.Identifiers = make(map[string]string)
		for rows.Next() {
			var kind, value string
			if err = rows.Scan(&kind, &value); err != nil {
				rows.Close()
				return err
			}
			b.Metadata.Identifiers[kind] = value
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	var description sql.NullString
	err = db.QueryRow(`SELECT text FROM comments WHERE book=?`, id).Scan(&description)
	if err == nil {
		b.Metadata.Description = description.String
	} else if !errors.Is(err, sql.ErrNoRows) && !isMissingTableError(err) {
		return err
	}
	return nil
}

func optionalStringRows(db queryer, query string, id int64, accept func(string)) error {
	rows, err := db.Query(query, id)
	if err != nil {
		if isMissingTableError(err) {
			return nil
		}
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return err
		}
		accept(value)
	}
	return rows.Err()
}

func readBookAssets(root string, db queryer, id int64, rel string, hasCover bool, b *Book) error {
	rows, err := db.Query(`SELECT format, uncompressed_size, name FROM data WHERE book=? ORDER BY id`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	bookDir, bookDirErr := safeBookPath(root, rel)
	for rows.Next() {
		var format, name string
		var size int64
		if err = rows.Scan(&format, &size, &name); err != nil {
			return err
		}
		format = normalizeFormat(format)
		if format == "" {
			continue
		}
		b.Formats = append(b.Formats, format)
		asset := Asset{Format: format, Name: name + "." + strings.ToLower(format), Size: size}
		if bookDirErr != nil {
			asset.Issue = bookDirErr.Error()
		} else if filepath.Base(name) != name || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
			asset.Issue = "Calibre format filename is not a plain filename"
		} else {
			asset.Path, err = safeExistingFile(root, filepath.Join(bookDir, asset.Name))
			if err != nil {
				asset.Issue = fmt.Sprintf("recorded %s is unavailable: %v", format, err)
			}
		}
		if format == "EPUB" && asset.Path != "" {
			b.EPUBPath = asset.Path
			b.Metadata.FileSize = size
		} else if format == "EPUB" && b.Issue == "" {
			b.Issue = asset.Issue
		}
		b.Assets = append(b.Assets, asset)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	sort.Strings(b.Formats)
	b.Formats = unique(b.Formats)
	if hasCover && bookDirErr == nil {
		candidate, err := safeExistingFile(root, filepath.Join(bookDir, "cover.jpg"))
		if err == nil {
			b.CoverPath = candidate
		}
	}
	return nil
}

func normalizeFormat(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func readPageCount(db queryer, id int64, b *Book) error {
	err := db.QueryRow(`SELECT pages FROM books_pages_link WHERE book=?`, id).Scan(&b.Metadata.PageCount)
	if errors.Is(err, sql.ErrNoRows) || isMissingTableError(err) {
		return nil
	}
	return err
}

func splitAuthorSort(value string) []string {
	parts := strings.Split(value, " & ")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func safeBookPath(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, `\`) {
		return "", errors.New("Calibre book directory path is invalid")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("Calibre book directory path escapes the library")
	}
	return safeExistingDirectory(root, filepath.Join(root, clean))
}

func safeExistingDirectory(root, path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve book directory: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if !inside(resolvedRoot, resolved) {
		return "", errors.New("Calibre book directory resolves outside the library")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("Calibre book path is not a directory")
	}
	return resolved, nil
}

func safeExistingFile(root, path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if !inside(resolvedRoot, resolved) {
		return "", errors.New("Calibre book file resolves outside the library")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("Calibre book file is not a regular file")
	}
	return resolved, nil
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func calibreDate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "0001-01-01") || strings.HasPrefix(value, "0101-01-01") {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999-07:00", "2006-01-02 15:04:05-07:00", "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	return value
}

func unique(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func isMissingTableError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table")
}

func sqliteFilePath(path string) string {
	slashPath := filepath.ToSlash(path)
	if volume := filepath.VolumeName(path); volume != "" {
		slashVolume := filepath.ToSlash(volume)
		slashPath = "/" + slashVolume + strings.TrimPrefix(slashPath, slashVolume)
	}
	return slashPath
}
