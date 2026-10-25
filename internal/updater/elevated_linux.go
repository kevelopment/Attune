//go:build linux

package updater

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// RunElevatedHelper is the narrow privileged entry point used by pkexec for a
// system-installed Linux build. It only writes into the directory containing
// this root-owned Attune executable.
func RunElevatedHelper(planPath, cacheRoot string) error {
	if os.Geteuid() != 0 {
		return errors.New("elevated updater helper must run as root")
	}
	uid, err := strconv.ParseUint(os.Getenv("PKEXEC_UID"), 10, 32)
	if err != nil {
		return errors.New("Polkit caller identity is unavailable")
	}
	if err := validateElevatedCallerCache(planPath, cacheRoot, uint32(uid)); err != nil {
		return err
	}
	plan, err := readPlan(planPath, cacheRoot)
	if err != nil {
		return err
	}
	if err := validateElevatedUpdate(cacheRoot, plan, uint32(uid)); err != nil {
		return err
	}
	err = runHelper(planPath, cacheRoot, waitForParent, ApplyPackage, func(string, string) error { return nil })
	if err != nil {
		// The cache directory is private to the Polkit caller. Make the error
		// readable by that caller's next Attune launch; the helper is root-owned.
		_ = os.Chmod(filepath.Join(cacheRoot, "updater", "last-update-error.txt"), 0o644)
	}
	return err
}

func validateElevatedCallerCache(planPath, cacheRoot string, uid uint32) error {
	if !filepath.IsAbs(planPath) || !filepath.IsAbs(cacheRoot) {
		return errors.New("update cache paths must be absolute")
	}

	cache, err := filepath.EvalSymlinks(cacheRoot)
	if err != nil {
		return fmt.Errorf("resolve updater cache: %w", err)
	}
	if err := requireOwnedDirectory(cache, uid); err != nil {
		return fmt.Errorf("unsafe updater cache: %w", err)
	}
	updaterDir, err := filepath.EvalSymlinks(filepath.Join(cacheRoot, "updater"))
	if err != nil {
		return fmt.Errorf("resolve updater plan directory: %w", err)
	}
	if !inside(cache, updaterDir) {
		return errors.New("updater plan directory is outside the private Attune cache")
	}
	if err := requireOwnedDirectory(updaterDir, uid); err != nil {
		return fmt.Errorf("unsafe updater plan directory: %w", err)
	}
	resolvedPlan, err := filepath.EvalSymlinks(planPath)
	if err != nil {
		return fmt.Errorf("resolve update plan: %w", err)
	}
	if filepath.Clean(filepath.Dir(resolvedPlan)) != filepath.Clean(updaterDir) || filepath.Base(resolvedPlan) != "pending-update.json" {
		return errors.New("update plan is outside the private Attune updater folder")
	}
	if err := requireOwnedRegularFile(resolvedPlan, uid, true); err != nil {
		return fmt.Errorf("unsafe update plan: %w", err)
	}
	return nil
}

func validateElevatedUpdate(cacheRoot string, plan Plan, uid uint32) error {
	if plan.GOOS != "linux" {
		return errors.New("elevated updater only accepts Linux packages")
	}
	cache, err := filepath.EvalSymlinks(cacheRoot)
	if err != nil {
		return fmt.Errorf("resolve updater cache: %w", err)
	}
	resolvedStage, err := filepath.EvalSymlinks(plan.Staged)
	if err != nil {
		return fmt.Errorf("resolve staged update: %w", err)
	}
	if !inside(filepath.Join(cache, "updates"), resolvedStage) {
		return errors.New("staged update is outside the private Attune update cache")
	}
	if err := requireOwnedDirectory(resolvedStage, uid); err != nil {
		return fmt.Errorf("unsafe staged update: %w", err)
	}
	if err := ValidatePackage(resolvedStage, "linux"); err != nil {
		return err
	}
	for _, name := range []string{"Attune", "kindling-cli", "INSTALL.txt", "licenses/Attune-LICENSE.txt", "licenses/THIRD_PARTY_NOTICES.md", "licenses/THIRD_PARTY_LICENSES.txt", "licenses/Kindling-LICENSE.txt", "licenses/Kindling-THIRD_PARTY_LICENSES.txt"} {
		if err := requireOwnedRegularFile(filepath.Join(resolvedStage, filepath.FromSlash(name)), uid, false); err != nil {
			return fmt.Errorf("unsafe staged package entry %s: %w", name, err)
		}
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve installed Attune executable: %w", err)
	}
	if filepath.Base(executable) != "Attune" {
		return errors.New("elevated updater was not launched from the installed Attune executable")
	}
	if err := requireOwnedRegularFile(executable, 0, false); err != nil {
		return fmt.Errorf("untrusted installed Attune executable: %w", err)
	}
	target, err := filepath.EvalSymlinks(filepath.Dir(executable))
	if err != nil {
		return fmt.Errorf("resolve installed Attune directory: %w", err)
	}
	if filepath.Clean(target) != "/usr/lib/attune" {
		return errors.New("elevated updates are only supported for the system package installed under /usr/lib/attune")
	}
	if resolvedTarget, e := filepath.EvalSymlinks(plan.Target); e != nil || filepath.Clean(resolvedTarget) != filepath.Clean(target) {
		return errors.New("update target does not match the installed Attune directory")
	}
	for path := target; ; path = filepath.Dir(path) {
		if err := requireOwnedDirectory(path, 0); err != nil {
			return fmt.Errorf("untrusted installed Attune path: %w", err)
		}
		if filepath.Dir(path) == path {
			break
		}
	}
	return nil
}

func requireOwnedDirectory(path string, uid uint32) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path is not a real directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uid {
		return errors.New("directory owner does not match the caller")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errors.New("directory is writable by another user")
	}
	return nil
}

func requireOwnedRegularFile(path string, uid uint32, private bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path is not a regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uid {
		return errors.New("file owner does not match the caller")
	}
	if info.Mode().Perm()&0o022 != 0 || (private && info.Mode().Perm()&0o077 != 0) {
		return errors.New("file permissions are not private")
	}
	return nil
}

func runElevatedUpdate(planPath, cacheRoot string, plan Plan) error {
	pkexec, err := exec.LookPath("pkexec")
	if err != nil {
		return errors.New("this system installation needs Polkit (pkexec) approval to update, but pkexec was not found")
	}
	installedApp := filepath.Join(plan.Target, "Attune")
	cmd := exec.Command(pkexec, installedApp, "--attune-update-helper-root", planPath, cacheRoot)
	cmd.Dir = filepath.Dir(planPath)
	cmd.Stderr = os.Stderr
	if err := startUpdateHelper(cmd); err != nil {
		return fmt.Errorf("request permission to update Attune: %w", err)
	}
	waitResult := make(chan error, 1)
	go func() { waitResult <- cmd.Wait() }()
	readyPath := planPath + ".ready"
	deadline := time.Now().Add(20 * time.Minute)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check privileged updater readiness: %w", err)
		}
		select {
		case waitErr := <-waitResult:
			if waitErr != nil {
				return fmt.Errorf("system update authorization failed or was cancelled: %w", waitErr)
			}
			return errors.New("privileged updater exited before becoming ready")
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			<-waitResult
			return errors.New("system update authorization timed out")
		}
	}
	if err := <-waitResult; err != nil {
		updateErr := fmt.Errorf("privileged system update failed: %w", err)
		persistElevatedUpdateError(cacheRoot, updateErr)
		return updateErr
	}
	if plan.Restart {
		if err := relaunch(plan.Target, plan.GOOS); err != nil {
			updateErr := fmt.Errorf("relaunch updated app: %w", err)
			persistElevatedUpdateError(cacheRoot, updateErr)
			return updateErr
		}
	}
	return nil
}

func persistElevatedUpdateError(cacheRoot string, updateErr error) {
	updaterDir := filepath.Join(cacheRoot, "updater")
	path := filepath.Join(updaterDir, "last-update-error.txt")
	_ = os.Remove(path)
	_ = os.MkdirAll(updaterDir, 0o700)
	_ = os.WriteFile(path, []byte(strings.TrimSpace(updateErr.Error())+"\n"), 0o600)
}
