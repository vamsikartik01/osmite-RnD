// Package platform holds everything that differs between operating systems
// apart from the PTY itself: where files live, which shell to start, and how
// to launch the daemon in the background.
//
// OS-specific code lives in platform_windows.go and platform_unix.go. Keep
// this package small; everything else in otmux should be OS-agnostic.
package platform

import (
	"os"
	"path/filepath"
)

// SocketPath is where the daemon listens. OTMUX_SOCKET overrides it, which
// is handy for running a second, isolated daemon while developing.
func SocketPath() string {
	if p := os.Getenv("OTMUX_SOCKET"); p != "" {
		return p
	}
	return filepath.Join(runtimeDir(), "otmux.sock")
}

// LogPath is where the background daemon writes its log: next to the socket,
// so an isolated OTMUX_SOCKET daemon gets its own log too.
func LogPath() string {
	return filepath.Join(filepath.Dir(SocketPath()), "otmux.log")
}

// EnsureRuntimeDir creates the directory holding the socket and log, readable
// only by the current user.
func EnsureRuntimeDir() error {
	return os.MkdirAll(filepath.Dir(SocketPath()), 0o700)
}

// Shell returns the command line for a new pane. OTMUX_SHELL overrides the
// platform default.
func Shell() []string {
	if s := os.Getenv("OTMUX_SHELL"); s != "" {
		return []string{s}
	}
	return defaultShell()
}

// CanOpenBrowser reports whether OpenURL is likely to reach the user: not
// over SSH, and on Linux only with a graphical session.
func CanOpenBrowser() bool {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return false
	}
	return canOpenBrowser()
}
