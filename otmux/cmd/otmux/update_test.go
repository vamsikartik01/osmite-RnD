package main

import (
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/update"
)

// TestTUIUpdate runs otmux against a fake release announcing version 9.9.9
// (whose binary is a copy of this build, so the swapped-in file still
// works). It checks the automatic update at start-up, the status bar
// notice, and Settings › Updates with its "Update now" button.
func TestTUIUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the real binary")
	}
	dir := shortTempDir(t)
	bin := filepath.Join(dir, "otmux")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	// Built like a release (only release builds update themselves).
	ldflags := "-X github.com/vamsikartik01/osmite-RnD/otmux/internal/version.Release=true"
	if out, err := exec.Command("go", "build", "-ldflags", ldflags, "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	payload, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	mux := http.NewServeMux()
	mux.HandleFunc("/version.txt", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "9.9.9") })
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), update.AssetName())
	})
	mux.HandleFunc("/"+update.AssetName(), func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	shell := "/bin/sh"
	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
	}
	env := []string{
		"OTMUX_SOCKET=" + filepath.Join(dir, "s.sock"), "OTMUX_SHELL=" + shell, "OTMUX_PANE=",
		"OTMUX_CONFIG=" + filepath.Join(dir, "config.json"), "OTMUX_PREFIX=", "OTMUX_NO_UPDATE=",
		"OTMUX_UPDATE_URL=" + srv.URL,
	}
	t.Cleanup(func() { runOtmux(t, env, bin, "kill-server") })
	term := startTermArgs(t, bin, []string{"new", "up"}, env, 110, 30)

	// The automatic check installs 9.9.9 and says so in the status bar.
	term.waitFor(t, "update notice", func(s string) bool { return strings.Contains(s, "9.9.9 installed") })
	if st := update.LoadState(dir); st.Installed != "9.9.9" || st.Latest != "9.9.9" {
		t.Fatalf("update state after the automatic check: %+v", st)
	}
	if out := runOtmux(t, env, bin, "version"); !strings.Contains(out, "otmux ") {
		t.Fatalf("swapped-in program doesn't run: %q", out)
	}

	// Clicking the notice opens Settings › Updates.
	term.clickStatus(t, "9.9.9 installed")
	term.waitFor(t, "updates section", func(s string) bool {
		return strings.Contains(s, "Auto update") && strings.Contains(s, "Update now") && strings.Contains(s, "Latest     9.9.9")
	})

	// Update now installs it again and reports it.
	term.click(t, "Update now")
	term.waitFor(t, "update now", func(s string) bool { return strings.Contains(s, "otmux 9.9.9 is installed") })
	t.Logf("settings › updates:\n%s", term.screen())

	// Turning auto update off is saved. (Wait out the double-click interval
	// first, or the console may merge this click with the previous one.)
	time.Sleep(700 * time.Millisecond)
	term.click(t, "Auto update")
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, _ := os.ReadFile(filepath.Join(dir, "config.json"))
		if strings.Contains(string(data), `"auto_update": false`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("settings after turning auto update off: %s\nscreen:\n%s", data, term.screen())
		}
		time.Sleep(50 * time.Millisecond)
	}
}
