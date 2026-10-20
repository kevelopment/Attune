package app

import (
	"os"
	"path/filepath"
	"testing"
)

func makeDiscoveredCalibre(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "metadata.db"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFindCalibreLibrariesDefaultsAndBoundedScan(t *testing.T) {
	home := t.TempDir()
	defaultPath := filepath.Join(home, "Calibre Library")
	makeDiscoveredCalibre(t, defaultPath)
	nestedPath := filepath.Join(home, "Documents", "Reading", "Calibre Library")
	makeDiscoveredCalibre(t, nestedPath)
	hiddenPath := filepath.Join(home, ".hidden", "Calibre Library")
	makeDiscoveredCalibre(t, hiddenPath)
	tooDeepPath := filepath.Join(home, "Documents", "one", "two", "three", "four", "Calibre Library")
	makeDiscoveredCalibre(t, tooDeepPath)
	externalHome := t.TempDir()
	externalPath := filepath.Join(externalHome, "Calibre Library")
	makeDiscoveredCalibre(t, externalPath)
	if err := os.Symlink(externalHome, filepath.Join(home, "linked")); err != nil {
		t.Fatal(err)
	}

	defaults, err := findCalibreLibraries(home, false, calibreScanLimits{})
	if err != nil || len(defaults) != 1 || defaults[0].Path != defaultPath {
		t.Fatalf("default discovery = %#v, %v", defaults, err)
	}
	scanned, err := findCalibreLibraries(home, true, calibreScanLimits{maxEntries: 1000, maxDepth: 3})
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, location := range scanned {
		found[location.Path] = true
	}
	if !found[defaultPath] || !found[nestedPath] {
		t.Fatalf("scan missed in-scope libraries: %#v", scanned)
	}
	if found[hiddenPath] || found[tooDeepPath] || found[externalPath] {
		t.Fatalf("scan included excluded locations: %#v", scanned)
	}

	limited, err := findCalibreLibraries(home, true, calibreScanLimits{maxEntries: 1, maxDepth: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 {
		t.Fatalf("entry-limited scan returned %#v", limited)
	}
}

func TestOnboardingSettingsPreserveLibraryPathAndCompleteOnExplicitChoice(t *testing.T) {
	a := testApp(t)
	defer a.store.Close()
	a.configPath = filepath.Join(t.TempDir(), "settings", "settings.json")
	state, err := a.GetOnboardingState()
	if err != nil || !state.Required {
		t.Fatalf("initial onboarding state = %#v, %v", state, err)
	}
	if err := a.CompleteOnboarding(); err != nil {
		t.Fatal(err)
	}
	settings, err := readLibrarySettings(a.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.OnboardingComplete || settings.LibraryPath != a.root {
		t.Fatalf("completed settings = %#v", settings)
	}
	if err := saveLibraryLocation(a.configPath, filepath.Join(t.TempDir(), "moved-library")); err != nil {
		t.Fatal(err)
	}
	settings, err = readLibrarySettings(a.configPath)
	if err != nil || !settings.OnboardingComplete {
		t.Fatalf("library relocation lost onboarding completion: %#v, %v", settings, err)
	}
	state, err = a.GetOnboardingState()
	if err != nil || state.Required {
		t.Fatalf("completed onboarding state = %#v, %v", state, err)
	}
}

func TestImportCalibreLibraryAtCompletesOnlyAfterSuccessfulRead(t *testing.T) {
	a := testApp(t)
	defer a.store.Close()
	a.configPath = filepath.Join(t.TempDir(), "settings", "settings.json")
	if _, err := a.ImportCalibreLibraryAt(filepath.Join(t.TempDir(), "not-a-library")); err == nil {
		t.Fatal("invalid Calibre location unexpectedly imported")
	}
	settings, err := readLibrarySettings(a.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if settings.OnboardingComplete {
		t.Fatal("failed import completed onboarding")
	}
	root, _ := makeImportCalibreLibrary(t)
	result, err := a.ImportCalibreLibraryAt(root)
	if err != nil || result.Imported != 2 || len(result.BookIDs) != 2 {
		t.Fatalf("Calibre import = %#v, %v", result, err)
	}
	settings, err = readLibrarySettings(a.configPath)
	if err != nil || settings.OnboardingComplete {
		t.Fatalf("import unexpectedly completed onboarding: %#v, %v", settings, err)
	}
	if err := a.CompleteOnboarding(); err != nil {
		t.Fatal(err)
	}
	settings, err = readLibrarySettings(a.configPath)
	if err != nil || !settings.OnboardingComplete || settings.LibraryPath != a.root {
		t.Fatalf("explicit completion settings = %#v, %v", settings, err)
	}
}

func TestGetOnboardingStateLegacyBooksAutoComplete(t *testing.T) {
	a := testApp(t)
	defer a.store.Close()
	a.configPath = filepath.Join(t.TempDir(), "settings", "settings.json")
	bookPath := filepath.Join(t.TempDir(), "existing.epub")
	writeTestEPUB(t, bookPath, "Existing")
	if result, err := a.ImportPaths([]string{bookPath}); err != nil || result.Imported != 1 {
		t.Fatalf("seed book import = %#v, %v", result, err)
	}
	state, err := a.GetOnboardingState()
	if err != nil || state.Required {
		t.Fatalf("legacy onboarding state = %#v, %v", state, err)
	}
	settings, err := readLibrarySettings(a.configPath)
	if err != nil || !settings.OnboardingComplete {
		t.Fatalf("legacy completion was not persisted: %#v, %v", settings, err)
	}
}
