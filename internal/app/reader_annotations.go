package app

import (
	"errors"

	"attune/internal/library"
)

// ReaderAnnotation is re-exported so Wails can return source annotations to
// the reader sidebar.
type ReaderAnnotation = library.ReaderAnnotation

func (a *App) GetReaderAnnotations(bookID, assetID string) ([]library.ReaderAnnotation, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return nil, errors.New("application is shutting down")
	}
	return a.store.GetReaderAnnotations(bookID, assetID)
}
