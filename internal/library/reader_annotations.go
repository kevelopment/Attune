package library

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ReaderAnnotation is a source-backed annotation shown in the reader sidebar.
// A CFI remains unplaced until the active reader successfully resolves it.
type ReaderAnnotation struct {
	ID        string `json:"id"`
	BookID    string `json:"bookId"`
	AssetID   string `json:"assetId,omitempty"`
	Format    string `json:"format,omitempty"`
	Kind      string `json:"kind"`
	Text      string `json:"text,omitempty"`
	Note      string `json:"note,omitempty"`
	Quote     string `json:"quote,omitempty"`
	CFI       string `json:"cfi,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Status    string `json:"status"`
}

// CalibreAnnotation is the normalized import payload for one Calibre reading
// state record. SourceID stays stable across imports from the same source.
type CalibreAnnotation struct {
	SourceID  string
	Format    string
	Kind      string
	Text      string
	Note      string
	Quote     string
	CFI       string
	Timestamp string
	Progress  *float64
	Status    string
}

func (s *Store) ensureReaderAnnotationSchema() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS reader_annotations (
		id TEXT PRIMARY KEY,
		book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
		asset_id TEXT NOT NULL DEFAULT '',
		format TEXT NOT NULL DEFAULT '',
		source_identity TEXT NOT NULL,
		source_annotation_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		text TEXT NOT NULL DEFAULT '',
		note TEXT NOT NULL DEFAULT '',
		quote TEXT NOT NULL DEFAULT '',
		cfi TEXT NOT NULL DEFAULT '',
		timestamp TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'unplaced',
		source_updated_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL,
		UNIQUE(book_id, source_identity, source_annotation_id)
	);
	CREATE INDEX IF NOT EXISTS reader_annotations_book_asset ON reader_annotations(book_id, asset_id, timestamp);`)
	if err != nil {
		return err
	}
	rows, err := s.db.Query(`PRAGMA table_info(reader_annotations)`)
	if err != nil {
		return err
	}
	hasFormat := false
	for rows.Next() {
		var cid, notnull, primary int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notnull, &defaultValue, &primary); err != nil {
			rows.Close()
			return err
		}
		if name == "format" {
			hasFormat = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !hasFormat {
		_, err = s.db.Exec(`ALTER TABLE reader_annotations ADD COLUMN format TEXT NOT NULL DEFAULT ''`)
	}
	return err
}

// ImportCalibreAnnotations inserts source annotations idempotently. It updates
// a prior source record only when Calibre supplies a newer timestamp; it does
// inserts a source CFI location only when no Attune location exists.
func (s *Store) ImportCalibreAnnotations(bookID, sourceIdentity string, annotations []CalibreAnnotation) error {
	if !validBookID(bookID) || strings.TrimSpace(sourceIdentity) == "" {
		return errors.New("Calibre annotation source identity and book are required")
	}
	if len(annotations) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureReaderAnnotationSchema(); err != nil {
		return err
	}
	var belongs bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM books WHERE id=?)`, bookID).Scan(&belongs); err != nil {
		return err
	}
	if !belongs {
		return ErrNotFound
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range annotations {
		if strings.TrimSpace(item.SourceID) == "" {
			continue
		}
		format := strings.ToLower(strings.TrimSpace(item.Format))
		if format == "" && item.CFI != "" {
			format = "epub"
		}
		assetID := ""
		assetCount := 0
		if format != "" {
			if err := tx.QueryRow(`SELECT COUNT(*),COALESCE(MIN(id),'') FROM book_files WHERE book_id=? AND LOWER(format)=?`, bookID, format).Scan(&assetCount, &assetID); err != nil {
				return err
			}
			if assetCount != 1 {
				assetID = ""
			}
		}
		status := "unplaced"
		now := time.Now().UTC().Format(time.RFC3339Nano)
		_, err = tx.Exec(`INSERT INTO reader_annotations(
			id,book_id,asset_id,format,source_identity,source_annotation_id,kind,text,note,quote,cfi,timestamp,status,source_updated_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(book_id,source_identity,source_annotation_id) DO UPDATE SET
			asset_id=excluded.asset_id,format=excluded.format,kind=excluded.kind,text=excluded.text,note=excluded.note,quote=excluded.quote,
			cfi=excluded.cfi,timestamp=excluded.timestamp,status=excluded.status,
			source_updated_at=excluded.source_updated_at,updated_at=excluded.updated_at
		WHERE excluded.source_updated_at<>'' AND excluded.source_updated_at>reader_annotations.source_updated_at`,
			annotationRowID(bookID, sourceIdentity, item.SourceID), bookID, assetID, format, sourceIdentity, item.SourceID,
			item.Kind, item.Text, item.Note, item.Quote, item.CFI, item.Timestamp, status, item.Timestamp, now)
		if err != nil {
			return err
		}
		if assetID != "" && format == "epub" && item.Kind == "position" && (item.CFI != "" || item.Progress != nil) {
			progress := 0.0
			if item.Progress != nil && *item.Progress >= 0 && *item.Progress <= 1 {
				progress = *item.Progress
			}
			kind := "text-offset"
			var locatorValue any = 0
			if item.CFI != "" {
				kind = "cfi"
				locatorValue = item.CFI
			}
			locator, marshalErr := json.Marshal(struct {
				Version  int     `json:"version"`
				Kind     string  `json:"kind"`
				Value    any     `json:"value"`
				Progress float64 `json:"progress"`
			}{Version: 1, Kind: kind, Value: locatorValue, Progress: progress})
			if marshalErr != nil {
				return marshalErr
			}
			if _, err = tx.Exec(`INSERT OR IGNORE INTO reader_locations(book_id,asset_id,schema_version,locator_json,updated_at) VALUES(?,?,?,?,?)`, bookID, assetID, 1, string(locator), now); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func annotationRowID(bookID, sourceIdentity, sourceAnnotationID string) string {
	return bookID + ":calibre:" + sourceIdentity + ":" + sourceAnnotationID
}

// GetReaderAnnotations returns annotations for a single asset plus unresolved
// items whose Calibre format could not be linked unambiguously.
func (s *Store) GetReaderAnnotations(bookID, assetID string) ([]ReaderAnnotation, error) {
	if !validBookID(bookID) {
		return nil, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureReaderAnnotationSchema(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id,book_id,asset_id,format,kind,text,note,quote,cfi,timestamp,status
		FROM reader_annotations WHERE book_id=? AND (asset_id=? OR asset_id='')
		ORDER BY CASE WHEN timestamp='' THEN 1 ELSE 0 END,timestamp DESC,id`, bookID, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ReaderAnnotation, 0)
	for rows.Next() {
		var item ReaderAnnotation
		var asset string
		if err := rows.Scan(&item.ID, &item.BookID, &asset, &item.Format, &item.Kind, &item.Text, &item.Note, &item.Quote, &item.CFI, &item.Timestamp, &item.Status); err != nil {
			return nil, err
		}
		item.AssetID = asset
		items = append(items, item)
	}
	return items, rows.Err()
}
