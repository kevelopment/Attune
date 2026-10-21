package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPackagedHelperStartup(t *testing.T) {
	appExecutable := os.Getenv("ATTUNE_UPDATER_TEST_APP")
	if appExecutable == "" {
		t.Skip("ATTUNE_UPDATER_TEST_APP is not set")
	}
	if !filepath.IsAbs(appExecutable) {
		t.Fatalf("ATTUNE_UPDATER_TEST_APP must be absolute: %q", appExecutable)
	}

	root := t.TempDir()
	helperDir := filepath.Join(root, "updater")
	helper := helperExecutable(helperDir)
	if err := os.MkdirAll(filepath.Dir(helper), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutableForHelper(appExecutable, helper); err != nil {
		t.Fatalf("copy packaged app executable and frameworks: %v", err)
	}
	sentinel := filepath.Join(root, "sentinel-update-plan.json")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, helper, "--attune-update-helper", sentinel).CombinedOutput()
	if err == nil {
		t.Fatalf("helper unexpectedly accepted missing plan %s", sentinel)
	}
	if !strings.Contains(string(output), filepath.Base(sentinel)) {
		t.Fatalf("helper did not reach Go startup for sentinel plan %s; output: %s", sentinel, output)
	}
}

func TestUpdaterHelperStartupChild(t *testing.T) {
	switch os.Getenv("ATTUNE_UPDATER_TEST_HELPER_CHILD") {
	case "exit":
		fmt.Fprintln(os.Stderr, "sentinel child startup failure")
		os.Exit(23)
	case "sleep":
		time.Sleep(time.Minute)
	}
}

func TestStartHelperAndWaitFailurePaths(t *testing.T) {
	for _, test := range []struct {
		name    string
		mode    string
		timeout time.Duration
		want    string
	}{
		{name: "early exit captures stderr", mode: "exit", timeout: 5 * time.Second, want: "sentinel child startup failure"},
		{name: "timeout kills and reaps child", mode: "sleep", timeout: 200 * time.Millisecond, want: "did not become ready"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			planPath := filepath.Join(root, "pending-update.json")
			if err := os.WriteFile(planPath, []byte("plan"), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestUpdaterHelperStartupChild$")
			cmd.Env = append(os.Environ(), "ATTUNE_UPDATER_TEST_HELPER_CHILD="+test.mode)
			err := startHelperAndWait(cmd, planPath, filepath.Join(root, "startup.log"), test.timeout)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("startHelperAndWait error = %v, want substring %q", err, test.want)
			}
			if cmd.ProcessState == nil {
				t.Fatalf("child process was not reaped: state=%v", cmd.ProcessState)
			}
			for _, path := range []string{planPath, planPath + ".ready"} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed startup left %s: %v", path, err)
				}
			}
		})
	}
}

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
		if _, err := os.Stat(planPath + ".ready"); err != nil {
			t.Errorf("helper did not signal readiness before waiting: %v", err)
		}
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
	if _, err := os.Stat(planPath + ".ready"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("success left readiness marker: %v", err)
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
	err := runHelper(planPath, cache, func(int, time.Duration) error {
		if _, err := os.Stat(planPath + ".ready"); err != nil {
			t.Errorf("helper did not signal readiness before waiting: %v", err)
		}
		return want
	}, nil, nil)
	if !errors.Is(err, want) {
		t.Fatalf("runHelper error = %v, want %v", err, want)
	}
	got, err := os.ReadFile(filepath.Join(updaterDir, "last-update-error.txt"))
	if err != nil || string(got) != want.Error()+"\n" {
		t.Fatalf("failure record = %q, %v", got, err)
	}
	if _, err := os.Stat(planPath + ".ready"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failure left readiness marker: %v", err)
	}
}

func TestRunHelperKeepsUpdateWhenRelaunchFails(t *testing.T) {
	cache := t.TempDir()
	updaterDir := filepath.Join(cache, "updater")
	stage := filepath.Join(cache, "updates", "release")
	planPath := filepath.Join(updaterDir, "pending-update.json")
	target := filepath.Join(t.TempDir(), "install")
	if err := os.MkdirAll(updaterDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "payload"), []byte("staged"), 0o600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(updaterDir, "state.json")
	if err := os.WriteFile(statePath, []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := Plan{ParentPID: 123, Staged: stage, Target: target, GOOS: runtime.GOOS, Restart: true}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	want := errors.New("launch failed")
	err = runHelper(planPath, cache, func(int, time.Duration) error { return nil }, func(string, string, string, string) error { return nil }, func(string, string) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("runHelper error = %v, want %v", err, want)
	}
	for _, path := range []string{planPath, statePath, filepath.Join(stage, "payload")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("relaunch failure should preserve %s: %v", path, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(updaterDir, "last-update-error.txt"))
	if err != nil || string(got) != "relaunch updated app: launch failed\n" {
		t.Fatalf("relaunch failure record = %q, %v", got, err)
	}
}
