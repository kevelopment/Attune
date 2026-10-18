package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyPortablePreservesAdjacentUserData(t *testing.T) {
	staged := writePortablePackage(t)
	install := t.TempDir()
	for path, content := range map[string]string{
		"Attune.exe": "old app", "kindling-cli.exe": "old helper", "INSTALL.txt": "old install",
		"library.sqlite": "user library", "settings.json": "user settings", "licenses/user.txt": "user notice",
		"licenses/Attune-LICENSE.txt": "old license",
	} {
		writeFile(t, filepath.Join(install, path), content)
	}
	if err := ApplyPackage(staged, install, "windows", ""); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"Attune.exe": "new app", "kindling-cli.exe": "new helper", "library.sqlite": "user library", "settings.json": "user settings", "licenses/user.txt": "user notice", "licenses/Attune-LICENSE.txt": "license"} {
		got, err := os.ReadFile(filepath.Join(install, path))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v", path, got, err)
		}
	}
}

func TestApplyPortableRollsBackIfLaterTargetIsInvalid(t *testing.T) {
	staged := writePortablePackage(t)
	install := t.TempDir()
	for path, content := range map[string]string{"Attune.exe": "old app", "kindling-cli.exe": "old helper", "INSTALL.txt": "old install", "licenses/Attune-LICENSE.txt": "old license"} {
		writeFile(t, filepath.Join(install, path), content)
	}
	if err := os.MkdirAll(filepath.Join(install, "licenses", "THIRD_PARTY_NOTICES.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := ApplyPackage(staged, install, "windows", "")
	if err == nil || !strings.Contains(err.Error(), "non-regular") {
		t.Fatalf("ApplyPackage error = %v", err)
	}
	for path, want := range map[string]string{"Attune.exe": "old app", "kindling-cli.exe": "old helper", "INSTALL.txt": "old install", "licenses/Attune-LICENSE.txt": "old license"} {
		got, err := os.ReadFile(filepath.Join(install, path))
		if err != nil || string(got) != want {
			t.Fatalf("rollback %s = %q, %v", path, got, err)
		}
	}
	entries, err := os.ReadDir(install)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".attune-update-stage-") {
			t.Fatalf("transaction staging directory was left behind: %s", entry.Name())
		}
	}
}

func TestMacLibrarySymlinkOverlapFailsClosed(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "Attune.app")
	if err := os.MkdirAll(filepath.Join(app, "Contents"), 0o700); err != nil {
		t.Fatal(err)
	}
	library := filepath.Join(app, "Contents", "library.sqlite")
	writeFile(t, library, "library data")
	alias := filepath.Join(root, "library-alias")
	if err := os.Symlink(library, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := rejectLibraryTarget(app, alias, "darwin"); err == nil {
		t.Fatal("library symlink alias inside app bundle was not rejected")
	}
}

func TestPortableUpdateRefusesConfiguredLibraryOverlap(t *testing.T) {
	root := t.TempDir()
	install := filepath.Join(root, "Portable")
	if err := os.MkdirAll(install, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PreflightTarget(install, install, "linux"); err == nil {
		t.Fatal("updater should refuse replacing files inside the configured library")
	}
}

func TestPreflightReportsReadOnlyTarget(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if err := PreflightTarget(missing, "", "linux"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("preflight error = %v", err)
	}
}

func writePortablePackage(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{"Attune.exe": "new app", "kindling-cli.exe": "new helper", "INSTALL.txt": "install", "licenses/Attune-LICENSE.txt": "license", "licenses/THIRD_PARTY_NOTICES.md": "notices", "licenses/THIRD_PARTY_LICENSES.txt": "licenses", "licenses/Kindling-LICENSE.txt": "kindling", "licenses/Kindling-THIRD_PARTY_LICENSES.txt": "crate licenses"} {
		writeFile(t, filepath.Join(root, name), content)
	}
	return root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
