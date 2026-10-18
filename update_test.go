package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestNewAppDoesNotReadOrRemoveUpdateStateInDevelopment(t *testing.T) {
	cacheRoot := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("LOCALAPPDATA", cacheRoot)
	case "darwin":
		t.Setenv("HOME", cacheRoot)
	default:
		t.Setenv("XDG_CACHE_HOME", cacheRoot)
	}
	actualCache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(actualCache, "Attune", "updater", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(map[string]any{"state": UpdateState{Downloaded: true, LatestVersion: "9.9.9"}, "stagedPath": "/unused"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, state, 0o600); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	if app.updateState.Downloaded || app.updateState.Status != "idle" {
		t.Fatalf("development app loaded production update state: %+v", app.updateState)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("development app touched the persisted state file: %v", err)
	}
}

func TestDevelopmentUpdaterIsDisabled(t *testing.T) {
	app := NewApp()
	if _, err := app.CheckForUpdates(); err == nil || err.Error() != "Self-updates are disabled for isolated development runs. Use the installed app." {
		t.Fatalf("CheckForUpdates error = %v", err)
	}
	if _, err := app.DownloadUpdate(); err == nil || err.Error() != "Self-updates are disabled for isolated development runs. Use the installed app." {
		t.Fatalf("DownloadUpdate error = %v", err)
	}
}

func TestShutdownCancelsAndJoinsUpdateOperation(t *testing.T) {
	app := &App{ctx: context.Background()}
	ctx, cancel, err := app.beginUpdateOperation(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		app.endUpdateOperation(cancel)
		close(done)
	}()
	app.shutdown(context.Background())
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel and join the updater operation")
	}
	if _, _, err := app.beginUpdateOperation(time.Second); err == nil {
		t.Fatal("new update operation was accepted after shutdown")
	}
}

func TestRestartNowRejectsActiveLibraryOperation(t *testing.T) {
	app := &App{}
	app.activeOps.Add(1)
	if err := app.RestartNow(); err == nil {
		t.Fatal("RestartNow accepted an active library operation")
	}
}
