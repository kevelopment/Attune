package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func userInstallTarget() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("home directory is not an absolute path")
	}
	return filepath.Join(home, ".local", "opt", "attune"), nil
}

func isUserInstallTarget(target string) bool {
	userTarget, err := userInstallTarget()
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	absUserTarget, err := filepath.Abs(userTarget)
	return err == nil && filepath.Clean(absTarget) == filepath.Clean(absUserTarget)
}

func installUserDesktopEntry(target string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	} else if !filepath.IsAbs(dataHome) {
		return fmt.Errorf("XDG_DATA_HOME is not an absolute path")
	}
	applications := filepath.Join(dataHome, "applications")
	if err := os.MkdirAll(applications, 0o755); err != nil {
		return err
	}

	executable := filepath.Join(target, "Attune")
	icon := "attune"
	if st, err := os.Lstat(filepath.Join(target, "attune.svg")); err == nil && st.Mode().IsRegular() {
		icon = filepath.Join(target, "attune.svg")
	}
	contents := "[Desktop Entry]\nType=Application\nName=Attune\nComment=Manage your ebook library\nExec=" + desktopExecArgument(executable) + "\nIcon=" + desktopValue(icon) + "\nTerminal=false\nCategories=Office;\nStartupWMClass=Attune\n"
	return writeUserFile(filepath.Join(applications, "attune.desktop"), []byte(contents), 0o644)
}

func desktopExecArgument(value string) string {
	value = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", `\`+"`", "$", `\$`, "%", "%%").Replace(value)
	return `"` + value + `"`
}

func desktopValue(value string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", "", "\r", "").Replace(value)
}

func writeUserFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".attune-launcher-*")
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
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return nil
}
