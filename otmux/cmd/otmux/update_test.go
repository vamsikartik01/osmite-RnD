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
// notice and the update panel it opens, Settings › Updates with its
// "Update now" button, and restarting from the panel.
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

	// Clicking the notice opens the update panel; Later closes it.
	term.clickStatus(t, "9.9.9 installed")
	term.waitFor(t, "update panel", func(s string) bool {
		return strings.Contains(s, "otmux 9.9.9 is installed") && strings.Contains(s, "What's new") &&
			strings.Contains(s, "Restart now") && strings.Contains(s, "otmux restart")
	})
	t.Logf("update panel:\n%s", term.screen())
	term.click(t, "Later")
	term.waitFor(t, "panel closed", func(s string) bool { return !strings.Contains(s, "What's new") })

	// Settings › Updates.
	term.typ("\x02s")
	term.waitFor(t, "settings", func(s string) bool { return strings.Contains(s, "Updates") })
	term.click(t, "Updates")
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

	// Restart now asks first; Cancel goes back without closing anything.
	term.typ("\x1b")
	term.waitFor(t, "settings closed", func(s string) bool { return !strings.Contains(s, "Auto update") })
	time.Sleep(700 * time.Millisecond)
	term.clickStatus(t, "9.9.9 installed")
	term.waitFor(t, "update panel again", func(s string) bool { return strings.Contains(s, "Restart now") })
	term.click(t, "Restart now")
	term.waitFor(t, "confirmation", func(s string) bool {
		return strings.Contains(s, "Yes, restart") && strings.Contains(s, "Close 1 tab and restart otmux?")
	})
	time.Sleep(700 * time.Millisecond)
	term.click(t, "Cancel")
	term.waitFor(t, "cancelled", func(s string) bool { return strings.Contains(s, "Restart now") })
	if out := runOtmux(t, env, bin, "ls"); !strings.Contains(out, "up") {
		t.Fatalf("cancel closed the workspace: ls = %q", out)
	}

	// Yes, restart: otmux stops the daemon and opens again on a fresh one
	// from the installed program, in the same terminal.
	time.Sleep(700 * time.Millisecond)
	term.click(t, "Restart now")
	term.waitFor(t, "confirmation again", func(s string) bool { return strings.Contains(s, "Yes, restart") })
	time.Sleep(700 * time.Millisecond)
	term.click(t, "Yes, restart")
	term.waitFor(t, "reopened", func(s string) bool { return currentWS(s, "default") })
	if out := runOtmux(t, env, bin, "ls"); strings.Contains(out, "up") || !strings.Contains(out, "default") {
		t.Fatalf("after restarting from the panel, ls = %q", out)
	}
}
