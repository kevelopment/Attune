package app

import (
	"errors"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"attune/internal/library"
	"attune/internal/model"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ServeBookFile streams one managed format asset to the local reader UI. IDs
// are resolved through the library index; request paths are never used as file
// system paths. Content is downloadable and scripts remain disabled.
func (a *App) ServeBookFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/book-files/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	bookID, err := url.PathUnescape(parts[0])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fileID, err := url.PathUnescape(parts[1])
	if err != nil || bookID == "" || fileID == "" || strings.ContainsAny(bookID+fileID, "/\\") {
		http.NotFound(w, r)
		return
	}
	if err = a.ready(); err != nil {
		http.Error(w, "Library unavailable", http.StatusServiceUnavailable)
		return
	}
	a.mu.Lock()
	if a.closing {
		a.mu.Unlock()
		http.Error(w, "Library unavailable", http.StatusServiceUnavailable)
		return
	}
	_, path, err := a.store.BookFileSource(bookID, fileID)
	a.mu.Unlock()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(filepath.Ext(info.Name()))
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(info.Name()))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'none'; object-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'none'")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

// AttachBookFiles adds one or more format assets to an existing library book.
func (a *App) AttachBookFiles(bookID string) (model.ImportResult, error) {
	paths, err := wruntime.OpenMultipleFilesDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:   "Add book files",
		Filters: []wruntime.FileFilter{{DisplayName: "Supported ebooks", Pattern: "*.epub;*.pdf;*.mobi;*.azw;*.azw3;*.prc;*.fb2;*.txt;*.cbz;*.cbr;*.rtf;*.doc;*.docx;*.odt;*.html;*.htmlz;*.pdb;*.lit;*.zip"}},
	})
	if err != nil || len(paths) == 0 {
		return model.ImportResult{BookIDs: []string{bookID}, Errors: []string{}}, err
	}
	return a.attachBookFiles(bookID, paths)
}

func (a *App) attachBookFiles(bookID string, paths []string) (model.ImportResult, error) {
	result := model.ImportResult{BookIDs: []string{bookID}, Errors: []string{}}
	if err := a.ready(); err != nil {
		return result, err
	}
	if len(paths) > 500 {
		return result, errors.New("add up to 500 files at a time")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return result, errors.New("application is shutting down")
	}
	if a.busy || a.activeOps.Load() != 0 {
		return result, errors.New("wait for the current library operation to finish before adding files")
	}
	a.activeOps.Add(1)
	defer a.activeOps.Add(-1)
	for _, path := range paths {
		file, duplicate, attachErr := a.store.AttachBookFile(bookID, path, false)
		if attachErr != nil {
			result.Errors = append(result.Errors, filepath.Base(path)+": "+attachErr.Error())
			continue
		}
		if duplicate {
			if file.BookID != bookID {
				result.Errors = append(result.Errors, filepath.Base(path)+": identical content is already attached to another book")
			} else {
				result.Duplicates++
			}
			continue
		}
		result.Imported++
	}
	a.event("library:changed")
	return result, nil
}

// OpenBookFile opens an imported asset with the operating system's default
// application. The library resolves and validates the managed path first.
func (a *App) OpenBookFile(bookID, fileID string) error {
	if err := a.ready(); err != nil {
		return err
	}
	_, path, err := a.store.BookFileSource(bookID, fileID)
	if err != nil {
		if errors.Is(err, library.ErrNotFound) {
			return errors.New("book file was not found")
		}
		return err
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", path)
	case "windows":
		command = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path)
	default:
		command = exec.Command("xdg-open", path)
	}
	if err = command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}
