package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"attune/internal/calibre"
	"attune/internal/converter"
	"attune/internal/device"
	"attune/internal/epub"
	"attune/internal/formats"
	"attune/internal/library"
	"attune/internal/model"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx                context.Context
	store              *library.Store
	manager            deviceManager
	converter          converterRunner
	root               string
	configPath         string
	initErr            error
	converterVersion   string
	converterErr       error
	mu                 sync.Mutex
	selected           map[string]model.Device
	busy               bool
	closing            bool
	cancel             context.CancelFunc
	jobs               sync.WaitGroup
	emit               func(string, ...interface{})
	updateMu           sync.Mutex
	updateState        UpdateState
	updateLastProgress time.Time
	updateScheduled    bool
	updateCancel       context.CancelFunc
	activeOps          atomic.Int32
}

type deviceManager interface {
	Discover() ([]model.Device, error)
	SelectVolume(string) (model.Device, error)
	List(model.Device) ([]model.DeviceBook, error)
	Send(context.Context, model.Device, string, string) (string, error)
	Remove(model.Device, string) error
	Eject(context.Context, model.Device) error
}

type converterRunner interface {
	Version(context.Context) (string, error)
	Convert(context.Context, string, string, string, []string) error
}

func NewApp() *App {
	state := UpdateState{CurrentVersion: appVersion, Status: "idle"}
	if isDesktopBinary() {
		state = loadUpdateState()
	}
	return &App{selected: make(map[string]model.Device), updateState: state}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.emit = func(event string, data ...interface{}) { wruntime.EventsEmit(ctx, event, data...) }
	envRoot := os.Getenv("EBOOK_LIBRARY_HOME")
	root := envRoot
	configPath, err := librarySettingsPath(envRoot)
	if err != nil {
		a.initErr = err
		return
	}
	a.configPath = configPath
	if envRoot == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			a.initErr = err
			return
		}
		// Keep the original data folder so existing Ebook Library users retain
		// their managed books and conversion cache after the Attune rename.
		root = filepath.Join(base, "Ebook Library")
	}
	if configured, found, err := loadLibraryLocation(configPath); err != nil {
		a.initErr = err
		return
	} else if found {
		root = configured
		if st, err := os.Stat(filepath.Join(root, "library.sqlite")); err != nil || !st.Mode().IsRegular() {
			if err == nil {
				err = errors.New("library database is not a regular file")
			}
			a.initErr = fmt.Errorf("configured library is unavailable: %w", err)
			return
		}
	}
	a.root = root
	a.store, a.initErr = library.Open(root)
	if a.initErr != nil {
		return
	}
	a.manager = device.NewManager()
	a.startTransportDiscovery(ctx)
	a.converter = converter.New(converterPath())
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	a.converterVersion, a.converterErr = a.converter.Version(checkCtx)
}

func (a *App) startTransportDiscovery(ctx context.Context) {
	refresher, ok := a.manager.(interface {
		RefreshTransports(context.Context) (bool, error)
	})
	if !ok {
		return
	}
	go func() {
		refresh := func() {
			refreshCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			changed, err := refresher.RefreshTransports(refreshCtx)
			cancel()
			if changed {
				a.event("devices:changed")
			}
			if err != nil && ctx.Err() == nil {
				// Discovery errors are surfaced when a user opens or refreshes a
				// device; startup stays responsive while readers are busy.
				return
			}
		}
		refresh()
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refresh()
			}
		}
	}()
}

func converterPath() string {
	if p := os.Getenv("EBOOK_LIBRARY_CONVERTER"); p != "" {
		return p
	}
	name := "kindling-cli"
	if goruntime.GOOS == "windows" {
		name += ".exe"
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if goruntime.GOOS == "darwin" {
			resource := filepath.Clean(filepath.Join(dir, "..", "Resources", name))
			if st, err := os.Stat(resource); err == nil && !st.IsDir() {
				return resource
			}
		}
		adjacent := filepath.Join(dir, name)
		if st, err := os.Stat(adjacent); err == nil && !st.IsDir() {
			return adjacent
		}
	}
	return filepath.Join("third_party", "kindling", goruntime.GOOS+"-"+goruntime.GOARCH, name)
}

func (a *App) ready() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initErr != nil {
		return fmt.Errorf("open library: %w", a.initErr)
	}
	if a.store == nil {
		return errors.New("library is starting; please try again")
	}
	return nil
}

func (a *App) event(name string, data ...interface{}) {
	if a.emit != nil {
		a.emit(name, data...)
	}
}

func (a *App) Shutdown(ctx context.Context) {
	a.mu.Lock()
	a.closing = true
	if a.cancel != nil {
		a.cancel()
	}
	if a.updateCancel != nil {
		a.updateCancel()
		a.updateCancel = nil
	}
	a.mu.Unlock()
	a.jobs.Wait()
	a.mu.Lock()
	if a.store != nil {
		_ = a.store.Close()
		a.store = nil
	}
	a.mu.Unlock()
	a.applyUpdateOnQuit()
}

func (a *App) BeforeClose(ctx context.Context) bool {
	a.mu.Lock()
	busy := a.busy
	a.mu.Unlock()
	if !busy {
		return false
	}
	answer, err := wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{Type: wruntime.QuestionDialog, Title: "Transfer in progress", Message: "Cancel the transfer and close Attune?", Buttons: []string{"Keep open", "Cancel and close"}, DefaultButton: "Keep open", CancelButton: "Keep open"})
	if err != nil || answer != "Cancel and close" {
		return true
	}
	a.CancelTransfer()
	return false
}

func (a *App) GetState() (model.AppState, error) {
	if err := a.ready(); err != nil {
		return model.AppState{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return model.AppState{}, errors.New("application is shutting down")
	}
	books, err := a.store.ListBooks()
	if err != nil {
		return model.AppState{}, err
	}
	devices, discoverErr := a.manager.Discover()
	if discoverErr != nil {
		devices = nil
	}
	if devices == nil {
		devices = []model.Device{}
	}
	seen := map[string]bool{}
	for _, d := range devices {
		seen[d.ID] = true
	}
	selected := make([]model.Device, 0, len(a.selected))
	for _, d := range a.selected {
		selected = append(selected, d)
	}
	for _, d := range selected {
		if seen[d.ID] {
			continue
		}
		valid, e := a.manager.SelectVolume(d.Root)
		if e == nil && valid.ID == d.ID {
			devices = append(devices, valid)
		} else {
			delete(a.selected, d.ID)
		}
	}
	for i := range devices {
		d := devices[i]
		if err := a.store.RecordDevice(d); err != nil {
			return model.AppState{}, err
		}
		if folders, formats, found, err := a.store.LoadDevicePreferences(d.ID); err != nil {
			return model.AppState{}, fmt.Errorf("load preferences for %s: %w", d.Name, err)
		} else if found {
			d.BookFolders = folders
			d.TransferFormats = formats
			if d.ProfileID == "generic" {
				d.RequiresExplicitSelection = len(folders) == 0 || len(formats) == 0
			}
		}
		// Operations receive the effective settings from this AppState value.
		devices[i] = d
	}
	if books == nil {
		books = []model.Book{}
	}
	return model.AppState{Books: books, Devices: devices, ConverterReady: a.converterErr == nil, ConverterVersion: a.converterVersion, LibraryPath: a.root}, nil
}

func (a *App) ImportBooks() (model.ImportResult, error) {
	paths, err := wruntime.OpenMultipleFilesDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Import books", Filters: []wruntime.FileFilter{{DisplayName: "Supported ebooks", Pattern: "*.epub;*.pdf;*.mobi;*.azw;*.azw3;*.prc;*.fb2;*.txt;*.cbz;*.cbr;*.rtf;*.doc;*.docx;*.odt;*.html;*.htmlz;*.pdb;*.lit;*.zip"}}})
	if err != nil {
		return model.ImportResult{}, err
	}
	return a.ImportPaths(paths)
}

// ImportCalibreLibrary copies Calibre format files, metadata and reading state.
func (a *App) ImportCalibreLibrary() (model.ImportResult, error) {
	if err := a.ready(); err != nil {
		return model.ImportResult{BookIDs: []string{}, Errors: []string{}}, err
	}
	root, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Choose a Calibre library"})
	if err != nil || root == "" {
		return model.ImportResult{BookIDs: []string{}, Errors: []string{}}, err
	}
	return a.importCalibreLibrary(root)
}

func (a *App) importCalibreLibrary(root string) (model.ImportResult, error) {
	result := model.ImportResult{BookIDs: []string{}, Errors: []string{}}
	a.event("import:progress", model.ImportProgress{Stage: "importing", Filename: "Reading Calibre library…"})
	if err := a.ready(); err != nil {
		return result, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return result, errors.New("application is shutting down")
	}
	if a.busy || a.activeOps.Load() != 0 {
		return result, errors.New("wait for the current library operation to finish before importing")
	}
	a.activeOps.Add(1)
	defer a.activeOps.Add(-1)
	books, err := calibre.ReadLibrary(root)
	if err != nil {
		a.event("import:progress", model.ImportProgress{Stage: "error", Filename: "Could not read Calibre library"})
		return result, err
	}
	total := len(books)
	if total == 0 {
		return result, nil
	}
	batchID, err := library.NewImportBatchID()
	if err != nil {
		return result, err
	}
	emit := func(p model.ImportProgress) { a.event("import:progress", p) }
	failed := 0
	seenBookIDs := make(map[string]struct{}, total)
	for i, item := range books {
		name := item.Metadata.Title
		if strings.TrimSpace(name) == "" {
			name = item.ID
		}
		filename := name
		emit(model.ImportProgress{Current: i, Total: total, Filename: filename, Imported: result.Imported, Duplicates: result.Duplicates, Failed: failed, Skipped: result.Skipped, Stage: "importing"})
		bookID, duplicate, itemErrors, importErr := a.importCalibreRecord(root, item, batchID)
		if importErr != nil {
			result.Errors = append(result.Errors, filename+": "+importErr.Error())
			failed++
		} else {
			for _, itemErr := range itemErrors {
				result.Errors = append(result.Errors, filename+": "+itemErr)
				failed++
			}
			if _, seen := seenBookIDs[bookID]; !seen {
				result.BookIDs = append(result.BookIDs, bookID)
				seenBookIDs[bookID] = struct{}{}
			}
			if duplicate {
				result.Duplicates++
			} else {
				result.Imported++
			}
		}
		emit(model.ImportProgress{Current: i + 1, Total: total, Filename: filename, Imported: result.Imported, Duplicates: result.Duplicates, Failed: failed, Skipped: result.Skipped, Stage: "importing"})
	}
	emit(model.ImportProgress{Current: total, Total: total, Imported: result.Imported, Duplicates: result.Duplicates, Failed: failed, Skipped: result.Skipped, Stage: "done"})
	a.event("library:changed")
	return result, nil
}

func (a *App) importCalibreRecord(root string, item calibre.Book, batchID string) (bookID string, duplicate bool, issues []string, err error) {
	defer recoverImportPanic("Calibre record "+item.ID, &err)
	issues = make([]string, 0)
	if item.Issue != "" {
		issues = append(issues, item.Issue)
	}
	libraryUUID := strings.TrimSpace(item.LibraryUUID)
	if libraryUUID == "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", false, issues, err
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err == nil {
			abs = resolved
		}
		sum := sha256.Sum256([]byte(filepath.Clean(abs)))
		libraryUUID = "path:" + hex.EncodeToString(sum[:])
	}
	bookUUID := strings.TrimSpace(item.UUID)
	if bookUUID == "" {
		bookUUID = "record:" + item.ID
	}
	bookID, mapped, err := a.store.CalibreBookID(libraryUUID, bookUUID)
	if err != nil {
		return "", false, issues, err
	}
	duplicate = mapped
	if !mapped {
		matches := make(map[string]bool)
		for _, asset := range item.Assets {
			if asset.Path == "" || asset.Issue != "" {
				continue
			}
			hash, hashErr := fileHash(asset.Path)
			if hashErr != nil {
				continue
			}
			file, found, lookupErr := a.store.FileByHash(hash)
			if lookupErr != nil {
				return "", false, issues, lookupErr
			}
			if found {
				matches[file.BookID] = true
			}
		}
		if len(matches) == 1 {
			for id := range matches {
				bookID = id
			}
			duplicate = true
		} else if len(matches) > 1 {
			issues = append(issues, "format files match multiple existing books; conflicting assets were preserved as far as possible")
		}
	}
	if bookID == "" {
		var cover []byte
		if item.CoverPath != "" {
			cover, err = readCalibreCover(item.CoverPath)
			if err != nil {
				issues = append(issues, "cover: "+err.Error())
				cover = nil
			}
		}
		book, createErr := a.store.CreateCalibreBook(item.Metadata, item.CustomValues, item.RawMetadata, item.ReadingState, cover, batchID)
		if createErr != nil {
			return "", false, issues, createErr
		}
		bookID = book.ID
	}
	for _, asset := range item.Assets {
		if asset.Path == "" {
			if asset.Issue != "" {
				issues = append(issues, strings.ToLower(asset.Format)+": "+asset.Issue)
			}
			continue
		}
		file, alreadyPresent, attachErr := a.store.AttachBookFile(bookID, asset.Path, false)
		if attachErr != nil {
			issues = append(issues, strings.ToLower(asset.Format)+": "+attachErr.Error())
			continue
		}
		if alreadyPresent && file.BookID != bookID {
			issues = append(issues, strings.ToLower(asset.Format)+": identical content is already attached to another Attune book")
		}
	}
	if err = a.store.LinkCalibreSource(libraryUUID, bookUUID, bookID, item.RawMetadata, item.CustomValues, item.ReadingState, item.Metadata.ModifiedAt); err != nil {
		return "", duplicate, issues, err
	}
	parsedAnnotations, parseErr := calibre.ParseAnnotations(item.ReadingState)
	if parseErr != nil {
		issues = append(issues, "reading annotations: "+parseErr.Error())
	} else {
		annotations := make([]library.CalibreAnnotation, 0, len(parsedAnnotations))
		for _, annotation := range parsedAnnotations {
			annotations = append(annotations, library.CalibreAnnotation{
				SourceID: annotation.SourceID, Format: annotation.Format, Kind: annotation.Kind, Text: annotation.Text,
				Note: annotation.Note, Quote: annotation.Quote, CFI: annotation.CFI,
				Timestamp: annotation.Timestamp, Progress: annotation.Progress, Status: annotation.Status,
			})
		}
		sourceIdentity := libraryUUID + ":" + bookUUID
		if importErr := a.store.ImportCalibreAnnotations(bookID, sourceIdentity, annotations); importErr != nil {
			return "", duplicate, issues, importErr
		}
	}
	return bookID, duplicate, issues, nil
}

func readCalibreCover(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 32<<20 {
		return nil, errors.New("cover must be a regular image between 1 byte and 32 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 32<<20 {
		return nil, errors.New("cover exceeds the 32 MiB size limit")
	}
	if _, _, err = library.ValidateCover(data); err != nil {
		return nil, fmt.Errorf("invalid cover: %w", err)
	}
	return data, nil
}

// ReadClipboardFiles exposes copied local file paths through the Wails-bound
// App instance. The platform helper only reads file references from clipboard.
func (a *App) ReadClipboardFiles() ([]string, error) { return ReadClipboardFiles() }

func (a *App) ImportPaths(paths []string) (model.ImportResult, error) {
	result := model.ImportResult{BookIDs: []string{}, Errors: []string{}}
	total := len(paths)
	emitProgress := func(p model.ImportProgress) {
		a.event("import:progress", p)
	}
	emitEarlyError := func() {
		if total > 0 {
			emitProgress(model.ImportProgress{Total: total, Failed: total, Stage: "error"})
		}
	}
	if err := a.ready(); err != nil {
		emitEarlyError()
		return result, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		emitEarlyError()
		return result, errors.New("application is shutting down")
	}
	if a.busy {
		emitEarlyError()
		return result, errors.New("wait for the transfer to finish before importing")
	}
	a.activeOps.Add(1)
	defer a.activeOps.Add(-1)
	if len(paths) > 500 {
		emitEarlyError()
		return result, errors.New("import up to 500 books at a time")
	}
	batchID := ""
	if total > 0 {
		var err error
		batchID, err = library.NewImportBatchID()
		if err != nil {
			emitEarlyError()
			return result, err
		}
	}
	failed := 0
	seenBookIDs := make(map[string]struct{}, len(paths))
	for i, p := range paths {
		filename := filepath.Base(p)
		emitProgress(model.ImportProgress{Current: i, Total: total, Filename: filename, Imported: result.Imported, Duplicates: result.Duplicates, Failed: failed, Stage: "importing"})
		book, duplicate, err := importFileSafely(p, func() (model.Book, bool, error) {
			return a.store.ImportFileInBatch(p, batchID)
		})
		if err != nil {
			result.Errors = append(result.Errors, filepath.Base(p)+": "+err.Error())
			failed++
		} else {
			if _, seen := seenBookIDs[book.ID]; !seen {
				result.BookIDs = append(result.BookIDs, book.ID)
				seenBookIDs[book.ID] = struct{}{}
			}
			if duplicate {
				result.Duplicates++
			} else {
				result.Imported++
			}
		}
		emitProgress(model.ImportProgress{Current: i + 1, Total: total, Filename: filename, Imported: result.Imported, Duplicates: result.Duplicates, Failed: failed, Stage: "importing"})
	}
	if total > 0 {
		emitProgress(model.ImportProgress{Current: total, Total: total, Imported: result.Imported, Duplicates: result.Duplicates, Failed: failed, Stage: "done"})
	}
	a.event("library:changed")
	return result, nil
}

func (a *App) UpdateBook(id, title string, authors []string) (model.Book, error) {
	if err := a.ready(); err != nil {
		return model.Book{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return model.Book{}, errors.New("application is shutting down")
	}
	if a.busy {
		return model.Book{}, errors.New("wait for the transfer to finish before editing")
	}
	b, err := a.store.UpdateBook(id, title, authors)
	if err == nil {
		a.event("library:changed")
	}
	return b, err
}

// UpdateBookMetadata saves descriptive library metadata. The current converter
// applies title and author overrides to generated files; other edited fields
// remain available in the library and do not alter the immutable source EPUB.
func (a *App) UpdateBookMetadata(id string, edit model.BookMetadataEdit) (model.Book, error) {
	if err := a.ready(); err != nil {
		return model.Book{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return model.Book{}, errors.New("application is shutting down")
	}
	if a.busy {
		return model.Book{}, errors.New("wait for the transfer to finish before editing")
	}
	b, err := a.store.UpdateBookMetadata(id, edit)
	if err == nil {
		a.event("library:changed")
	}
	return b, err
}

// ChangeBookCover opens the native picker and replaces a book's managed cover.
// An empty result means the user cancelled the picker.
func (a *App) ChangeBookCover(id string) (model.Book, error) {
	if err := a.ready(); err != nil {
		return model.Book{}, err
	}
	filename, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:   "Choose a book cover",
		Filters: []wruntime.FileFilter{{DisplayName: "JPEG and PNG images", Pattern: "*.jpg;*.jpeg;*.png"}},
	})
	if err != nil || filename == "" {
		return model.Book{}, err
	}
	return a.changeBookCoverFromPath(id, filename)
}

func (a *App) changeBookCoverFromPath(id, filename string) (model.Book, error) {
	f, err := os.Open(filename)
	if err != nil {
		return model.Book{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return model.Book{}, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 32<<20 {
		return model.Book{}, errors.New("cover image must be a regular file between 1 byte and 32 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	if err != nil {
		return model.Book{}, err
	}
	if len(data) > 32<<20 {
		return model.Book{}, errors.New("cover image must be 32 MiB or smaller")
	}
	if err = a.ready(); err != nil {
		return model.Book{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return model.Book{}, errors.New("application is shutting down")
	}
	if a.busy {
		return model.Book{}, errors.New("wait for the transfer to finish before changing covers")
	}
	b, err := a.store.ReplaceBookCover(id, data)
	if err == nil {
		a.event("library:changed")
	}
	return b, err
}

func (a *App) GetBookPreview(id string) (model.BookPreview, error) {
	if err := a.ready(); err != nil {
		return model.BookPreview{}, err
	}
	a.mu.Lock()
	if a.closing {
		a.mu.Unlock()
		return model.BookPreview{}, errors.New("application is shutting down")
	}
	book, source, err := a.store.BookSource(id)
	a.mu.Unlock()
	if err != nil {
		return model.BookPreview{}, err
	}
	chapters, err := epub.Preview(source)
	if err != nil {
		return model.BookPreview{}, err
	}
	result := make([]model.PreviewChapter, 0, len(chapters))
	for _, chapter := range chapters {
		result = append(result, model.PreviewChapter{ID: chapter.ID, Title: chapter.Title, Href: previewURL(id, chapter.Href)})
	}
	return model.BookPreview{Book: book, Chapters: result}, nil
}

func (a *App) GetReadingProgress(id string) (model.ReadingProgress, error) {
	if err := a.ready(); err != nil {
		return model.ReadingProgress{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return model.ReadingProgress{}, errors.New("application is shutting down")
	}
	return a.store.ReadingProgress(id)
}

func (a *App) SaveReadingProgress(id string, chapterIndex int, position float64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if chapterIndex < 0 {
		return errors.New("invalid reading position")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return errors.New("application is shutting down")
	}
	return a.store.SaveReadingProgress(id, chapterIndex, position)
}

func (a *App) GetReadingBookmarks(id string) ([]model.ReadingBookmark, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return nil, errors.New("application is shutting down")
	}
	return a.store.ReadingBookmarks(id)
}

func (a *App) AddReadingBookmark(id string, chapterIndex int, position float64) (model.ReadingBookmark, error) {
	if err := a.ready(); err != nil {
		return model.ReadingBookmark{}, err
	}
	preview, err := a.GetBookPreview(id)
	if err != nil {
		return model.ReadingBookmark{}, err
	}
	if chapterIndex < 0 || chapterIndex >= len(preview.Chapters) {
		return model.ReadingBookmark{}, errors.New("invalid bookmark chapter")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return model.ReadingBookmark{}, errors.New("application is shutting down")
	}
	return a.store.AddReadingBookmark(id, chapterIndex, position)
}

func (a *App) DeleteReadingBookmark(id string, bookmarkID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return errors.New("application is shutting down")
	}
	return a.store.DeleteReadingBookmark(id, bookmarkID)
}

func previewURL(id, resource string) string {
	parts := strings.Split(resource, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return "/preview/" + id + "/" + strings.Join(parts, "/")
}

func (a *App) DeleteBook(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return errors.New("application is shutting down")
	}
	if a.busy {
		return errors.New("wait for the transfer to finish before removing books")
	}
	if err := a.store.DeleteBook(id); err != nil {
		return err
	}
	a.event("library:changed")
	return nil
}

func (a *App) SelectDevice() (model.Device, error) {
	if err := a.ready(); err != nil {
		return model.Device{}, err
	}
	defaultDirectory := "/media"
	switch goruntime.GOOS {
	case "darwin":
		defaultDirectory = "/Volumes"
	case "windows":
		defaultDirectory = `C:\`
	}
	root, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Choose mounted reader storage", DefaultDirectory: defaultDirectory})
	if err != nil {
		return model.Device{}, err
	}
	if root == "" {
		return model.Device{}, nil
	}
	d, err := a.manager.SelectVolume(root)
	if err != nil {
		return d, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return model.Device{}, errors.New("application is shutting down")
	}
	if err = a.store.RecordDevice(d); err != nil {
		return d, err
	}
	if folders, formats, found, prefErr := a.store.LoadDevicePreferences(d.ID); prefErr != nil {
		return d, prefErr
	} else if found {
		d.BookFolders = folders
		d.TransferFormats = formats
		if d.ProfileID == "generic" {
			d.RequiresExplicitSelection = len(folders) == 0 || len(formats) == 0
		}
	}
	a.selected[d.ID] = d
	a.event("devices:changed")
	return d, nil
}

func (a *App) findDevice(id string) (model.Device, error) {
	state, err := a.GetState()
	if err != nil {
		return model.Device{}, err
	}
	for _, d := range state.Devices {
		if d.ID == id {
			return d, nil
		}
	}
	return model.Device{}, errors.New("reader disconnected; reconnect it and try again")
}

func (a *App) GetDeviceBooks(id string) ([]model.DeviceBook, error) {
	d, err := a.findDevice(id)
	if err != nil {
		return nil, err
	}
	books, err := a.manager.List(d)
	if books == nil {
		books = []model.DeviceBook{}
	}
	if err == nil {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.closing {
			return nil, errors.New("application is shutting down")
		}
		libraryBooks, e := a.store.ListBooks()
		if e != nil {
			return nil, e
		}
		for i := range books {
			for _, book := range libraryBooks {
				if strings.Contains(books[i].Name, "-"+book.ID+"-") {
					books[i].BookID = book.ID
					for _, file := range book.Files {
						if formats.CanRead(file.Format) {
							books[i].CanRead = true
							break
						}
					}
					if books[i].Title == "" {
						books[i].Title = book.Title
					}
					mergeLibraryMetadata(&books[i], book)
					break
				}
			}
		}
	}
	return books, err
}

func mergeLibraryMetadata(dst *model.DeviceBook, src model.Book) {
	if dst.Title == "" {
		dst.Title = src.Title
	}
	if len(dst.Authors) == 0 {
		dst.Authors = append([]string(nil), src.Authors...)
	}
	if dst.Language == "" {
		dst.Language = src.Language
	}
	if len(dst.Subjects) == 0 {
		dst.Subjects = append([]string(nil), src.Subjects...)
	}
	if dst.CoverURL == "" {
		dst.CoverURL = src.CoverURL
	}
	if dst.Publisher == "" {
		dst.Publisher = src.Publisher
	}
	if dst.Published == "" {
		dst.Published = src.Published
	}
	if dst.Description == "" {
		dst.Description = src.Description
	}
	if dst.Series == "" {
		dst.Series = src.Series
	}
	if dst.SeriesIndex == "" {
		dst.SeriesIndex = src.SeriesIndex
	}
	if dst.PageCount == 0 {
		dst.PageCount = src.PageCount
	}
	if len(dst.AuthorSort) == 0 {
		dst.AuthorSort = append([]string(nil), src.AuthorSort...)
	}
	if dst.TitleSort == "" {
		dst.TitleSort = src.TitleSort
	}
	if len(src.Identifiers) > 0 {
		if dst.Identifiers == nil {
			dst.Identifiers = make(map[string]string)
		}
		for k, v := range src.Identifiers {
			if dst.Identifiers[k] == "" {
				dst.Identifiers[k] = v
			}
		}
	}
}

func cacheKey(b model.Book, version string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00kindle-1-2\x00%s", b.SHA256, b.Revision, version)))
	return hex.EncodeToString(sum[:])
}

func (a *App) SendBooks(deviceID string, ids []string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return errors.New("select at least one book")
	}
	d, err := a.findDevice(deviceID)
	if err != nil {
		return err
	}
	if d.RequiresExplicitSelection {
		return errors.New("choose a book folder and supported formats in reader settings before sending books to this unrecognized device")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return errors.New("application is shutting down")
	}
	if a.busy {
		return errors.New("a transfer is already running")
	}
	unique := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if !seen[id] {
			if _, err := a.store.GetBook(id); err != nil {
				return err
			}
			unique = append(unique, id)
			seen[id] = true
		}
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel
	a.busy = true
	a.jobs.Add(1)
	go func() {
		defer a.jobs.Done()
		defer cancel()
		defer func() { a.mu.Lock(); a.busy = false; a.cancel = nil; a.mu.Unlock(); a.event("devices:changed") }()
		a.send(ctx, d, unique)
	}()
	return nil
}

func (a *App) send(ctx context.Context, d model.Device, ids []string) {
	for i, id := range ids {
		b, err := a.store.GetBook(id)
		p := model.Progress{ID: d.ID, BookID: id, Title: b.Title, Current: i + 1, Total: len(ids)}
		progress := func(stage, message string) { p.Stage = stage; p.Message = message; a.event("transfer:progress", p) }
		fail := func(err error) {
			stage := "error"
			if ctx.Err() != nil {
				stage = "cancelled"
			}
			progress(stage, err.Error())
		}
		if err != nil {
			fail(err)
			return
		}
		if err = ctx.Err(); err != nil {
			fail(err)
			return
		}
		asset, source, err := transferAsset(a.store, b, d.TransferFormats)
		converted := false
		if err != nil && d.AllowEPUBToMOBI {
			asset, source, err = a.convertForKindle(ctx, b, progress)
			converted = err == nil
		}
		if err != nil {
			fail(err)
			return
		}
		if err = ctx.Err(); err != nil {
			fail(err)
			return
		}
		// Conversion assets are content-addressed and cached separately; existing
		// compatible files are transferred directly without changing the original.
		digest := asset.SHA256
		if converted {
			digest, err = fileHash(source)
		}
		if err != nil {
			fail(err)
			return
		}
		key := ""
		if converted {
			key = cacheKey(b, a.converterVersion)
		} else {
			keyBytes := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s", digest, b.Revision, d.ProfileID, asset.Format)))
			key = hex.EncodeToString(keyBytes[:])
		}
		filename := sanitizeDeviceTitle(b.Title) + "-" + b.ID + "-" + key[:12] + "." + asset.Format
		if relative, exists, findErr := a.findExistingTransfer(d, filename, digest, key); findErr != nil {
			fail(findErr)
			return
		} else if exists {
			if err = a.store.RecordTransfer(d.ID, b.ID, relative, key); err != nil {
				fail(err)
				return
			}
			progress("done", "Already on device")
			continue
		}
		transferMessage := "Copying and verifying"
		if d.Transport != "" && d.Transport != "usb-storage" {
			transferMessage = "Copying; checking device-reported size"
		}
		progress("transferring", transferMessage)
		var relative string
		if sender, ok := a.manager.(interface {
			SendWithProgress(context.Context, model.Device, string, string, device.ProgressFunc) (string, error)
		}); ok {
			var lastReported uint64
			relative, err = sender.SendWithProgress(ctx, d, source, filename, func(done, total uint64) {
				if done != total && done-lastReported < 1<<20 {
					return
				}
				lastReported = done
				progress("transferring", fmt.Sprintf("Copied %s of %s", humanBytes(done), humanBytes(total)))
			})
		} else {
			relative, err = a.manager.Send(ctx, d, source, filename)
		}
		if err != nil {
			fail(err)
			return
		}
		if err = a.store.RecordTransfer(d.ID, b.ID, relative, key); err != nil {
			fail(err)
			return
		}
		progress("done", "Sent to device")
	}
}

func humanBytes(value uint64) string {
	if value < 1<<20 {
		return fmt.Sprintf("%.0f KB", float64(value)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(value)/(1<<20))
}

func transferAsset(store *library.Store, book model.Book, preferredFormats []string) (model.BookFile, string, error) {
	formats := preferredFormats
	if len(formats) == 0 {
		formats = []string{"epub", "pdf", "azw3", "mobi", "prc", "fb2", "txt", "cbz", "cbr", "rtf", "doc", "docx", "odt", "html", "htmlz", "pdb", "lit", "zip"}
	}
	for _, format := range formats {
		for _, file := range book.Files {
			if strings.EqualFold(file.Format, format) {
				resolved, path, err := store.BookFileSource(book.ID, file.ID)
				if err != nil {
					return model.BookFile{}, "", err
				}
				return resolved, path, nil
			}
		}
	}
	if book.PreferredFileID == "" && len(book.Files) == 0 {
		return model.BookFile{}, "", fmt.Errorf("%q has no imported book files", book.Title)
	}
	return model.BookFile{}, "", fmt.Errorf("%q has no file compatible with this device; add one of the supported formats: %s", book.Title, strings.Join(formats, ", "))
}

func (a *App) convertForKindle(ctx context.Context, book model.Book, progress func(string, string)) (model.BookFile, string, error) {
	if a.converterErr != nil {
		return model.BookFile{}, "", fmt.Errorf("this Kindle accepts AZW3/MOBI/PRC, but only EPUB is available and the converter is unavailable: %w", a.converterErr)
	}
	var epubFile model.BookFile
	for _, file := range book.Files {
		if strings.EqualFold(file.Format, "epub") {
			epubFile = file
			break
		}
	}
	if epubFile.ID == "" {
		return model.BookFile{}, "", fmt.Errorf("this Kindle accepts AZW3/MOBI/PRC; add one of those formats or an EPUB to convert")
	}
	key := cacheKey(book, a.converterVersion)
	cached, err := a.store.GetConversion(key)
	if err != nil {
		return model.BookFile{}, "", err
	}
	expected := filepath.Join(a.root, "conversions", key+".mobi")
	if cached == expected {
		if st, statErr := os.Lstat(cached); statErr == nil && st.Mode().IsRegular() && st.Mode()&os.ModeSymlink == 0 && st.Size() > 0 {
			digest, hashErr := fileHash(cached)
			if hashErr != nil {
				return model.BookFile{}, "", hashErr
			}
			return model.BookFile{Format: "mobi", SHA256: digest}, cached, nil
		}
	}
	progress("converting", "Converting EPUB for Kindle")
	dir := filepath.Join(a.root, "conversions")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return model.BookFile{}, "", err
	}
	output := expected
	if st, statErr := os.Lstat(output); statErr == nil {
		if !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
			return model.BookFile{}, "", errors.New("conversion cache path is not a regular file")
		}
		if err = os.Remove(output); err != nil {
			return model.BookFile{}, "", err
		}
	} else if !os.IsNotExist(statErr) {
		return model.BookFile{}, "", statErr
	}
	_, epubPath, err := a.store.BookFileSource(book.ID, epubFile.ID)
	if err != nil {
		return model.BookFile{}, "", err
	}
	if err = a.converter.Convert(ctx, epubPath, output, book.Title, book.Authors); err != nil {
		return model.BookFile{}, "", err
	}
	if err = a.store.SaveConversion(book.ID, key, output); err != nil {
		return model.BookFile{}, "", err
	}
	digest, err := fileHash(output)
	return model.BookFile{Format: "mobi", SHA256: digest}, output, err
}

func (a *App) findExistingTransfer(d model.Device, filename, digest, transferKey string) (string, bool, error) {
	if d.Transport != "" && d.Transport != "usb-storage" {
		books, err := a.manager.List(d)
		if err != nil {
			return "", false, err
		}
		for _, book := range books {
			if book.Name != filename {
				continue
			}
			recordedKey, err := a.store.TransferKey(d.ID, book.Path)
			if err != nil {
				return "", false, err
			}
			if recordedKey == transferKey {
				return book.Path, true, nil
			}
			return "", false, fmt.Errorf("a different device file already uses %q; remove it before retrying", filename)
		}
		return "", false, nil
	}
	for _, folder := range d.BookFolders {
		p := filepath.Join(d.Root, folder, filename)
		st, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", false, err
		}
		if !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
			continue
		}
		actual, err := fileHash(p)
		if err != nil {
			return "", false, err
		}
		if actual == digest {
			return filepath.ToSlash(filepath.Join(folder, filename)), true, nil
		}
	}
	books, err := a.manager.List(d)
	if err != nil {
		return "", false, err
	}
	for _, book := range books {
		if book.Name != filename {
			continue
		}
		path := filepath.Join(d.Root, filepath.FromSlash(book.Path))
		actual, hashErr := fileHash(path)
		if hashErr != nil {
			return "", false, hashErr
		}
		if actual == digest {
			return book.Path, true, nil
		}
	}
	return "", false, nil
}

func sanitizeDeviceTitle(title string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range title {
		if b.Len() >= 80 {
			break
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteByte(byte(r))
			lastDash = false
		} else if b.Len() > 0 && !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	clean := strings.Trim(b.String(), "-")
	if clean == "" {
		return "Book"
	}
	return clean
}

func (a *App) CancelTransfer() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *App) RemoveDeviceBooks(id string, paths []string) error {
	if err := a.ready(); err != nil {
		return err
	}
	d, err := a.findDevice(id)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return errors.New("application is shutting down")
	}
	if a.busy {
		return errors.New("wait for the transfer to finish before removing books")
	}
	for _, p := range paths {
		if err = a.manager.Remove(d, p); err != nil {
			return err
		}
		if err = a.store.ForgetTransfer(d.ID, p); err != nil {
			return err
		}
	}
	a.event("devices:changed")
	return nil
}

func (a *App) EjectDevice(id string) error {
	d, err := a.findDevice(id)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return errors.New("application is shutting down")
	}
	if a.busy {
		return errors.New("wait for the transfer to finish before ejecting")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	if err = a.manager.Eject(ctx, d); err != nil {
		return err
	}
	delete(a.selected, id)
	a.event("devices:changed")
	return nil
}

func (a *App) OpenLibraryFolder() error {
	if err := a.ready(); err != nil {
		return err
	}
	tool := "xdg-open"
	if goruntime.GOOS == "darwin" {
		tool = "open"
	} else if goruntime.GOOS == "windows" {
		tool = "explorer"
	}
	a.mu.Lock()
	root := a.root
	a.mu.Unlock()
	return exec.Command(tool, root).Run()
}

// ChangeLibraryLocation asks for a parent folder and moves a validated copy of
// the current library into its Attune Library subfolder.
func (a *App) ChangeLibraryLocation() (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	parent, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Choose where to store your library"})
	if err != nil || parent == "" {
		return "", err
	}
	destination := filepath.Join(parent, "Attune Library")
	return a.relocateLibrary(destination)
}

func (a *App) ServeCovers(w http.ResponseWriter, r *http.Request) {
	const maxCoverBytes = 32 << 20
	if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/covers/") {
		http.NotFound(w, r)
		return
	}
	if a.ready() != nil {
		http.Error(w, "Library unavailable", 503)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/covers/")
	if strings.ContainsAny(id, "/\\.") {
		http.NotFound(w, r)
		return
	}
	a.mu.Lock()
	if a.closing {
		a.mu.Unlock()
		http.Error(w, "Library unavailable", 503)
		return
	}
	p, err := a.store.CoverPath(id)
	a.mu.Unlock()
	if err != nil || p == "" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, maxCoverBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxCoverBytes {
		http.NotFound(w, r)
		return
	}
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	mime := http.DetectContentType(head)
	if mime != "image/jpeg" && mime != "image/png" && mime != "image/gif" && mime != "image/webp" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}

func (a *App) ServePreviewResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/preview/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	id, err := url.PathUnescape(parts[0])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	resourceParts := make([]string, 0, len(parts)-1)
	for _, part := range parts[1:] {
		p, e := url.PathUnescape(part)
		if e != nil || p == "" || strings.ContainsAny(p, "/\\") {
			http.NotFound(w, r)
			return
		}
		resourceParts = append(resourceParts, p)
	}
	resource := strings.Join(resourceParts, "/")
	if a.ready() != nil {
		http.Error(w, "Library unavailable", 503)
		return
	}
	a.mu.Lock()
	if a.closing {
		a.mu.Unlock()
		http.Error(w, "Library unavailable", 503)
		return
	}
	_, source, err := a.store.BookSource(id)
	a.mu.Unlock()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	body, contentType, err := epub.Resource(source, resource, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self' data:; media-src 'self'; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}
