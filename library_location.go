package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"attune/internal/library"
)

type librarySettings struct {
	LibraryPath        string `json:"libraryPath,omitempty"`
	OnboardingComplete bool   `json:"onboardingComplete,omitempty"`
}

func librarySettingsPath(envRoot string) (string, error) {
	if envRoot != "" {
		return envRoot + ".settings.json", nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Attune", "settings.json"), nil
}

func loadLibraryLocation(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var settings librarySettings
	if err = json.Unmarshal(data, &settings); err != nil {
		return "", false, fmt.Errorf("read library settings: %w", err)
	}
	if strings.TrimSpace(settings.LibraryPath) == "" {
		return "", false, errors.New("library settings contain an empty library path")
	}
	path, err = filepath.Abs(settings.LibraryPath)
	return path, true, err
}

func saveLibraryLocation(path, libraryPath string) error {
	if path == "" {
		return errors.New("library settings path is unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	settings, err := readLibrarySettings(path)
	if err != nil {
		return err
	}
	settings.LibraryPath = libraryPath
	return saveLibrarySettings(path, settings)
}

func readLibrarySettings(path string) (librarySettings, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return librarySettings{}, nil
	}
	if err != nil {
		return librarySettings{}, err
	}
	var settings librarySettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return librarySettings{}, fmt.Errorf("read library settings: %w", err)
	}
	return settings, nil
}

func saveLibrarySettings(path string, settings librarySettings) error {
	if path == "" {
		return errors.New("library settings path is unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
}

func pathsOverlap(a, b string) bool {
	rel, err := filepath.Rel(a, b)
	if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
		return true
	}
	rel, err = filepath.Rel(b, a)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func (a *App) relocateLibrary(destination string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return "", errors.New("application is shutting down")
	}
	if a.busy {
		return "", errors.New("wait for the transfer to finish before moving the library")
	}
	a.activeOps.Add(1)
	defer a.activeOps.Add(-1)
	if a.configPath == "" {
		return "", errors.New("library settings path is unavailable")
	}
	if a.store == nil {
		return "", errors.New("library is starting; please try again")
	}
	destination, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil {
		return "", fmt.Errorf("resolve destination folder: %w", err)
	}
	destination = filepath.Join(parent, filepath.Base(destination))
	current, err := canonicalPath(a.root)
	if err != nil {
		return "", err
	}
	if pathsOverlap(current, destination) {
		return "", errors.New("library location cannot be inside the current library")
	}
	destinationExisted := false
	var destinationInfo os.FileInfo
	if st, err := os.Lstat(destination); err == nil {
		destinationExisted = true
		destinationInfo = st
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return "", errors.New("destination must be a new or empty folder")
		}
		entries, readErr := os.ReadDir(destination)
		if readErr != nil {
			return "", readErr
		}
		if len(entries) != 0 {
			return "", errors.New("destination folder is not empty")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	stage, err := os.MkdirTemp(parent, ".attune-library-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	if err = a.store.CopyTo(stage); err != nil {
		return "", fmt.Errorf("copy library: %w", err)
	}
	check, err := library.Open(stage)
	if err != nil {
		return "", fmt.Errorf("validate copied library: %w", err)
	}
	books, err := check.ListBooks()
	if err != nil {
		_ = check.Close()
		return "", fmt.Errorf("validate copied library: %w", err)
	}
	for _, book := range books {
		if _, _, err = check.BookSource(book.ID); err != nil {
			_ = check.Close()
			return "", fmt.Errorf("validate copied library: %w", err)
		}
	}
	if err = check.Close(); err != nil {
		return "", err
	}
	if destinationExisted {
		currentInfo, statErr := os.Lstat(destination)
		if statErr != nil || !os.SameFile(destinationInfo, currentInfo) || currentInfo.Mode()&os.ModeSymlink != 0 || !currentInfo.IsDir() {
			return "", errors.New("destination folder changed during library copy")
		}
		entries, readErr := os.ReadDir(destination)
		if readErr != nil {
			return "", readErr
		}
		if len(entries) != 0 {
			return "", errors.New("destination folder changed during library copy")
		}
		if err = os.Remove(destination); err != nil {
			return "", err
		}
	} else if _, statErr := os.Lstat(destination); statErr == nil {
		return "", errors.New("destination folder appeared during library copy")
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return "", statErr
	}
	if err = os.Rename(stage, destination); err != nil {
		if destinationExisted {
			_ = os.Mkdir(destination, 0o700)
		}
		return "", err
	}
	newStore, err := library.Open(destination)
	if err != nil {
		_ = os.RemoveAll(destination)
		if destinationExisted {
			_ = os.Mkdir(destination, 0o700)
		}
		return "", fmt.Errorf("open relocated library: %w", err)
	}
	if err = saveLibraryLocation(a.configPath, destination); err != nil {
		_ = newStore.Close()
		_ = os.RemoveAll(destination)
		if destinationExisted {
			_ = os.Mkdir(destination, 0o700)
		}
		return "", fmt.Errorf("save library location: %w", err)
	}
	oldStore := a.store
	a.store = newStore
	a.root = destination
	if err = oldStore.Close(); err != nil {
		return destination, fmt.Errorf("library moved, but closing previous library failed: %w", err)
	}
	a.event("library:changed")
	return destination, nil
}
