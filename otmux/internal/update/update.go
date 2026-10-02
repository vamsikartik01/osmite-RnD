// Package update keeps otmux current. It reads the newest version from the
// rolling otmux/latest GitHub release, downloads the matching binary,
// checks it against the release's SHA-256 checksums, and swaps it in for
// the running program file. Nothing is restarted: running shells stay up,
// and the new version is used from the next start.
package update

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
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is where releases are downloaded from. OTMUX_UPDATE_URL
// overrides it (used by tests).
const DefaultBaseURL = "https://github.com/vamsikartik01/osmite-RnD/releases/download/otmux/latest"

// CheckInterval is how often automatic updates look for a new version.
const CheckInterval = 24 * time.Hour

// ErrNotManaged means this copy of otmux shouldn't update itself: it's a dev
// build, it was installed by a package manager, or its folder isn't writable.
var ErrNotManaged = errors.New("this copy of otmux is managed elsewhere")

func baseURL() string {
	if u := os.Getenv("OTMUX_UPDATE_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return DefaultBaseURL
}

// Disabled reports whether OTMUX_NO_UPDATE turns automatic updates off.
func Disabled() bool { return os.Getenv("OTMUX_NO_UPDATE") != "" }

var client = &http.Client{Timeout: 60 * time.Second}

func get(ctx context.Context, name string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL()+"/"+name, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download %s: %s", name, resp.Status)
	}
	return resp.Body, nil
}

// Latest returns the newest released version, e.g. "1.1.0".
func Latest(ctx context.Context) (string, error) {
	body, err := get(ctx, "version.txt")
	if err != nil {
		return "", err
	}
	defer body.Close()
	b, err := io.ReadAll(io.LimitReader(body, 64))
	if err != nil {
		return "", err
	}
	v := strings.TrimPrefix(strings.TrimSpace(string(b)), "v")
	if _, ok := parse(v); !ok {
		return "", fmt.Errorf("bad version %q", v)
	}
	return v, nil
}

// parse splits "1.2.3" into numbers.
func parse(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Newer reports whether version a is newer than b. Unparsable versions
// (like "dev") are never newer and never older.
func Newer(a, b string) bool {
	va, oka := parse(a)
	vb, okb := parse(b)
	if !oka || !okb {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

// AssetName is the release file for this platform.
func AssetName() string {
	name := "otmux-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// Executable returns the running program's path.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// Managed reports whether the program at exe may update itself.
func Managed(exe, version string) error {
	if _, ok := parse(version); !ok {
		return fmt.Errorf("%w: development build", ErrNotManaged)
	}
	lower := strings.ToLower(filepath.ToSlash(exe))
	for _, marker := range []string{"/scoop/", "/winget/", "/program files", "/cellar/", "/homebrew/", "/usr/bin/", "/usr/local/bin/", "/nix/store/", "/snap/"} {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("%w: installed by a package manager", ErrNotManaged)
		}
	}
	f, err := os.CreateTemp(filepath.Dir(exe), ".otmux-write-test-*")
	if err != nil {
		return fmt.Errorf("%w: %s isn't writable", ErrNotManaged, filepath.Dir(exe))
	}
	f.Close()
	os.Remove(f.Name())
	return nil
}

// Install downloads the latest release and swaps it in for the program at
// exe. The running program keeps working; the new one is used next time.
func Install(ctx context.Context, exe string) error {
	dir := filepath.Dir(exe)
	asset := AssetName()

	sums, err := checksums(ctx)
	if err != nil {
		return err
	}
	want, ok := sums[asset]
	if !ok {
		return fmt.Errorf("checksums.txt has no entry for %s", asset)
	}

	tmp, err := os.CreateTemp(dir, ".otmux-new-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once it's been renamed into place
	body, err := get(ctx, asset)
	if err != nil {
		tmp.Close()
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, h), body)
	body.Close()
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch for %s (expected %s, got %s)", asset, want, got)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return swap(tmp.Name(), exe)
}

// swap puts new in place of exe. Windows can't overwrite a running
// program but can rename it, so the old one is moved aside first.
func swap(newPath, exe string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(newPath, exe)
	}
	old := filepath.Join(filepath.Dir(exe), fmt.Sprintf("otmux.old-%d.exe", time.Now().UnixNano()))
	if err := os.Rename(exe, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("move the old otmux aside: %w", err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(old, exe) // put the old one back
		return err
	}
	return nil
}

// Cleanup removes old copies left by earlier updates. Copies still in use
// by a running otmux can't be removed yet and are tried again next time.
func Cleanup(exe string) {
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), "otmux.old-*.exe"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// checksums downloads checksums.txt as a map of file name to SHA-256.
func checksums(ctx context.Context) (map[string]string, error) {
	body, err := get(ctx, "checksums.txt")
	if err != nil {
		return nil, err
	}
	defer body.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(io.LimitReader(body, 1<<20))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 {
			out[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	return out, sc.Err()
}

// State remembers when updates were last checked, and the result.
type State struct {
	LastCheck time.Time `json:"last_check"`
	Latest    string    `json:"latest,omitempty"`
	Installed string    `json:"installed,omitempty"` // version swapped in, awaiting restart
}

// LoadState reads the state file in dir.
func LoadState(dir string) State {
	var s State
	if b, err := os.ReadFile(filepath.Join(dir, "update.json")); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// Save writes the state file in dir.
func (s State) Save(dir string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "update.json"), b, 0o600)
}

// Due reports whether an automatic check should run now.
func (s State) Due(now time.Time) bool { return now.Sub(s.LastCheck) >= CheckInterval }

// Result describes what Run did.
type Result struct {
	Current, Latest string
	Installed       bool
}

// Run checks for a newer version and installs it into exe.
func Run(ctx context.Context, exe, current string) (Result, error) {
	r := Result{Current: current}
	if err := Managed(exe, current); err != nil {
		return r, err
	}
	latest, err := Latest(ctx)
	if err != nil {
		return r, err
	}
	r.Latest = latest
	if !Newer(latest, current) {
		return r, nil
	}
	if err := Install(ctx, exe); err != nil {
		return r, err
	}
	r.Installed = true
	return r, nil
}
