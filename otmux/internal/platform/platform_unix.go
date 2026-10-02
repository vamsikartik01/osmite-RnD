//go:build !windows

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
)

func runtimeDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "otmux")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("otmux-%d", os.Getuid()))
}

func defaultShell() []string {
	if s := os.Getenv("SHELL"); s != "" {
		return []string{s}
	}
	return []string{"/bin/sh"}
}

// Detach configures cmd to run in its own session, outliving the parent.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func canOpenBrowser() bool {
	return runtime.GOOS == "darwin" || os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// OpenURL opens url in the default browser, in the background.
func OpenURL(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
