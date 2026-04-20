// Package updater implements online self-update from GitHub releases.
//
// Release artifacts are expected to follow the convention used by the
// install.sh script in the repo root:
//
//	https://github.com/<repo>/releases/download/<tag>/ccmate-<os>-<arch>.tar.gz
//
// containing an executable named `ccmate-<os>-<arch>`.
package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// restartPending is set after a successful Apply() so that the main loop
// can exit with a non-zero code, which makes systemd relaunch the service
// under both Restart=on-failure and Restart=always.
var restartPending atomic.Bool

// RestartPending reports whether an online update completed and the
// process should exit non-zero on shutdown so systemd picks up the new
// binary.
func RestartPending() bool { return restartPending.Load() }

const (
	defaultRepo = "cloverstd/ccmate"
	apiBase     = "https://api.github.com"
)

// Release mirrors the subset of GitHub's release payload we expose to the UI.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []Asset   `json:"assets"`
}

// Asset is a single downloadable file in a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int    `json:"size"`
}

// Info describes the currently-installed build and, when available, the
// newest release on GitHub so the UI can render an "update available" banner.
type Info struct {
	CurrentVersion  string   `json:"current_version"`
	LatestVersion   string   `json:"latest_version,omitempty"`
	UpdateAvailable bool     `json:"update_available"`
	Platform        string   `json:"platform"`
	AssetName       string   `json:"asset_name"`
	Supported       bool     `json:"supported"`
	ReleaseURL      string   `json:"release_url,omitempty"`
	Error           string   `json:"error,omitempty"`
}

// Updater encapsulates version lookup and self-replacement.
type Updater struct {
	Repo    string
	Current string
	HTTP    *http.Client
}

// New builds an Updater for the given running binary version.
func New(current string) *Updater {
	return &Updater{
		Repo:    defaultRepo,
		Current: current,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// PlatformAsset returns the asset base name for the current OS/ARCH that
// matches the convention in install.sh, e.g. "ccmate-linux-amd64".
func PlatformAsset() string {
	return fmt.Sprintf("ccmate-%s-%s", runtime.GOOS, runtime.GOARCH)
}

// PlatformSupported reports whether the current OS/ARCH has prebuilt
// release artifacts (matches install.sh's supported matrix).
func PlatformSupported() bool {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return false
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return false
	}
	return true
}

// ListReleases returns up to 30 most recent non-draft releases.
func (u *Updater) ListReleases(ctx context.Context) ([]Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=30", apiBase, u.Repo)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("github returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var all []Release
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return nil, err
	}
	out := make([]Release, 0, len(all))
	for _, r := range all {
		if r.Draft {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// Latest returns the newest stable (non-prerelease) release, falling back to
// the most recent prerelease if no stable exists.
func (u *Updater) Latest(ctx context.Context) (*Release, error) {
	releases, err := u.ListReleases(ctx)
	if err != nil {
		return nil, err
	}
	for i := range releases {
		if !releases[i].Prerelease {
			return &releases[i], nil
		}
	}
	if len(releases) > 0 {
		return &releases[0], nil
	}
	return nil, nil
}

// BuildInfo assembles the Info struct for the UI.
func (u *Updater) BuildInfo(ctx context.Context) Info {
	info := Info{
		CurrentVersion: u.Current,
		Platform:       runtime.GOOS + "/" + runtime.GOARCH,
		AssetName:      PlatformAsset(),
		Supported:      PlatformSupported(),
	}
	latest, err := u.Latest(ctx)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	if latest != nil {
		info.LatestVersion = latest.TagName
		info.ReleaseURL = latest.HTMLURL
		info.UpdateAvailable = info.Supported && IsNewer(u.Current, latest.TagName)
	}
	return info
}

// IsNewer reports whether `candidate` is a strictly newer version than
// `current`. Versions are compared using a best-effort semver parse
// (leading "v" stripped, pre-release suffix ignored). A "dev" current
// version is treated as older than any released tag.
func IsNewer(current, candidate string) bool {
	if current == "" || current == "dev" {
		return candidate != "" && candidate != "dev"
	}
	if candidate == "" || candidate == "dev" {
		return false
	}
	if strings.EqualFold(strings.TrimPrefix(current, "v"), strings.TrimPrefix(candidate, "v")) {
		return false
	}
	cp := parseSemver(current)
	np := parseSemver(candidate)
	for i := 0; i < 3; i++ {
		if np[i] != cp[i] {
			return np[i] > cp[i]
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	var out [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		n, _ := strconv.Atoi(parts[i])
		out[i] = n
	}
	return out
}

// Apply downloads the release tarball for the current platform, extracts
// the binary, replaces the running executable and returns. The caller is
// expected to exit the process shortly after so the service manager
// (systemd, docker, etc.) can start the new binary.
func (u *Updater) Apply(ctx context.Context, tag string) error {
	if !PlatformSupported() {
		return fmt.Errorf("platform %s/%s is not supported for online update", runtime.GOOS, runtime.GOARCH)
	}
	if strings.TrimSpace(tag) == "" {
		return fmt.Errorf("tag is required")
	}

	assetName := PlatformAsset()
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s.tar.gz", u.Repo, tag, assetName)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	dl := &http.Client{Timeout: 5 * time.Minute}
	resp, err := dl.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("download returned %d (url=%s)", resp.StatusCode, url)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	// Extract the target binary into a temp file next to the executable.
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".ccmate-new-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		// Best-effort cleanup if we bail out before the rename.
		if _, statErr := os.Stat(tmpPath); statErr == nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := extractBinary(resp.Body, assetName, tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}

	// On POSIX, rename over the running executable is permitted: the kernel
	// keeps the open text file alive until the process exits.
	if err := os.Rename(tmpPath, exe); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	restartPending.Store(true)
	return nil
}

// ScheduleRestart sends SIGTERM to ourselves after `delay` so the HTTP
// response can flush, main() can drain running agents, and then the
// process exits with a non-zero code (see RestartPending) — that triggers
// systemd to relaunch with the newly-installed binary.
func ScheduleRestart(delay time.Duration) {
	go func() {
		time.Sleep(delay)
		if p, err := os.FindProcess(os.Getpid()); err == nil {
			_ = p.Signal(syscall.SIGTERM)
		}
	}()
}

// extractBinary reads a ccmate release tarball from `src` and writes the
// binary matching `name` to `dst`. The tarball typically contains a single
// file named like "ccmate-linux-amd64".
func extractBinary(src io.Reader, name string, dst io.Writer) error {
	gz, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base := filepath.Base(hdr.Name)
		if base == name || base == "ccmate" {
			if _, err := io.Copy(dst, tr); err != nil {
				return fmt.Errorf("write binary: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("binary %q not found in archive", name)
}

// Verify returns nil if the running binary looks like a ccmate build (used
// as a defensive check before self-replace). Currently a no-op but kept to
// document intent for future signature verification.
func (u *Updater) Verify() error {
	_, err := exec.LookPath(filepath.Base(os.Args[0]))
	_ = err // intentionally ignored; presence in PATH is not required.
	return nil
}
