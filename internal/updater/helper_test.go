package updater

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRunHelperAppliesAndCleansPendingUpdate(t *testing.T) {
	cache := t.TempDir()
	updaterDir := filepath.Join(cache, "updater")
	stage := filepath.Join(cache, "updates", "release")
	planPath := filepath.Join(updaterDir, "pending-update.json")
	target := filepath.Join(t.TempDir(), "install")
	if err := os.MkdirAll(filepath.Dir(planPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "licenses"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(updaterDir, "state.json"), []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "payload"), []byte("staged"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := Plan{ParentPID: 123, Staged: stage, Target: target, GOOS: runtime.GOOS, LibraryPath: filepath.Join(t.TempDir(), "library")}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	waited := false
	applied := false
	if err := runHelper(planPath, cache, func(pid int, timeout time.Duration) error {
		waited = pid == plan.ParentPID && timeout == 20*time.Minute
		return nil
	}, func(staged, install, goos, library string) error {
		applied = staged == plan.Staged && install == plan.Target && goos == plan.GOOS && library == plan.LibraryPath
		return nil
	}, func(string, string) error { t.Fatal("unexpected restart"); return nil }); err != nil {
		t.Fatal(err)
	}
	if !waited || !applied {
		t.Fatalf("waited=%t applied=%t", waited, applied)
	}
	for _, path := range []string{planPath, filepath.Join(updaterDir, "state.json"), filepath.Join(cache, "updates")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected cleanup of %s, stat error=%v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(updaterDir, "last-update-error.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("success left stale error record: %v", err)
	}
}

func TestRunHelperPersistsFailure(t *testing.T) {
	cache := t.TempDir()
	updaterDir := filepath.Join(cache, "updater")
	planPath := filepath.Join(updaterDir, "pending-update.json")
	if err := os.MkdirAll(updaterDir, 0o700); err != nil {
		t.Fatal(err)
	}
	plan := Plan{ParentPID: 123, Staged: filepath.Join(cache, "updates", "release"), Target: filepath.Join(t.TempDir(), "install"), GOOS: runtime.GOOS}
	data, _ := json.Marshal(plan)
	if err := os.WriteFile(planPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	want := errors.New("parent wait failed")
	err := runHelper(planPath, cache, func(int, time.Duration) error { return want }, nil, nil)
	if !errors.Is(err, want) {
		t.Fatalf("runHelper error = %v, want %v", err, want)
	}
	got, err := os.ReadFile(filepath.Join(updaterDir, "last-update-error.txt"))
	if err != nil || string(got) != want.Error()+"\n" {
		t.Fatalf("failure record = %q, %v", got, err)
	}
}
