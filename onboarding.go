package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"attune/internal/model"
)

const (
	calibreScanMaxEntries = 6000
	calibreScanMaxDepth   = 5
	calibreScanMaxTime    = 3 * time.Second
)

type calibreScanLimits struct {
	maxEntries int
	maxDepth   int
	maxTime    time.Duration
}

func defaultCalibreLocations(home string) []model.CalibreLibraryLocation {
	paths := []string{
		filepath.Join(home, "Calibre Library"),
		filepath.Join(home, "Documents", "Calibre Library"),
		filepath.Join(home, "Books", "Calibre Library"),
		filepath.Join(home, "OneDrive", "Documents", "Calibre Library"),
	}
	seen := map[string]bool{}
	locations := make([]model.CalibreLibraryLocation, 0, len(paths))
	for _, path := range paths {
		location, ok := calibreLocation(path)
		if !ok || seen[location.Path] {
			continue
		}
		seen[location.Path] = true
		locations = append(locations, location)
	}
	return locations
}

// FindCalibreLibraries checks a few familiar locations by default. Passing
// scan=true searches bounded folders under the user's home directory.
func (a *App) FindCalibreLibraries(scan bool) ([]model.CalibreLibraryLocation, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return findCalibreLibraries(home, scan, calibreScanLimits{
		maxEntries: calibreScanMaxEntries,
		maxDepth:   calibreScanMaxDepth,
		maxTime:    calibreScanMaxTime,
	})
}

func findCalibreLibraries(home string, scan bool, limits calibreScanLimits) ([]model.CalibreLibraryLocation, error) {
	locations := defaultCalibreLocations(home)
	if !scan {
		return locations, nil
	}
	seen := make(map[string]bool, len(locations))
	for _, location := range locations {
		seen[location.Path] = true
	}
	started := time.Now()
	timeExpired := func() bool { return limits.maxTime > 0 && time.Since(started) >= limits.maxTime }
	entries := 0
	documents := filepath.Join(home, "Documents")
	desktop := filepath.Join(home, "Desktop")
	downloads := filepath.Join(home, "Downloads")
	roots := []string{documents, desktop, downloads}
	if runtime.GOOS == "windows" {
		roots = append(roots, filepath.Join(home, "OneDrive", "Documents"))
	}
	roots = append(roots, home)
	priorityRoots := map[string]bool{filepath.Clean(documents): true, filepath.Clean(desktop): true, filepath.Clean(downloads): true}
	if runtime.GOOS == "windows" {
		priorityRoots[filepath.Clean(filepath.Join(home, "OneDrive", "Documents"))] = true
	}
	for _, root := range roots {
		if len(locations) >= 100 || entries >= limits.maxEntries || timeExpired() {
			break
		}
		rootInfo, err := os.Lstat(root)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			continue
		}
		if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		rootDepth := strings.Count(filepath.Clean(root), string(filepath.Separator)) - strings.Count(filepath.Clean(home), string(filepath.Separator))
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			entries++
			if entries > limits.maxEntries || timeExpired() || len(locations) >= 100 {
				return filepath.SkipAll
			}
			if path == root {
				return nil
			}
			depth := rootDepth + strings.Count(filepath.Clean(path), string(filepath.Separator)) - strings.Count(filepath.Clean(root), string(filepath.Separator))
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if path != root && priorityRoots[filepath.Clean(path)] {
					return filepath.SkipDir
				}
				name := entry.Name()
				if strings.HasPrefix(name, ".") || ignoredCalibreScanDirectory(name) {
					return filepath.SkipDir
				}
				if depth > limits.maxDepth {
					return filepath.SkipDir
				}
				if hasCalibreDatabase(path) {
					if location, ok := calibreLocation(path); ok && !seen[location.Path] {
						locations = append(locations, location)
						seen[location.Path] = true
					}
					return filepath.SkipDir
				}
			}
			return nil
		})
		if err != nil && !errors.Is(err, filepath.SkipAll) {
			continue
		}
	}
	return locations, nil
}

func ignoredCalibreScanDirectory(name string) bool {
	switch strings.ToLower(name) {
	case "library", "node_modules", "build", "dist", "target", "cache", "caches", "appdata":
		return true
	default:
		return false
	}
}

func hasCalibreDatabase(root string) bool {
	info, err := os.Lstat(filepath.Join(root, "metadata.db"))
	return err == nil && info.Mode().IsRegular()
}

func calibreLocation(path string) (model.CalibreLibraryLocation, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !hasCalibreDatabase(path) {
		return model.CalibreLibraryLocation{}, false
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return model.CalibreLibraryLocation{}, false
	}
	return model.CalibreLibraryLocation{Path: filepath.Clean(absolute), Name: filepath.Base(filepath.Clean(path))}, true
}

// GetOnboardingState returns default discovery results without walking the
// user's folders. Existing libraries bypass onboarding and are marked done.
func (a *App) GetOnboardingState() (model.OnboardingState, error) {
	if err := a.ready(); err != nil {
		return model.OnboardingState{}, err
	}
	a.mu.Lock()
	if a.closing {
		a.mu.Unlock()
		return model.OnboardingState{}, errors.New("application is shutting down")
	}
	settings, err := readLibrarySettings(a.configPath)
	if err != nil {
		a.mu.Unlock()
		return model.OnboardingState{}, err
	}
	books, err := a.store.ListBooks()
	if err != nil {
		a.mu.Unlock()
		return model.OnboardingState{}, err
	}
	if len(books) > 0 && !settings.OnboardingComplete {
		// Legacy libraries must not be interrupted by a settings-folder write
		// failure. They still bypass onboarding for this session.
		if strings.TrimSpace(settings.LibraryPath) == "" {
			settings.LibraryPath = a.root
		}
		settings.OnboardingComplete = true
		_ = saveLibrarySettings(a.configPath, settings)
	}
	a.mu.Unlock()
	if settings.OnboardingComplete || len(books) > 0 {
		return model.OnboardingState{Required: false, Libraries: []model.CalibreLibraryLocation{}}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return model.OnboardingState{}, err
	}
	libraries, err := findCalibreLibraries(home, false, calibreScanLimits{})
	if err != nil {
		return model.OnboardingState{}, err
	}
	return model.OnboardingState{Required: !settings.OnboardingComplete, Libraries: libraries}, nil
}

// CompleteOnboarding records the user's explicit onboarding choice.
func (a *App) CompleteOnboarding() error {
	if err := a.ready(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return errors.New("application is shutting down")
	}
	settings, err := readLibrarySettings(a.configPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(settings.LibraryPath) == "" {
		settings.LibraryPath = a.root
	}
	settings.OnboardingComplete = true
	return saveLibrarySettings(a.configPath, settings)
}

// ImportCalibreLibraryAt imports from a selected discovered location. The UI
// records the user's onboarding decision separately after a successful import.
func (a *App) ImportCalibreLibraryAt(path string) (model.ImportResult, error) {
	return a.importCalibreLibrary(path)
}
