package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"attune/internal/updater"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var appVersion = "0.0.1"
var updateEnabled = "false"

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
	stagedPath     string
}

func (a *App) GetUpdateState() UpdateState {
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	if a.updateState.CurrentVersion == "" {
		a.updateState.CurrentVersion = appVersion
	}
	return a.updateState
}

func (a *App) CheckForUpdates() (UpdateState, error) {
	if !isDesktopBinary() {
		return a.GetUpdateState(), errors.New("Self-updates are disabled for isolated development runs. Use the installed app.")
	}
	a.updateMu.Lock()
	if a.updateState.Status == "checking" || a.updateState.Status == "downloading" || a.updateState.Status == "restarting" {
		state := a.updateState
		a.updateMu.Unlock()
		return state, nil
	}
	if a.updateState.Downloaded {
		state := a.updateState
		a.updateMu.Unlock()
		return state, nil
	}
	a.updateState.Status = "checking"
	a.updateState.Error = ""
	a.updateState.CurrentVersion = appVersion
	a.updateMu.Unlock()
	a.emitUpdateState()
	ctx, cancel, err := a.beginUpdateOperation(25 * time.Second)
	if err != nil {
		return a.setUpdateError(err)
	}
	defer a.endUpdateOperation(cancel)
	goos, goarch := updater.Platform()
	client := updater.NewClient()
	release, found, err := client.Check(ctx, appVersion, goos, goarch)
	if err != nil {
		return a.setUpdateError(err)
	}
	a.updateMu.Lock()
	if found {
		a.updateState.LatestVersion = release.Version.String()
		a.updateState.Available = true
		a.updateState.ReleaseURL = release.URL
		a.updateState.ReleaseNotes = release.Notes
		a.updateState.Status = "available"
	} else {
		a.updateState.LatestVersion = ""
		a.updateState.Available = false
		a.updateState.ReleaseURL = ""
		a.updateState.ReleaseNotes = ""
		a.updateState.Status = "idle"
	}
	state := a.updateState
	a.updateMu.Unlock()
	a.emitUpdateState()
	return state, nil
}

func (a *App) DownloadUpdate() (UpdateState, error) {
	if !isDesktopBinary() {
		return a.GetUpdateState(), errors.New("Self-updates are disabled for isolated development runs. Use the installed app.")
	}
	a.updateMu.Lock()
	if a.updateState.Downloaded {
		state := a.updateState
		a.updateMu.Unlock()
		return state, nil
	}
	if a.updateState.Status == "checking" || a.updateState.Status == "downloading" || a.updateState.Status == "restarting" {
		state := a.updateState
		a.updateMu.Unlock()
		return state, nil
	}
	if !a.updateState.Available {
		a.updateMu.Unlock()
		return a.GetUpdateState(), errors.New("no update is available")
	}
	version := a.updateState.LatestVersion
	releaseURL, err := url.Parse(a.updateState.ReleaseURL)
	if err != nil || releaseURL.Scheme != "https" || !strings.EqualFold(releaseURL.Hostname(), "github.com") {
		a.updateMu.Unlock()
		return a.setUpdateError(errors.New("release page URL is invalid"))
	}
	release := updater.Release{URL: a.updateState.ReleaseURL, Notes: a.updateState.ReleaseNotes, Assets: map[string]string{}}
	// The API is checked again to retrieve the asset URLs. This avoids persisting
	// stale download links across app sessions.
	a.updateState.Status = "downloading"
	a.updateState.Progress = 0
	a.updateState.Received = 0
	a.updateState.Total = 0
	a.updateState.Error = ""
	a.updateMu.Unlock()
	a.emitUpdateState()
	ctx, cancel, err := a.beginUpdateOperation(30 * time.Minute)
	if err != nil {
		return a.setUpdateError(err)
	}
	defer a.endUpdateOperation(cancel)
	client := updater.NewClient()
	goos, goarch := updater.Platform()
	fresh, found, err := client.Check(ctx, appVersion, goos, goarch)
	if err != nil {
		return a.setUpdateError(err)
	}
	if !found || fresh.Version.String() != version {
		return a.setUpdateError(errors.New("release changed; check for updates again"))
	}
	release = fresh
	target, err := installationTarget()
	if err != nil {
		return a.setUpdateError(err)
	}
	libraryPath := a.configuredLibraryPath()
	if err = updater.PreflightTarget(target, libraryPath, goos); err != nil {
		return a.setUpdateError(err)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return a.setUpdateError(err)
	}
	progressState := UpdateState{}
	client.Progress = func(received, total int64) {
		a.updateMu.Lock()
		now := time.Now()
		p := 0
		if total > 0 {
			p = int(received * 100 / total)
			if p > 100 {
				p = 100
			}
		}
		shouldEmit := p != a.updateState.Progress || now.Sub(a.updateLastProgress) >= 250*time.Millisecond || received == total
		a.updateState.Received = received
		a.updateState.Total = total
		a.updateState.Progress = p
		if shouldEmit {
			a.updateLastProgress = now
			progressState = a.updateState
		}
		a.updateMu.Unlock()
		if shouldEmit {
			a.event("update:state", progressState)
		}
	}
	goos, goarch = updater.Platform()
	staged, err := client.DownloadAndStage(ctx, release, goos, goarch, filepath.Join(cache, "Attune", "updates"))
	if err != nil {
		return a.setUpdateError(err)
	}
	a.updateMu.Lock()
	a.updateState.Status = "ready"
	a.updateState.Downloaded = true
	a.updateState.Available = true
	a.updateState.Progress = 100
	a.updateState.stagedPath = staged
	a.updateState.Error = ""
	state := a.updateState
	a.updateMu.Unlock()
	if err := a.persistUpdateState(); err != nil {
		return a.setUpdateError(err)
	}
	a.emitUpdateState()
	return state, nil
}

func (a *App) RestartNow() error {
	if a.activeOps.Load() > 0 {
		return errors.New("finish importing or moving the library before restarting to update")
	}
	libraryPath := a.configuredLibraryPath()
	a.mu.Lock()
	if a.closing {
		a.mu.Unlock()
		return errors.New("application is already closing")
	}
	if a.busy {
		a.mu.Unlock()
		return errors.New("finish the active transfer before restarting to update")
	}
	if a.activeOps.Load() > 0 {
		a.mu.Unlock()
		return errors.New("finish importing or moving the library before restarting to update")
	}
	a.updateMu.Lock()
	if !a.updateState.Downloaded || a.updateState.stagedPath == "" {
		a.updateMu.Unlock()
		a.mu.Unlock()
		return errors.New("download an update before restarting")
	}
	staged := a.updateState.stagedPath
	a.updateMu.Unlock()
	if a.updateScheduled {
		a.mu.Unlock()
		return errors.New("the update is already scheduled")
	}
	target, err := installationTarget()
	if err != nil {
		a.mu.Unlock()
		return err
	}
	if err = updater.LaunchHelper(staged, target, runtime.GOOS, libraryPath, true); err != nil {
		a.mu.Unlock()
		return err
	}
	a.updateScheduled = true
	a.closing = true
	a.mu.Unlock()
	a.updateMu.Lock()
	a.updateState.Status = "restarting"
	state := a.updateState
	a.updateMu.Unlock()
	a.event("update:state", state)
	wruntime.Quit(a.ctx)
	return nil
}

func (a *App) setUpdateError(err error) (UpdateState, error) {
	a.updateMu.Lock()
	a.updateState.Status = "error"
	a.updateState.Error = err.Error()
	state := a.updateState
	a.updateMu.Unlock()
	a.event("update:state", state)
	return state, err
}

func (a *App) emitUpdateState() { state := a.GetUpdateState(); a.event("update:state", state) }

func (a *App) persistUpdateState() error {
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(cache, "Attune", "updater")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	a.updateMu.Lock()
	state := a.updateState
	a.updateMu.Unlock()
	data, err := json.Marshal(struct {
		State      UpdateState `json:"state"`
		StagedPath string      `json:"stagedPath"`
	}{state, state.stagedPath})
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "state.json")
	tmp, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func loadUpdateState() UpdateState {
	state := UpdateState{CurrentVersion: appVersion, Status: "idle"}
	cache, err := os.UserCacheDir()
	if err != nil {
		return state
	}
	updaterDir := filepath.Join(cache, "Attune", "updater")
	if lastError, e := os.ReadFile(filepath.Join(updaterDir, "last-update-error.txt")); e == nil {
		state.Status = "error"
		state.Error = strings.TrimSpace(string(lastError))
	}
	data, err := os.ReadFile(filepath.Join(updaterDir, "state.json"))
	if err != nil {
		return state
	}
	var persisted struct {
		State      UpdateState `json:"state"`
		StagedPath string      `json:"stagedPath"`
	}
	if json.Unmarshal(data, &persisted) != nil || !persisted.State.Downloaded || persisted.StagedPath == "" {
		_ = os.Remove(filepath.Join(updaterDir, "state.json"))
		return state
	}
	saved := persisted.State
	saved.stagedPath = persisted.StagedPath
	if _, err = os.Stat(saved.stagedPath); err != nil {
		_ = os.Remove(filepath.Join(updaterDir, "state.json"))
		return state
	}
	current, currentErr := updater.ParseVersion(appVersion)
	latest, latestErr := updater.ParseVersion(saved.LatestVersion)
	if currentErr != nil || latestErr != nil || latest.Compare(current) <= 0 {
		_ = os.Remove(filepath.Join(updaterDir, "state.json"))
		return state
	}
	saved.CurrentVersion = appVersion
	saved.Status = "ready"
	if lastError, e := os.ReadFile(filepath.Join(updaterDir, "last-update-error.txt")); e == nil {
		saved.Error = strings.TrimSpace(string(lastError))
	} else {
		saved.Error = ""
	}
	return saved
}

func (a *App) applyUpdateOnQuit() {
	if !isDesktopBinary() {
		return
	}
	libraryPath := a.configuredLibraryPath()
	a.mu.Lock()
	if a.updateScheduled {
		a.mu.Unlock()
		return
	}
	a.updateMu.Lock()
	if !a.updateState.Downloaded || a.updateState.stagedPath == "" {
		a.updateMu.Unlock()
		a.mu.Unlock()
		return
	}
	staged := a.updateState.stagedPath
	a.updateMu.Unlock()
	target, err := installationTarget()
	if err == nil {
		err = updater.LaunchHelper(staged, target, runtime.GOOS, libraryPath, false)
	}
	if err != nil {
		a.mu.Unlock()
		_, _ = a.setUpdateError(fmt.Errorf("could not schedule update on quit: %w", err))
		_ = a.persistUpdateState()
		return
	}
	a.updateScheduled = true
	a.mu.Unlock()
}

func (a *App) beginUpdateOperation(timeout time.Duration) (context.Context, context.CancelFunc, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		return nil, nil, errors.New("application is shutting down")
	}
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	a.updateCancel = cancel
	a.jobs.Add(1)
	return ctx, cancel, nil
}

func (a *App) endUpdateOperation(cancel context.CancelFunc) {
	cancel()
	a.mu.Lock()
	if a.updateCancel != nil {
		a.updateCancel = nil
	}
	a.mu.Unlock()
	a.jobs.Done()
}

func (a *App) configuredLibraryPath() string {
	a.mu.Lock()
	root := a.root
	config := a.configPath
	a.mu.Unlock()
	if root != "" {
		return root
	}
	envRoot := os.Getenv("EBOOK_LIBRARY_HOME")
	if config == "" {
		config, _ = librarySettingsPath(envRoot)
	}
	if configured, found, err := loadLibraryLocation(config); err == nil && found {
		return configured
	}
	if envRoot != "" {
		return envRoot
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "Ebook Library")
}

func installationTarget() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)
	if runtime.GOOS == "darwin" {
		for p := dir; p != filepath.Dir(p); p = filepath.Dir(p) {
			if strings.HasSuffix(strings.ToLower(p), ".app") {
				return p, nil
			}
		}
		return "", errors.New("Attune.app bundle could not be found")
	}
	return dir, nil
}

func isDesktopBinary() bool {
	if updateEnabled != "true" || os.Getenv("EBOOK_LIBRARY_HOME") != "" {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	name := strings.ToLower(filepath.Base(exe))
	return name == "attune" || name == "attune.exe"
}
