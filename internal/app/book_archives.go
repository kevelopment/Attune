package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"attune/internal/model"
	rardecode "github.com/nwaples/rardecode/v2"
)

const maxComicPageBytes = 64 << 20
const maxComicPageCount = 10000

type comicManifest struct {
	Pages []comicPage `json:"pages"`
}

type comicPage struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
}

type comicEntry struct {
	name string
	size int64
}

// ServeBookArchive exposes a lazy page manifest and individual image pages
// from a managed CBZ/CBR asset. Archive member paths are treated as opaque
// names and are never extracted to disk.
func (a *App) ServeBookArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/book-archives/"), "/")
	if len(parts) < 3 || len(parts) > 4 {
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
	asset, source, err := a.archiveSource(bookID, fileID)
	if err != nil || (asset.Format != "cbz" && asset.Format != "cbr") {
		http.NotFound(w, r)
		return
	}
	if parts[2] == "manifest" && len(parts) == 3 {
		entries, listErr := comicEntries(source, asset.Format)
		if listErr != nil {
			http.Error(w, "Could not read comic archive", http.StatusUnprocessableEntity)
			return
		}
		pages := make([]comicPage, len(entries))
		for i, entry := range entries {
			pages[i] = comicPage{URL: fmt.Sprintf("/book-archives/%s/%s/pages/%d", url.PathEscape(bookID), url.PathEscape(fileID), i), Filename: path.Base(entry.name)}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'none'; object-src 'none'; base-uri 'none'")
		_ = json.NewEncoder(w).Encode(comicManifest{Pages: pages})
		return
	}
	if parts[2] != "pages" || len(parts) != 4 {
		http.NotFound(w, r)
		return
	}
	index, parseErr := strconv.Atoi(parts[3])
	if parseErr != nil || index < 0 || index >= maxComicPageCount {
		http.NotFound(w, r)
		return
	}
	entries, err := comicEntries(source, asset.Format)
	if err != nil {
		http.Error(w, "Could not read comic archive", http.StatusUnprocessableEntity)
		return
	}
	if index >= len(entries) {
		http.NotFound(w, r)
		return
	}
	data, err := readComicPage(source, asset.Format, entries[index])
	if err != nil {
		http.Error(w, "Could not read comic page", http.StatusUnprocessableEntity)
		return
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(entries[index].name)))
	if contentType == "" || !strings.HasPrefix(contentType, "image/") {
		http.Error(w, "Unsupported comic image", http.StatusUnsupportedMediaType)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(path.Base(entries[index].name)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'none'; object-src 'none'; base-uri 'none'")
	http.ServeContent(w, r, path.Base(entries[index].name), time.Time{}, bytes.NewReader(data))
}

func (a *App) archiveSource(bookID, fileID string) (model.BookFile, string, error) {
	if err := a.ready(); err != nil {
		return model.BookFile{}, "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return model.BookFile{}, "", errors.New("application is shutting down")
	}
	asset, source, err := a.store.BookFileSource(bookID, fileID)
	return asset, source, err
}

func comicEntries(source, format string) ([]comicEntry, error) {
	var entries []comicEntry
	if format == "cbz" {
		archive, err := zip.OpenReader(source)
		if err != nil {
			return nil, err
		}
		defer archive.Close()
		for _, file := range archive.File {
			if isComicImage(file.Name) && file.UncompressedSize64 > 0 && file.UncompressedSize64 <= maxComicPageBytes {
				entries = append(entries, comicEntry{name: file.Name, size: int64(file.UncompressedSize64)})
			}
		}
	} else if format == "cbr" {
		files, err := rardecode.List(source, rardecode.MaxReadFileSize(maxComicPageBytes))
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if !file.IsDir && !file.Encrypted && !file.UnKnownSize && file.UnPackedSize > 0 && file.UnPackedSize <= maxComicPageBytes && isComicImage(file.Name) {
				entries = append(entries, comicEntry{name: file.Name, size: file.UnPackedSize})
			}
		}
	} else {
		return nil, errors.New("unsupported archive format")
	}
	sort.SliceStable(entries, func(i, j int) bool { return naturalLess(entries[i].name, entries[j].name) })
	if len(entries) == 0 || len(entries) > maxComicPageCount {
		return nil, errors.New("comic archive has no supported pages or exceeds page limit")
	}
	return entries, nil
}

func readComicPage(source, format string, entry comicEntry) ([]byte, error) {
	if entry.size <= 0 || entry.size > maxComicPageBytes || !isComicImage(entry.name) {
		return nil, errors.New("invalid comic page")
	}
	if format == "cbz" {
		archive, err := zip.OpenReader(source)
		if err != nil {
			return nil, err
		}
		defer archive.Close()
		for _, file := range archive.File {
			if file.Name != entry.name || file.UncompressedSize64 > maxComicPageBytes {
				continue
			}
			reader, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer reader.Close()
			data, err := io.ReadAll(io.LimitReader(reader, maxComicPageBytes+1))
			if err != nil {
				return nil, err
			}
			if len(data) > maxComicPageBytes {
				return nil, errors.New("comic page exceeds size limit")
			}
			return data, nil
		}
		return nil, errors.New("comic page not found")
	}
	if format == "cbr" {
		archive, err := rardecode.OpenFS(source, rardecode.MaxReadFileSize(maxComicPageBytes))
		if err != nil {
			return nil, err
		}
		data, err := archive.ReadFile(entry.name)
		if err != nil {
			return nil, err
		}
		return data, nil
	}
	return nil, errors.New("unsupported archive format")
}

func isComicImage(name string) bool {
	clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return false
	}
	switch strings.ToLower(path.Ext(clean)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".bmp":
		return true
	default:
		return false
	}
}

func naturalLess(a, b string) bool {
	left, right := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	for i, j := 0, 0; i < len(left) && j < len(right); {
		if left[i] >= '0' && left[i] <= '9' && right[j] >= '0' && right[j] <= '9' {
			iEnd, jEnd := i, j
			for iEnd < len(left) && left[iEnd] >= '0' && left[iEnd] <= '9' {
				iEnd++
			}
			for jEnd < len(right) && right[jEnd] >= '0' && right[jEnd] <= '9' {
				jEnd++
			}
			li, _ := strconv.ParseUint(string(left[i:iEnd]), 10, 64)
			rj, _ := strconv.ParseUint(string(right[j:jEnd]), 10, 64)
			if li != rj {
				return li < rj
			}
			if iEnd-i != jEnd-j {
				return iEnd-i < jEnd-j
			}
			i, j = iEnd, jEnd
			continue
		}
		if left[i] != right[j] {
			return left[i] < right[j]
		}
		i++
		j++
	}
	return len(left) < len(right)
}
