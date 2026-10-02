//go:build !windows

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
