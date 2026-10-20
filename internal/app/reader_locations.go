package app

import (
	"encoding/json"
	"errors"

	"attune/internal/library"
)

// ReaderLocation is re-exported for Wails so the frontend can persist a
// versioned position against one specific format asset.
type ReaderLocation = library.ReaderLocation

func (a *App) GetReaderLocation(bookID, assetID string) (library.ReaderLocation, error) {
	if err := a.ready(); err != nil {
		return library.ReaderLocation{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return library.ReaderLocation{}, errors.New("application is shutting down")
	}
	location, found, err := a.store.GetReaderLocation(bookID, assetID)
	location.Found = found
	return location, err
}

func (a *App) SaveReaderLocation(bookID, assetID string, schemaVersion int, locator string) error {
	if len(locator) == 0 || len(locator) > 1<<20 || !json.Valid([]byte(locator)) {
		return errors.New("invalid reader location")
	}
	if err := a.ready(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return errors.New("application is shutting down")
	}
	return a.store.SaveReaderLocation(bookID, assetID, schemaVersion, json.RawMessage(locator))
}
