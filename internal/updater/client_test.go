package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPackageAssetName(t *testing.T) {
	cases := []struct{ goos, arch, version, want string }{
		{"darwin", "arm64", "1.2.3", "Attune-macos-arm64-1.2.3.zip"},
		{"windows", "amd64", "1.2.3", "Attune-windows-amd64-1.2.3.zip"},
		{"linux", "arm64", "1.2.3", "Attune-linux-arm64-1.2.3.tar.gz"},
		{"plan9", "amd64", "1.2.3", ""},
	}
	for _, tc := range cases {
		if got := PackageAssetName(tc.goos, tc.arch, tc.version); got != tc.want {
			t.Errorf("PackageAssetName(%q,%q,%q) = %q, want %q", tc.goos, tc.arch, tc.version, got, tc.want)
		}
	}
}

func TestCheckSelectsReleaseByInstalledChannelAndTrustedAssets(t *testing.T) {
	flaggedPrerelease := githubReleaseFixture("v1.4.0", false, "Attune-windows-amd64-1.4.0.zip", "SHA256SUMS.txt")
	flaggedPrerelease["prerelease"] = true
	releases := []map[string]any{
		githubReleaseFixture("v9.0.0", true, "Attune-windows-amd64-9.0.0.zip", "SHA256SUMS.txt"),
		githubReleaseFixture("v8.0.0-beta.1", false, "Attune-windows-amd64-8.0.0-beta.1.zip", "SHA256SUMS.txt"),
		githubReleaseFixture("v7.0.0", false, "Attune-windows-amd64-7.0.0.zip"), // checksum missing
		githubReleaseFixtureWithURL("v6.0.0", "https://github.com/attacker/Attune/releases/download/v6.0.0/Attune-windows-amd64-6.0.0.zip", "https://github.com/kevelopment/Attune/releases/download/v6.0.0/SHA256SUMS.txt"),
		githubReleaseFixture("v1.2.0-beta.4", false, "Attune-windows-amd64-1.2.0-beta.4.zip", "SHA256SUMS.txt"),
		githubReleaseFixture("v1.1.0", false, "Attune-windows-amd64-1.1.0.zip", "SHA256SUMS.txt"),
		githubReleaseFixture("v1.3.0", false, "Attune-windows-amd64-1.3.0.zip", "SHA256SUMS.txt"),
		flaggedPrerelease,
	}
	client := releaseCheckClient(t, releases)
	got, ok, err := client.Check(context.Background(), "1.0.0", "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Version.String() != "1.3.0" {
		t.Fatalf("stable channel result = (%+v, %v)", got.Version, ok)
	}

	alphaClient := releaseCheckClient(t, []map[string]any{
		githubReleaseFixture("v1.2.0-alpha.2", false, "Attune-windows-amd64-1.2.0-alpha.2.zip", "SHA256SUMS.txt"),
		githubReleaseFixture("v1.2.0-alpha.10", false, "Attune-windows-amd64-1.2.0-alpha.10.zip", "SHA256SUMS.txt"),
		githubReleaseFixture("v1.2.0", false, "Attune-windows-amd64-1.2.0.zip", "SHA256SUMS.txt"),
	})
	got, ok, err = alphaClient.Check(context.Background(), "1.2.0-alpha.1", "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Version.String() != "1.2.0" {
		t.Fatalf("prerelease channel should accept newer stable release: (%+v, %v)", got.Version, ok)
	}
}

func TestCheckDoesNotSelectOtherPlatformOrMissingTrustedAssets(t *testing.T) {
	client := releaseCheckClient(t, []map[string]any{githubReleaseFixture("v2.0.0", false, "Attune-linux-amd64-2.0.0.tar.gz", "SHA256SUMS.txt")})
	if _, ok, err := client.Check(context.Background(), "1.0.0", "windows", "amd64"); err != nil || ok {
		t.Fatalf("wrong-platform assets selected: ok=%v err=%v", ok, err)
	}
}

func TestUpdateRedirectsRejectHTTPUserinfoAndUntrustedHosts(t *testing.T) {
	for _, target := range []string{
		"http://github.com/kevelopment/Attune/releases/download/file.zip",
		"https://user@github.com/kevelopment/Attune/releases/download/file.zip",
		"https://evil.example/kevelopment/Attune/releases/download/file.zip",
	} {
		client := NewClient().HTTP
		client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			h := make(http.Header)
			h.Set("Location", target)
			return &http.Response{StatusCode: http.StatusFound, Header: h, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		})
		_, err := client.Get("https://github.com/kevelopment/Attune/releases/download/file.zip")
		if err == nil {
			t.Errorf("redirect to %q was accepted", target)
		}
	}
}

func TestChecksumForValidBadMissingAndExactFilename(t *testing.T) {
	const filename = "Attune-linux-amd64-1.2.3.tar.gz"
	digest := strings.Repeat("a", 64)
	got, err := checksumFor([]byte(fmt.Sprintf("%s  other.tar.gz\n%s *%s\n", strings.Repeat("b", 64), digest, filename)), filename)
	if err != nil || got != digest {
		t.Fatalf("valid checksum = %q, %v", got, err)
	}
	for _, data := range [][]byte{
		[]byte(strings.Repeat("z", 64) + "  " + filename + "\n"),
		[]byte(strings.Repeat("a", 63) + "  " + filename + "\n"),
		[]byte(strings.Repeat("a", 64) + "  releases/" + filename + "\n"),
		[]byte(strings.Repeat("a", 64) + "  Attune-linux-arm64-1.2.3.tar.gz\n"),
	} {
		if _, err := checksumFor(data, filename); err == nil {
			t.Errorf("invalid/missing checksum accepted: %q", data)
		}
	}
}

func TestDownloadAndStageVerifiesChecksumAndKeepsOnlyValidatedPackage(t *testing.T) {
	archive := windowsFixtureZip(t)
	sum := sha256.Sum256(archive)
	filename := "Attune-windows-amd64-1.2.3.zip"
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + filename + "\n")
	client := stagedDownloadClient(archive, sums)
	version, _ := ParseVersion("1.2.3")
	release := Release{Version: version, Assets: map[string]string{
		filename:         "https://github.com/kevelopment/Attune/releases/download/v1.2.3/" + filename,
		"SHA256SUMS.txt": "https://github.com/kevelopment/Attune/releases/download/v1.2.3/SHA256SUMS.txt",
	}}
	cache := t.TempDir()
	staged, err := client.DownloadAndStage(context.Background(), release, "windows", "amd64", cache)
	if err != nil {
		t.Fatal(err)
	}
	if rel, err := filepath.Rel(cache, staged); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("staged path %q escaped cache %q", staged, cache)
	}
	for _, name := range []string{"Attune.exe", "kindling-cli.exe", "licenses/Attune-LICENSE.txt"} {
		if _, err := os.Stat(filepath.Join(staged, name)); err != nil {
			t.Errorf("validated staged package missing %s: %v", name, err)
		}
	}

	badCache := t.TempDir()
	badSums := []byte(strings.Repeat("0", 64) + "  " + filename + "\n")
	if _, err := stagedDownloadClient(archive, badSums).DownloadAndStage(context.Background(), release, "windows", "amd64", badCache); err == nil {
		t.Fatal("package with wrong checksum was staged")
	}
	entries, err := os.ReadDir(badCache)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed staging left cache artifacts: %+v", entries)
	}
}

func TestDownloadRejectsTruncatedResponseAndRemovesPartialFile(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "update.zip")
	client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        make(http.Header),
			Body:          io.NopCloser(strings.NewReader("partial")),
			ContentLength: int64(len("partial") + 1),
			Request:       req,
		}, nil
	})}}

	err := client.download(context.Background(), "https://github.com/kevelopment/Attune/releases/download/v1.2.3/file.zip", dest, nil)
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("download error = %v, want incomplete response error", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("partial download remains at %s: %v", dest, err)
	}
}

func releaseCheckClient(t *testing.T, releases []map[string]any) *Client {
	t.Helper()
	data, err := json.Marshal(releases)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/kevelopment/Attune/releases" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	return &Client{APIBase: server.URL}
}

func githubReleaseFixture(tag string, draft bool, names ...string) map[string]any {
	assets := make([]map[string]string, 0, len(names))
	for _, name := range names {
		assets = append(assets, map[string]string{"name": name, "browser_download_url": "https://github.com/kevelopment/Attune/releases/download/" + tag + "/" + name})
	}
	return map[string]any{"tag_name": tag, "html_url": "https://github.com/kevelopment/Attune/releases/tag/" + tag, "draft": draft, "assets": assets}
}

func githubReleaseFixtureWithURL(tag, packageURL, sumsURL string) map[string]any {
	return map[string]any{"tag_name": tag, "draft": false, "assets": []map[string]string{{"name": "Attune-windows-amd64-" + strings.TrimPrefix(tag, "v") + ".zip", "browser_download_url": packageURL}, {"name": "SHA256SUMS.txt", "browser_download_url": sumsURL}}}
}

func windowsFixtureZip(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, name := range []string{"Attune.exe", "kindling-cli.exe", "INSTALL.txt", "licenses/Attune-LICENSE.txt", "licenses/THIRD_PARTY_NOTICES.md", "licenses/THIRD_PARTY_LICENSES.txt", "licenses/Kindling-LICENSE.txt", "licenses/Kindling-THIRD_PARTY_LICENSES.txt"} {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte("fixture:" + name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func stagedDownloadClient(archive, sums []byte) *Client {
	return &Client{HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body []byte
		switch {
		case strings.HasSuffix(req.URL.Path, "SHA256SUMS.txt"):
			body = sums
		case strings.HasSuffix(req.URL.Path, ".zip"):
			body = archive
		default:
			return nil, fmt.Errorf("unexpected request %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req}, nil
	})}}
}
