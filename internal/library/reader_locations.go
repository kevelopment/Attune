package library

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ReaderLocation stores a versioned position belonging to one specific book
// asset. Locators are opaque to the library package so reader engines can
// evolve their own schemas without changing the persistence layer.
type ReaderLocation struct {
	BookID        string          `json:"bookId"`
	AssetID       string          `json:"assetId"`
	Found         bool            `json:"found"`
	SchemaVersion int             `json:"schemaVersion"`
	Locator       json.RawMessage `json:"locator"`
	UpdatedAt     string          `json:"updatedAt"`
}

// GetReaderLocation returns the persisted location for a book asset. The bool
// is false when no location has been saved yet.
func (s *Store) GetReaderLocation(bookID, assetID string) (ReaderLocation, bool, error) {
	if !validBookID(bookID) || assetID == "" {
		return ReaderLocation{}, false, ErrNotFound
	}
	var location ReaderLocation
	var locator, updatedAt string
	err := s.db.QueryRow(`SELECT book_id,asset_id,schema_version,locator_json,updated_at
		FROM reader_locations WHERE book_id=? AND asset_id=?`, bookID, assetID).
		Scan(&location.BookID, &location.AssetID, &location.SchemaVersion, &locator, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ReaderLocation{}, false, nil
	}
	if err != nil {
		return ReaderLocation{}, false, err
	}
	location.Found = true
	location.Locator = json.RawMessage(locator)
	location.UpdatedAt = updatedAt
	return location, true, nil
}

// SaveReaderLocation persists a versioned reader-engine locator. A location
// is only accepted for an asset that belongs to the supplied book.
func (s *Store) SaveReaderLocation(bookID, assetID string, version int, locator json.RawMessage) error {
	if !validBookID(bookID) || assetID == "" || version < 1 || len(locator) == 0 || !json.Valid(locator) {
		return errors.New("invalid reader location")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var belongs bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM book_files WHERE id=? AND book_id=?)`, assetID, bookID).Scan(&belongs); err != nil {
		return err
	}
	if !belongs {
		return errors.New("reader asset does not belong to book")
	}
	_, err := s.db.Exec(`INSERT INTO reader_locations(book_id,asset_id,schema_version,locator_json,updated_at)
		VALUES(?,?,?,?,?) ON CONFLICT(book_id,asset_id) DO UPDATE SET
		schema_version=excluded.schema_version,locator_json=excluded.locator_json,updated_at=excluded.updated_at`,
		bookID, assetID, version, string(locator), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
