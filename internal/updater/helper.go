package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func RunHelper(planPath string) (result error) {
	cacheDir, cacheErr := os.UserCacheDir()
	if cacheErr != nil {
		return cacheErr
	}
	cacheRoot := filepath.Join(cacheDir, "Attune")
	plan, err := readPlan(planPath, cacheRoot)
	if err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		if err := PreflightTarget(plan.Target, plan.LibraryPath, plan.GOOS); err != nil {
			if errors.Is(err, os.ErrPermission) {
				return runElevatedUpdate(planPath, cacheRoot, plan)
			}
			return err
		}
	}
	return runHelper(planPath, cacheRoot, waitForParent, ApplyPackage, relaunch)
}

func runHelper(planPath, cacheRoot string, wait func(int, time.Duration) error, apply func(string, string, string, string) error, restart func(string, string) error) (result error) {
	updaterDir := filepath.Join(cacheRoot, "updater")
	defer func() {
		if result == nil {
			return
		}
		_ = os.MkdirAll(updaterDir, 0o700)
		_ = os.WriteFile(filepath.Join(updaterDir, "last-update-error.txt"), []byte(result.Error()+"\n"), 0o600)
	}()
	plan, err := readPlan(planPath, cacheRoot)
	if err != nil {
		return err
	}
	defer os.Remove(planPath + ".ready")
	if err := os.WriteFile(planPath+".ready", []byte("ready\n"), 0o600); err != nil {
		return fmt.Errorf("signal updater helper readiness: %w", err)
	}
	if err := wait(plan.ParentPID, 20*time.Minute); err != nil {
		return err
	}
	if err := apply(plan.Staged, plan.Target, plan.GOOS, plan.LibraryPath); err != nil {
		return err
	}
	if plan.Restart {
		if err := restart(plan.Target, plan.GOOS); err != nil {
			return fmt.Errorf("relaunch updated app: %w", err)
		}
	}
	_ = os.Remove(planPath)
	_ = os.Remove(filepath.Join(updaterDir, "state.json"))
	cache, err := filepath.Abs(cacheRoot)
	if err != nil {
		return err
	}
	if inside(filepath.Join(cache, "updates"), plan.Staged) {
		_ = os.RemoveAll(filepath.Dir(plan.Staged))
	}
	_ = os.Remove(filepath.Join(updaterDir, "last-update-error.txt"))
	return nil
}

func readPlan(planPath, cacheRoot string) (Plan, error) {
	var plan Plan
	updaterDir := filepath.Join(cacheRoot, "updater")
	info, err := os.Lstat(planPath)
	if err != nil {
		return plan, err
	}
	if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return plan, errors.New("update plan is not a private regular file")
	}
	if !inside(updaterDir, planPath) {
		return plan, errors.New("update plan is outside the private Attune updater folder")
	}
	b, err := os.ReadFile(planPath)
	if err != nil {
		return plan, err
	}
	if err := json.Unmarshal(b, &plan); err != nil {
		return plan, err
	}
	if plan.ParentPID <= 0 || plan.GOOS != runtime.GOOS {
		return plan, errors.New("update plan does not match this process")
	}
	if plan.Staged == "" || plan.Target == "" || !filepath.IsAbs(plan.Staged) || !filepath.IsAbs(plan.Target) {
		return plan, errors.New("update plan contains invalid paths")
	}
	cache, err := filepath.Abs(cacheRoot)
	if err != nil {
		return plan, err
	}
	if !inside(cache, plan.Staged) {
		return plan, errors.New("staged update is outside the Attune cache")
	}
	return plan, nil
}

func inside(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func relaunch(target, goos string) error {
	if goos == "darwin" {
		cmd := exec.Command("open", target)
		cmd.Dir = filepath.Dir(target)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("open updated app: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}
	name := "Attune"
	if goos == "windows" {
		name += ".exe"
	}
	cmd := exec.Command(filepath.Join(target, name))
	cmd.Dir = filepath.Dir(target)
	return startDetached(cmd)
}

func writePlan(path string, p Plan) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".update-plan-*")
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

func CopySelfForHelper(dest string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return copyExecutableForHelper(exe, dest)
}

func copyExecutableForHelper(exe, dest string) error {
	if runtime.GOOS == "darwin" {
		if err := copyHelperFrameworks(filepath.Dir(exe), filepath.Dir(dest)); err != nil {
			return fmt.Errorf("copy updater helper frameworks: %w", err)
		}
	}
	in, err := os.Open(exe)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove previous updater helper: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".helper-*")
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
	if err = tmp.Chmod(0o700); err == nil {
		_, err = io.Copy(tmp, in)
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
	if err = os.Rename(tmpName, dest); err != nil {
		return err
	}
	ok = true
	return nil
}

func copyHelperFrameworks(executableDir, helperDir string) error {
	source := filepath.Clean(filepath.Join(executableDir, "..", "Frameworks"))
	if _, err := os.Stat(source); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	parent := filepath.Dir(helperDir)
	target := filepath.Join(parent, "Frameworks")
	stage, err := os.MkdirTemp(parent, ".updater-frameworks-")
	if err != nil {
		return err
	}
	_ = os.Remove(stage)
	if err := copyTree(source, stage); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	if err := os.Rename(stage, target); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	return nil
}

func LaunchHelper(staged, target, goos, libraryPath string, restart bool) error {
	if err := ValidatePackage(staged, goos); err != nil {
		return err
	}
	needsElevation := false
	if err := PreflightTarget(target, libraryPath, goos); err != nil {
		if goos == "linux" && errors.Is(err, os.ErrPermission) {
			needsElevation = true
		} else {
			return err
		}
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(cache, "Attune", "updater")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	helperPath := helperExecutable(dir)
	if err := os.MkdirAll(filepath.Dir(helperPath), 0o700); err != nil {
		return err
	}
	if err := CopySelfForHelper(helperPath); err != nil {
		return err
	}
	planPath := filepath.Join(dir, "pending-update.json")
	if err := os.Remove(planPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove previous update plan: %w", err)
	}
	if err := writePlan(planPath, Plan{ParentPID: os.Getpid(), Staged: staged, Target: target, GOOS: goos, LibraryPath: libraryPath, Restart: restart}); err != nil {
		return err
	}
	readyPath := planPath + ".ready"
	if err := os.Remove(readyPath); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(planPath)
		return fmt.Errorf("remove previous updater readiness marker: %w", err)
	}
	startupLog := filepath.Join(dir, "helper-startup.log")
	cmd := exec.Command(helperExecutable(dir), "--attune-update-helper", planPath)
	cmd.Dir = dir
	timeout := 5 * time.Second
	if needsElevation {
		timeout = 20 * time.Minute
	}
	return startHelperAndWait(cmd, planPath, startupLog, timeout)
}

func startHelperAndWait(cmd *exec.Cmd, planPath, startupLog string, timeout time.Duration) error {
	readyPath := planPath + ".ready"
	logFile, err := os.OpenFile(startupLog, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		_ = os.Remove(planPath)
		return fmt.Errorf("open updater startup log: %w", err)
	}
	cmd.Stderr = logFile
	if err := startUpdateHelper(cmd); err != nil {
		_ = logFile.Close()
		_ = os.Remove(planPath)
		_ = os.Remove(readyPath)
		return fmt.Errorf("start update helper: %w", err)
	}
	_ = logFile.Close()
	waitResult := make(chan error, 1)
	go func() { waitResult <- cmd.Wait() }()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(readyPath); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			_ = cmd.Process.Kill()
			<-waitResult
			_ = os.Remove(planPath)
			_ = os.Remove(readyPath)
			return fmt.Errorf("check updater helper readiness: %w", err)
		}
		select {
		case err := <-waitResult:
			output, _ := os.ReadFile(startupLog)
			_ = os.Remove(planPath)
			_ = os.Remove(readyPath)
			if detail := strings.TrimSpace(string(output)); detail != "" {
				return fmt.Errorf("update helper exited before becoming ready: %s", detail)
			}
			if err == nil {
				return errors.New("update helper exited before becoming ready")
			}
			return fmt.Errorf("update helper exited before becoming ready: %w", err)
		case <-time.After(50 * time.Millisecond):
		}
	}
	_ = cmd.Process.Kill()
	<-waitResult
	output, _ := os.ReadFile(startupLog)
	_ = os.Remove(planPath)
	_ = os.Remove(readyPath)
	if detail := strings.TrimSpace(string(output)); detail != "" {
		return fmt.Errorf("update helper did not become ready: %s", detail)
	}
	return fmt.Errorf("update helper did not become ready within %s", timeout)
}

func helperExecutable(dir string) string {
	name := "attune-update-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(dir, "MacOS", name)
	}
	return filepath.Join(dir, name)
}
