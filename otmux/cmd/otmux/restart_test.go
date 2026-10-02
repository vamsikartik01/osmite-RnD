package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRestart: otmux restart stops the daemon (closing its workspaces) and
// opens otmux again on a fresh one. Without a terminal to ask on, it wants
// -y; inside otmux it refuses.
func TestRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the real binary")
	}
	dir := shortTempDir(t)
	bin := filepath.Join(dir, "otmux")
	shell := "/bin/sh"
	if runtime.GOOS == "windows" {
		bin += ".exe"
		shell = "cmd.exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	env := []string{
		"OTMUX_SOCKET=" + filepath.Join(dir, "s.sock"), "OTMUX_SHELL=" + shell, "OTMUX_PANE=",
		"OTMUX_CONFIG=" + filepath.Join(dir, "config.json"), "OTMUX_PREFIX=", "OTMUX_NO_UPDATE=1",
	}
	t.Cleanup(func() { runOtmux(t, env, bin, "kill-server") })

	first := startTerm(t, bin, env, 100, 30) // workspace "smoke"
	first.waitFor(t, "first client", func(s string) bool { return currentWS(s, "smoke") })

	// No terminal to ask on: refuse rather than close shells unasked.
	cmd := exec.Command(bin, "restart")
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "restart -y") {
		t.Fatalf("restart without a terminal: err=%v, output %q", err, out)
	}
	if out := runOtmux(t, env, bin, "ls"); !strings.Contains(out, "smoke") {
		t.Fatalf("smoke should still run after a refused restart: %q", out)
	}

	// Inside otmux: refuse.
	cmd = exec.Command(bin, "restart", "-y")
	cmd.Env = append(append(os.Environ(), env...), "OTMUX_PANE=1")
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "inside otmux") {
		t.Fatalf("restart inside otmux: err=%v, output %q", err, out)
	}

	// -y: the old daemon stops (the first client is told), and a new client
	// opens on a fresh daemon with only the default workspace.
	second := startTermArgs(t, bin, []string{"restart", "-y"}, env, 100, 30)
	second.waitFor(t, "reopened", func(s string) bool { return currentWS(s, "default") })
	<-first.exited
	if out := runOtmux(t, env, bin, "ls"); strings.Contains(out, "smoke") || !strings.Contains(out, "default") {
		t.Fatalf("after restart, ls = %q", out)
	}
}
