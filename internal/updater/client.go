package updater

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	repository       = "kevelopment/Attune"
	maxAPIBytes      = 8 << 20
	maxDownloadBytes = 1 << 30
)

type Release struct {
	Version Version
	URL     string
	Notes   string
	Assets  map[string]string
}

type Client struct {
	HTTP     *http.Client
	APIBase  string
	Progress func(received, total int64)
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("too many update download redirects")
		}
		if req.URL.Scheme != "https" || req.URL.User != nil || (req.URL.Port() != "" && req.URL.Port() != "443") {
			return errors.New("update redirect must remain on HTTPS")
		}
		switch req.URL.Hostname() {
		case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
			return nil
		default:
			return fmt.Errorf("update download redirected to an untrusted host: %s", req.URL.Hostname())
		}
	}}, APIBase: "https://api.github.com"}
}

type githubRelease struct {
	Tag        string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (c *Client) Check(ctx context.Context, current string, goos, goarch string) (Release, bool, error) {
	installed, err := ParseVersion(current)
	if err != nil {
		return Release{}, false, fmt.Errorf("parse installed version: %w", err)
	}
	base, err := url.Parse(c.APIBase)
	if err != nil {
		return Release{}, false, err
	}
	loopbackTest := base.Scheme == "http" && (strings.HasPrefix(base.Host, "127.0.0.1:") || strings.HasPrefix(base.Host, "localhost:"))
	if (base.Scheme != "https" && !loopbackTest) || base.User != nil || (base.Port() != "" && base.Port() != "443" && !loopbackTest) {
		return Release{}, false, errors.New("release API must use HTTPS")
	}
	if !loopbackTest && !strings.EqualFold(base.Hostname(), "api.github.com") {
		return Release{}, false, errors.New("release API host is not trusted")
	}
	var best Release
	found := false
	for page := 1; page <= 10; page++ {
		u := *base
		u.Path = strings.TrimRight(base.Path, "/") + "/repos/" + repository + "/releases"
		q := u.Query()
		q.Set("per_page", "100")
		q.Set("page", fmt.Sprint(page))
		u.RawQuery = q.Encode()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		res, err := c.httpClient().Do(req)
		if err != nil {
			return Release{}, false, err
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return Release{}, false, fmt.Errorf("GitHub release check returned HTTP %d", res.StatusCode)
		}
		data, readErr := io.ReadAll(io.LimitReader(res.Body, maxAPIBytes+1))
		closeErr := res.Body.Close()
		if readErr != nil {
			return Release{}, false, readErr
		}
		if closeErr != nil {
			return Release{}, false, closeErr
		}
		if len(data) > maxAPIBytes {
			return Release{}, false, errors.New("GitHub release response is too large")
		}
		var pageReleases []githubRelease
		if err := json.Unmarshal(data, &pageReleases); err != nil {
			return Release{}, false, fmt.Errorf("decode GitHub releases: %w", err)
		}
		if len(pageReleases) == 0 {
			break
		}
		for _, item := range pageReleases {
			if item.Draft {
				continue
			}
			v, err := ParseVersion(item.Tag)
			if err != nil {
				continue
			}
			if (item.Prerelease || v.IsPrerelease()) && !installed.IsPrerelease() {
				continue
			}
			if v.Compare(installed) <= 0 || (found && v.Compare(best.Version) <= 0) {
				continue
			}
			assets := make(map[string]string, len(item.Assets))
			for _, asset := range item.Assets {
				if trustedAssetURL(asset.URL) {
					assets[asset.Name] = asset.URL
				}
			}
			assetName := PackageAssetName(goos, goarch, v.String())
			if assetName == "" || assets[assetName] == "" || assets["SHA256SUMS.txt"] == "" {
				continue
			}
			best = Release{Version: v, URL: item.HTMLURL, Notes: item.Body, Assets: assets}
			found = true
		}
		if len(pageReleases) < 100 {
			break
		}
	}
	return best, found, nil
}

func PackageAssetName(goos, goarch, version string) string {
	switch goos {
	case "darwin":
		return fmt.Sprintf("Attune-macos-%s-%s.zip", goarch, version)
	case "windows":
		return fmt.Sprintf("Attune-windows-%s-%s.zip", goarch, version)
	case "linux":
		return fmt.Sprintf("Attune-linux-%s-%s.tar.gz", goarch, version)
	default:
		return ""
	}
}

func trustedAssetURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	return strings.EqualFold(u.Hostname(), "github.com") && strings.HasPrefix(strings.ToLower(u.Path), "/kevelopment/attune/releases/download/")
}

func (c *Client) DownloadAndStage(ctx context.Context, release Release, goos, goarch, cacheRoot string) (string, error) {
	name := PackageAssetName(goos, goarch, release.Version.String())
	assetURL, sumsURL := release.Assets[name], release.Assets["SHA256SUMS.txt"]
	if !trustedAssetURL(assetURL) || !trustedAssetURL(sumsURL) {
		return "", errors.New("release is missing trusted package/checksum URLs")
	}
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(cacheRoot, "update-"+release.Version.String()+"-")
	if err != nil {
		return "", err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	archive := filepath.Join(dir, name)
	var total int64
	if err := c.download(ctx, assetURL, archive, &total); err != nil {
		return "", err
	}
	checksums, err := c.get(ctx, sumsURL, 1<<20)
	if err != nil {
		return "", err
	}
	expected, err := checksumFor(checksums, name)
	if err != nil {
		return "", err
	}
	actual, err := hashFile(archive)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(expected, actual) {
		return "", errors.New("update package SHA-256 does not match SHA256SUMS.txt")
	}
	packageDir := filepath.Join(dir, "package")
	if err := os.Mkdir(packageDir, 0o700); err != nil {
		return "", err
	}
	if err := ExtractPackage(archive, packageDir, goos, goarch); err != nil {
		return "", err
	}
	if err := ValidatePackage(packageDir, goos); err != nil {
		return "", err
	}
	keep = true
	return packageDir, nil
}

func (c *Client) get(ctx context.Context, raw string, limit int64) ([]byte, error) {
	if !trustedAssetURL(raw) {
		return nil, errors.New("refusing untrusted update URL")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	res, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update server returned HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("update response exceeds size limit")
	}
	return b, nil
}

func (c *Client) download(ctx context.Context, raw, dest string, overall *int64) error {
	if !trustedAssetURL(raw) {
		return errors.New("refusing untrusted update URL")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	res, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("update package download returned HTTP %d", res.StatusCode)
	}
	if res.ContentLength > maxDownloadBytes {
		return errors.New("update package exceeds size limit")
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	reader := &progressReader{r: io.LimitReader(res.Body, maxDownloadBytes+1), total: res.ContentLength, callback: c.Progress, overall: overall}
	n, err := io.Copy(f, reader)
	if err != nil {
		return err
	}
	if n > maxDownloadBytes {
		return errors.New("update package exceeds size limit")
	}
	return f.Sync()
}

type progressReader struct {
	r        io.Reader
	total    int64
	callback func(int64, int64)
	received int64
	overall  *int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, e := p.r.Read(b)
	p.received += int64(n)
	if p.overall != nil {
		*p.overall = p.received
	}
	if p.callback != nil {
		p.callback(p.received, p.total)
	}
	return n, e
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return NewClient().HTTP
}

func checksumFor(data []byte, filename string) (string, error) {
	s := bufio.NewScanner(strings.NewReader(string(data)))
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 2 {
			continue
		}
		name := fields[len(fields)-1]
		if name == "*"+filename {
			name = filename
		}
		if name == filename && len(fields[0]) == 64 {
			if _, err := hex.DecodeString(fields[0]); err == nil {
				return fields[0], nil
			}
		}
	}
	if err := s.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("SHA256SUMS.txt has no checksum for %s", filename)
}
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func Platform() (string, string) { return runtime.GOOS, runtime.GOARCH }
