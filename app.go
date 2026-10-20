package main

import (
	"context"
	"net/http"

	backend "attune/internal/app"
	"attune/internal/model"
)

type appBindings interface {
	GetState() (model.AppState, error)
	GetOnboardingState() (model.OnboardingState, error)
	CompleteOnboarding() error
	FindCalibreLibraries(bool) ([]model.CalibreLibraryLocation, error)
	ImportCalibreLibraryAt(string) (model.ImportResult, error)
	ImportBooks() (model.ImportResult, error)
	ImportCalibreLibrary() (model.ImportResult, error)
	ReadClipboardFiles() ([]string, error)
	ImportPaths([]string) (model.ImportResult, error)
	UpdateBook(string, string, []string) (model.Book, error)
	UpdateBookMetadata(string, model.BookMetadataEdit) (model.Book, error)
	ChangeBookCover(string) (model.Book, error)
	GetBookPreview(string) (model.BookPreview, error)
	OpenBookFile(string, string) error
	AttachBookFiles(string) (model.ImportResult, error)
	GetReadingProgress(string) (model.ReadingProgress, error)
	SaveReadingProgress(string, int, float64) error
	GetReaderLocation(string, string) (backend.ReaderLocation, error)
	SaveReaderLocation(string, string, int, string) error
	GetReadingBookmarks(string) ([]model.ReadingBookmark, error)
	AddReadingBookmark(string, int, float64) (model.ReadingBookmark, error)
	DeleteReadingBookmark(string, int64) error
	DeleteBook(string) error
	SelectDevice() (model.Device, error)
	GetDevicePreferences(string) (backend.DevicePreferences, error)
	SaveDevicePreferences(string, []string, []string) error
	ResetDevicePreferences(string) error
	GetDeviceBooks(string) ([]model.DeviceBook, error)
	SendBooks(string, []string) error
	CancelTransfer()
	RemoveDeviceBooks(string, []string) error
	EjectDevice(string) error
	OpenLibraryFolder() error
	ChangeLibraryLocation() (string, error)
	RestartNow() error
}

type UpdateState struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	Available      bool   `json:"available"`
	Downloaded     bool   `json:"downloaded"`
	Status         string `json:"status"`
	ReleaseURL     string `json:"releaseURL"`
	ReleaseNotes   string `json:"releaseNotes"`
	Progress       int    `json:"progress"`
	Received       int64  `json:"received"`
	Total          int64  `json:"total"`
	Error          string `json:"error"`
}

// App keeps Wails bindings in the main package while the implementation lives
// in internal/app.
type App struct {
	appBindings
	core *backend.App
}

func NewApp() *App {
	core := backend.NewApp()
	return &App{appBindings: core, core: core}
}

func (a *App) startup(ctx context.Context) {
	followSystemAppIcon()
	a.core.Startup(ctx)
}

func (a *App) shutdown(ctx context.Context) { a.core.Shutdown(ctx) }

func (a *App) beforeClose(ctx context.Context) bool { return a.core.BeforeClose(ctx) }

func (a *App) covers(w http.ResponseWriter, r *http.Request) {
	a.core.ServeCovers(w, r)
}

func (a *App) previewResource(w http.ResponseWriter, r *http.Request) {
	a.core.ServePreviewResource(w, r)
}

func (a *App) bookFile(w http.ResponseWriter, r *http.Request) {
	a.core.ServeBookFile(w, r)
}

func (a *App) bookArchive(w http.ResponseWriter, r *http.Request) {
	a.core.ServeBookArchive(w, r)
}

func mapUpdateState(state backend.UpdateState) UpdateState {
	return UpdateState{
		CurrentVersion: state.CurrentVersion,
		LatestVersion:  state.LatestVersion,
		Available:      state.Available,
		Downloaded:     state.Downloaded,
		Status:         state.Status,
		ReleaseURL:     state.ReleaseURL,
		ReleaseNotes:   state.ReleaseNotes,
		Progress:       state.Progress,
		Received:       state.Received,
		Total:          state.Total,
		Error:          state.Error,
	}
}

func (a *App) GetUpdateState() UpdateState {
	return mapUpdateState(a.core.GetUpdateState())
}

func (a *App) CheckForUpdates() (UpdateState, error) {
	state, err := a.core.CheckForUpdates()
	return mapUpdateState(state), err
}

func (a *App) DownloadUpdate() (UpdateState, error) {
	state, err := a.core.DownloadUpdate()
	return mapUpdateState(state), err
}
