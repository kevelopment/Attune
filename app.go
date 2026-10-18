package main

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
	"time"

	"attune/internal/converter"
	"attune/internal/device"
	"attune/internal/epub"
	"attune/internal/library"
	"attune/internal/model"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx              context.Context
	store            *library.Store
	manager          deviceManager
	converter        converterRunner
	root             string
	configPath       string
	initErr          error
	converterVersion string
	converterErr     error
	mu               sync.Mutex
	selected         map[string]model.Device
	busy             bool
	closing          bool
	cancel           context.CancelFunc
	jobs             sync.WaitGroup
	emit             func(string, ...interface{})
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

func NewApp() *App { return &App{selected: make(map[string]model.Device)} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	followSystemAppIcon()
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
	a.converter = converter.New(converterPath())
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	a.converterVersion, a.converterErr = a.converter.Version(checkCtx)
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

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	a.closing = true
	if a.cancel != nil {
		a.cancel()
	}
	a.mu.Unlock()
	a.jobs.Wait()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store != nil {
		_ = a.store.Close()
	}
}

func (a *App) beforeClose(ctx context.Context) bool {
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
	for _, d := range devices {
		if err := a.store.RecordDevice(d); err != nil {
			return model.AppState{}, err
		}
	}
	if books == nil {
		books = []model.Book{}
	}
	return model.AppState{Books: books, Devices: devices, ConverterReady: a.converterErr == nil, ConverterVersion: a.converterVersion, LibraryPath: a.root}, nil
}

func (a *App) ImportBooks() (model.ImportResult, error) {
	paths, err := wruntime.OpenMultipleFilesDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Import EPUB books", Filters: []wruntime.FileFilter{{DisplayName: "EPUB books", Pattern: "*.epub"}}})
	if err != nil {
		return model.ImportResult{}, err
	}
	return a.ImportPaths(paths)
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
	if len(paths) > 500 {
		emitEarlyError()
		return result, errors.New("import up to 500 books at a time")
	}
	failed := 0
	seenBookIDs := make(map[string]struct{}, len(paths))
	for i, p := range paths {
		filename := filepath.Base(p)
		emitProgress(model.ImportProgress{Current: i, Total: total, Filename: filename, Imported: result.Imported, Duplicates: result.Duplicates, Failed: failed, Stage: "importing"})
		book, duplicate, err := a.store.ImportEPUB(p)
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
	if goruntime.GOOS != "darwin" {
		return model.Device{}, fmt.Errorf("Kindle USB support is available on macOS only: %w", errors.ErrUnsupported)
	}
	if err := a.ready(); err != nil {
		return model.Device{}, err
	}
	root, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Choose the mounted Kindle volume", DefaultDirectory: "/Volumes"})
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
	return model.Device{}, errors.New("Kindle disconnected; reconnect it and try again")
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
	if a.converterErr != nil {
		return fmt.Errorf("Kindling unavailable: %w", a.converterErr)
	}
	if len(ids) == 0 {
		return errors.New("select at least one book")
	}
	d, err := a.findDevice(deviceID)
	if err != nil {
		return err
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
			if _, _, err := a.store.BookSource(id); err != nil {
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
		b, source, err := a.store.BookSource(id)
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
		key := cacheKey(b, a.converterVersion)
		cached, err := a.store.GetConversion(key)
		if err != nil {
			fail(err)
			return
		}
		expectedCache := filepath.Join(a.root, "conversions", key+".mobi")
		if cached != expectedCache {
			cached = ""
		}
		if cached != "" {
			if st, e := os.Lstat(cached); e != nil || !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 || st.Size() == 0 {
				cached = ""
			}
		}
		if cached == "" {
			progress("converting", "Converting for Kindle 1 / 2")
			dir := filepath.Join(a.root, "conversions")
			if err = os.MkdirAll(dir, 0700); err != nil {
				fail(err)
				return
			}
			output := filepath.Join(dir, key+".mobi")
			// A previous run may have published the validated converter output and
			// stopped before recording it in SQLite. Clear that managed orphan so
			// the converter can safely rebuild it on this run.
			if st, e := os.Lstat(output); e == nil {
				if !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
					fail(errors.New("conversion cache path is not a regular file"))
					return
				}
				if e = os.Remove(output); e != nil {
					fail(e)
					return
				}
			} else if !os.IsNotExist(e) {
				fail(e)
				return
			}
			if err = a.converter.Convert(ctx, source, output, b.Title, b.Authors); err != nil {
				fail(err)
				return
			}
			if err = a.store.SaveConversion(b.ID, key, output); err != nil {
				fail(err)
				return
			}
			cached = output
		}
		// A content hash in the filename distinguishes editions without replacing any existing book.
		digest, err := fileHash(cached)
		if err != nil {
			fail(err)
			return
		}
		filename := sanitizeDeviceTitle(b.Title) + "-" + b.ID + "-" + key[:12] + ".mobi"
		relative := filepath.Join("documents", filename)
		prior, err := a.store.TransferKey(d.ID, relative)
		if err != nil {
			fail(err)
			return
		}
		target := filepath.Join(d.Root, relative)
		if st, e := os.Lstat(target); e == nil {
			if st.Mode().IsRegular() && st.Mode()&os.ModeSymlink == 0 {
				if actual, hashErr := fileHash(target); hashErr == nil && actual == digest {
					// Recover a transfer completed just before a crash prevented its
					// device_books record from being written.
					if prior != key {
						if err = a.store.RecordTransfer(d.ID, b.ID, relative, key); err != nil {
							fail(err)
							return
						}
					}
					progress("done", "Already on Kindle")
					continue
				}
			}
		} else if !os.IsNotExist(e) {
			fail(e)
			return
		}
		progress("transferring", "Copying and verifying")
		relative, err = a.manager.Send(ctx, d, cached, filename)
		if err != nil {
			fail(err)
			return
		}
		if err = a.store.RecordTransfer(d.ID, b.ID, relative, key); err != nil {
			fail(err)
			return
		}
		progress("done", "Sent to Kindle")
	}
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

func (a *App) covers(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}

func (a *App) previewResource(w http.ResponseWriter, r *http.Request) {
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
