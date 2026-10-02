package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	official = func() bool { return true } // tests stand in for release builds
	os.Exit(m.Run())
}

func TestSourceBuildsDontUpdate(t *testing.T) {
	official = func() bool { return false }
	defer func() { official = func() bool { return true } }()
	exe := filepath.Join(t.TempDir(), "otmux.exe")
	_ = os.WriteFile(exe, []byte("old"), 0o755)
	if _, err := Run(context.Background(), exe, "1.0.0"); !errors.Is(err, ErrNotManaged) {
		t.Fatalf("source build: err = %v, want ErrNotManaged", err)
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.0.1", "1.0.0", true},
		{"1.1.0", "1.0.9", true},
		{"2.0.0", "1.9.9", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.1", false},
		{"v1.2.0", "1.1.0", true},
		{"1.2.0", "dev", false},
		{"garbage", "1.0.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// release serves a fake otmux/latest release.
func release(t *testing.T, version string, binary []byte, tamper bool) {
	t.Helper()
	sum := sha256.Sum256(binary)
	if tamper {
		binary = append(append([]byte{}, binary...), 'x')
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/version.txt", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, version) })
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), AssetName())
	})
	mux.HandleFunc("/"+AssetName(), func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(binary) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("OTMUX_UPDATE_URL", srv.URL)
}

func TestRunInstallsNewerVersion(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "otmux.exe")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	release(t, "1.1.0", []byte("new build"), false)

	r, err := Run(context.Background(), exe, "1.0.0")
	if err != nil || !r.Installed || r.Latest != "1.1.0" {
		t.Fatalf("Run = %+v, %v", r, err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new build" {
		t.Fatalf("program file has %q", b)
	}

	// Same version: nothing to do.
	r, err = Run(context.Background(), exe, "1.1.0")
	if err != nil || r.Installed {
		t.Fatalf("up to date: Run = %+v, %v", r, err)
	}
}

func TestRunRejectsTamperedDownload(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "otmux.exe")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	release(t, "1.1.0", []byte("new build"), true)

	if _, err := Run(context.Background(), exe, "1.0.0"); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("tampered download: err = %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatalf("program file changed to %q after a failed update", b)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".otmux-new-*")); len(left) != 0 {
		t.Fatalf("left temp files behind: %v", left)
	}
}

func TestDevBuildsDontUpdate(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "otmux.exe")
	_ = os.WriteFile(exe, []byte("old"), 0o755)
	if _, err := Run(context.Background(), exe, "dev"); !errors.Is(err, ErrNotManaged) {
		t.Fatalf("dev build: err = %v, want ErrNotManaged", err)
	}
}

// The real case: replacing the program file while that program is running,
// which Windows only allows by moving the running file aside first.
func TestInstallOverRunningProgram(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a helper program")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "sleeper", "main.go")
	_ = os.MkdirAll(filepath.Dir(src), 0o700)
	_ = os.WriteFile(src, []byte("package main\n\nimport \"time\"\n\nfunc main() { time.Sleep(time.Minute) }\n"), 0o600)
	exe := filepath.Join(dir, "otmux")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	build := exec.Command("go", "build", "-o", exe, src)
	build.Env = append(os.Environ(), "GO111MODULE=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	running := exec.Command(exe)
	if err := running.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = running.Process.Kill(); _ = running.Wait() })
	time.Sleep(200 * time.Millisecond)

	release(t, "1.1.0", []byte("new build"), false)
	if err := Install(context.Background(), exe); err != nil {
		t.Fatalf("install over a running program: %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new build" {
		t.Fatalf("program file has %q", b)
	}

	// Once the old program exits, Cleanup removes its moved-aside copy.
	_ = running.Process.Kill()
	_ = running.Wait()
	time.Sleep(200 * time.Millisecond)
	Cleanup(exe)
	if left, _ := filepath.Glob(filepath.Join(dir, "otmux.old-*")); len(left) != 0 {
		t.Fatalf("old copies left: %v", left)
	}
}

func TestStateDue(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if !LoadState(dir).Due(now) {
		t.Fatal("first run should be due")
	}
	if err := (State{LastCheck: now, Latest: "1.0.0"}).Save(dir); err != nil {
		t.Fatal(err)
	}
	s := LoadState(dir)
	if s.Due(now.Add(time.Hour)) || !s.Due(now.Add(25*time.Hour)) || s.Latest != "1.0.0" {
		t.Fatalf("state %+v", s)
	}
}
