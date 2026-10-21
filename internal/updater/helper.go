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
	return runHelper(planPath, filepath.Join(cacheDir, "Attune"), waitForParent, ApplyPackage, relaunch)
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
	info, err := os.Lstat(planPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return errors.New("update plan is not a private regular file")
	}
	if !inside(updaterDir, planPath) {
		return errors.New("update plan is outside the private Attune updater folder")
	}
	b, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	var plan Plan
	if err := json.Unmarshal(b, &plan); err != nil {
		return err
	}
	if plan.ParentPID <= 0 || plan.GOOS != runtime.GOOS {
		return errors.New("update plan does not match this process")
	}
	if plan.Staged == "" || plan.Target == "" || !filepath.IsAbs(plan.Staged) || !filepath.IsAbs(plan.Target) {
		return errors.New("update plan contains invalid paths")
	}
	cache, err := filepath.Abs(cacheRoot)
	if err != nil {
		return err
	}
	if !inside(cache, plan.Staged) {
		return errors.New("staged update is outside the Attune cache")
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
	if inside(filepath.Join(cache, "updates"), plan.Staged) {
		_ = os.RemoveAll(filepath.Dir(plan.Staged))
	}
	_ = os.Remove(filepath.Join(updaterDir, "last-update-error.txt"))
	return nil
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

func LaunchHelper(staged, target, goos, libraryPath string, restart bool) error {
	if err := ValidatePackage(staged, goos); err != nil {
		return err
	}
	if err := PreflightTarget(target, libraryPath, goos); err != nil {
		return err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(cache, "Attune", "updater")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := CopySelfForHelper(helperExecutable(dir)); err != nil {
		return err
	}
	planPath := filepath.Join(dir, "pending-update.json")
	if err := os.Remove(planPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove previous update plan: %w", err)
	}
	if err := writePlan(planPath, Plan{ParentPID: os.Getpid(), Staged: staged, Target: target, GOOS: goos, LibraryPath: libraryPath, Restart: restart}); err != nil {
		return err
	}
	cmd := exec.Command(helperExecutable(dir), "--attune-update-helper", planPath)
	cmd.Dir = dir
	if err := startDetached(cmd); err != nil {
		_ = os.Remove(planPath)
		return fmt.Errorf("start update helper: %w", err)
	}
	return nil
}

func helperExecutable(dir string) string {
	name := "attune-update-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name)
}
