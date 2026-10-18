package updater

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Plan struct {
	ParentPID   int    `json:"parentPid"`
	Staged      string `json:"staged"`
	Target      string `json:"target"`
	GOOS        string `json:"goos"`
	LibraryPath string `json:"libraryPath"`
	Restart     bool   `json:"restart"`
}

func ApplyPackage(staged, target, goos, libraryPath string) error {
	if err := ValidatePackage(staged, goos); err != nil {
		return err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if err := rejectLibraryTarget(absTarget, libraryPath, goos); err != nil {
		return err
	}
	if goos == "darwin" {
		return applyMacBundle(filepath.Join(staged, "Attune.app"), absTarget)
	}
	if err := rejectPortableLibrary(absTarget, libraryPath, goos); err != nil {
		return err
	}
	return applyPortable(staged, absTarget, goos)
}

func PreflightTarget(target, libraryPath, goos string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if goos == "darwin" {
		if err := rejectLibraryTarget(absTarget, libraryPath, goos); err != nil {
			return err
		}
		st, err := os.Lstat(absTarget)
		if err != nil {
			return fmt.Errorf("app bundle is unavailable: %w", err)
		}
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return errors.New("app bundle path is not a real directory")
		}
		parent := filepath.Dir(absTarget)
		probe, err := os.CreateTemp(parent, ".attune-update-check-")
		if err != nil {
			return fmt.Errorf("app bundle parent is not writable: %w", err)
		}
		_ = probe.Close()
		_ = os.Remove(probe.Name())
		return nil
	}
	if err := rejectPortableLibrary(absTarget, libraryPath, goos); err != nil {
		return err
	}
	st, err := os.Stat(absTarget)
	if err != nil {
		return fmt.Errorf("installation folder is unavailable: %w", err)
	}
	if !st.IsDir() {
		return errors.New("installation target is not a directory")
	}
	probe, err := os.CreateTemp(absTarget, ".attune-update-check-")
	if err != nil {
		return fmt.Errorf("installation folder is not writable: %w", err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return nil
}

func rejectPortableLibrary(target, libraryPath, goos string) error {
	if libraryPath == "" {
		return nil
	}
	lib, err := resolvedPath(libraryPath)
	if err != nil {
		return fmt.Errorf("cannot safely resolve configured library location: %w", err)
	}
	files := []string{"INSTALL.txt", "licenses/Attune-LICENSE.txt", "licenses/THIRD_PARTY_NOTICES.md", "licenses/THIRD_PARTY_LICENSES.txt", "licenses/Kindling-LICENSE.txt", "licenses/Kindling-THIRD_PARTY_LICENSES.txt"}
	if goos == "windows" {
		files = append(files, "Attune.exe", "kindling-cli.exe")
	} else {
		files = append(files, "Attune", "kindling-cli")
	}
	for _, rel := range files {
		file, err := resolvedPath(filepath.Join(target, rel))
		if err != nil {
			return fmt.Errorf("cannot safely resolve installed path %s: %w", rel, err)
		}
		if pathOverlaps(file, lib) {
			return fmt.Errorf("cannot update %s because it overlaps the configured library", rel)
		}
	}
	return nil
}

func rejectLibraryTarget(target, libraryPath, goos string) error {
	if goos != "darwin" || libraryPath == "" {
		return nil
	}
	appResolved, err := resolvedPath(target)
	if err != nil {
		return err
	}
	lib, err := resolvedPath(libraryPath)
	if err != nil {
		return fmt.Errorf("cannot safely resolve configured library location: %w", err)
	}
	if pathOverlaps(appResolved, lib) {
		return errors.New("cannot update the app bundle because it contains the configured library")
	}
	return nil
}

func resolvedPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	cur := abs
	var suffix []string
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("no existing ancestor for %s", path)
		}
		suffix = append([]string{filepath.Base(cur)}, suffix...)
		cur = parent
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			for _, part := range suffix {
				resolved = filepath.Join(resolved, part)
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
	}
}

func pathOverlaps(a, b string) bool {
	a, err := filepath.Abs(a)
	if err != nil {
		return true
	}
	b, err = filepath.Abs(b)
	if err != nil {
		return true
	}
	rel, err := filepath.Rel(a, b)
	if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
		return true
	}
	rel, err = filepath.Rel(b, a)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func applyMacBundle(stagedApp, target string) error {
	parent := filepath.Dir(target)
	probe, err := os.CreateTemp(parent, ".attune-update-check-")
	if err != nil {
		return fmt.Errorf("app bundle parent is not writable: %w", err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	if codesign, lookErr := exec.LookPath("codesign"); lookErr == nil {
		if out, err := exec.Command(codesign, "--verify", "--deep", "--strict", stagedApp).CombinedOutput(); err != nil {
			return fmt.Errorf("updated app bundle signature check failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	stageCopy, err := os.MkdirTemp(parent, ".attune-update-stage-")
	if err != nil {
		return fmt.Errorf("create staging folder beside app: %w", err)
	}
	_ = os.Remove(stageCopy)
	backup, err := os.MkdirTemp(parent, ".attune-update-backup-")
	if err != nil {
		return fmt.Errorf("create backup folder beside app: %w", err)
	}
	_ = os.Remove(backup)
	if err := copyTree(stagedApp, stageCopy); err != nil {
		_ = os.RemoveAll(stageCopy)
		return fmt.Errorf("stage app bundle beside installation: %w", err)
	}
	if existing, err := os.Lstat(target); err == nil {
		if existing.Mode()&os.ModeSymlink != 0 || !existing.IsDir() {
			_ = os.RemoveAll(stageCopy)
			return errors.New("existing app bundle path is not a real directory")
		}
		if err = os.Rename(target, backup); err != nil {
			_ = os.RemoveAll(stageCopy)
			return fmt.Errorf("back up current app bundle: %w", err)
		}
	} else if !os.IsNotExist(err) {
		_ = os.RemoveAll(stageCopy)
		return err
	}
	if err := os.Rename(stageCopy, target); err != nil {
		if _, e := os.Lstat(backup); e == nil {
			if restoreErr := os.Rename(backup, target); restoreErr != nil {
				return fmt.Errorf("install updated app bundle: %w; restore backup at %s: %v", err, backup, restoreErr)
			}
		}
		_ = os.RemoveAll(stageCopy)
		return fmt.Errorf("install updated app bundle: %w", err)
	}
	_ = os.RemoveAll(backup)
	return nil
}

func applyPortable(staged, target, goos string) error {
	if goos != "windows" && goos != "linux" {
		return fmt.Errorf("unsupported update target %q", goos)
	}
	if st, err := os.Stat(target); err != nil {
		return fmt.Errorf("installation folder is unavailable: %w", err)
	} else if !st.IsDir() {
		return errors.New("installation target is not a directory")
	}
	stageDir, err := os.MkdirTemp(target, ".attune-update-stage-")
	if err != nil {
		return fmt.Errorf("installation folder is not writable: %w", err)
	}
	keepStage := false
	defer func() {
		if !keepStage {
			_ = os.RemoveAll(stageDir)
		}
	}()
	files := []string{"INSTALL.txt", "licenses/Attune-LICENSE.txt", "licenses/THIRD_PARTY_NOTICES.md", "licenses/THIRD_PARTY_LICENSES.txt", "licenses/Kindling-LICENSE.txt", "licenses/Kindling-THIRD_PARTY_LICENSES.txt"}
	if goos == "windows" {
		files = append([]string{"Attune.exe", "kindling-cli.exe"}, files...)
	} else {
		files = append([]string{"Attune", "kindling-cli"}, files...)
	}
	for _, rel := range files {
		if err := copyTree(filepath.Join(staged, rel), filepath.Join(stageDir, rel)); err != nil {
			return fmt.Errorf("stage %s: %w", rel, err)
		}
	}
	backup := filepath.Join(stageDir, ".backup")
	if err := os.Mkdir(backup, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(filepath.Join(target, "licenses")); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return errors.New("existing licenses path is not a regular directory")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	var replaced []string
	rollback := func() error {
		var errs []string
		for i := len(replaced) - 1; i >= 0; i-- {
			rel := replaced[i]
			dst := filepath.Join(target, rel)
			if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err.Error())
			}
			bak := filepath.Join(backup, rel)
			if _, e := os.Lstat(bak); e == nil {
				if err := os.Rename(bak, dst); err != nil {
					errs = append(errs, fmt.Sprintf("restore %s from %s: %v", rel, bak, err))
				}
			}
		}
		if len(errs) > 0 {
			return errors.New(strings.Join(errs, "; "))
		}
		return nil
	}
	for _, rel := range files {
		dst, src, bak := filepath.Join(target, rel), filepath.Join(stageDir, rel), filepath.Join(backup, rel)
		if st, err := os.Lstat(dst); err == nil {
			if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
				if restoreErr := rollback(); restoreErr != nil {
					keepStage = true
					return fmt.Errorf("refuse replacing non-regular installed path %s; rollback failed (backup retained at %s): %v", rel, backup, restoreErr)
				}
				return fmt.Errorf("refuse replacing non-regular installed path %s", rel)
			}
			if err = os.MkdirAll(filepath.Dir(bak), 0o700); err != nil {
				if restoreErr := rollback(); restoreErr != nil {
					keepStage = true
					return fmt.Errorf("prepare backup for %s: %w; rollback failed (backup retained at %s): %v", rel, err, backup, restoreErr)
				}
				return err
			}
			if err = os.Rename(dst, bak); err != nil {
				if restoreErr := rollback(); restoreErr != nil {
					keepStage = true
					return fmt.Errorf("back up %s: %w; rollback failed (backup retained at %s): %v", rel, err, backup, restoreErr)
				}
				return fmt.Errorf("back up %s: %w", rel, err)
			}
		} else if !os.IsNotExist(err) {
			if restoreErr := rollback(); restoreErr != nil {
				keepStage = true
				return fmt.Errorf("inspect %s: %w; rollback failed (backup retained at %s): %v", rel, err, backup, restoreErr)
			}
			return err
		}
		replaced = append(replaced, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			if restoreErr := rollback(); restoreErr != nil {
				keepStage = true
				return fmt.Errorf("prepare %s: %w; rollback failed (backup retained at %s): %v", rel, err, backup, restoreErr)
			}
			return err
		}
		if err := os.Rename(src, dst); err != nil {
			if restoreErr := rollback(); restoreErr != nil {
				keepStage = true
				return fmt.Errorf("install %s: %w; rollback failed (backup retained at %s): %v", rel, err, backup, restoreErr)
			}
			return fmt.Errorf("install %s: %w", rel, err)
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symbolic link %s", src)
	}
	if st.IsDir() {
		if err := os.MkdirAll(dst, st.Mode().Perm()|0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTree(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("refusing special file %s", src)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, st.Mode().Perm()&0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
