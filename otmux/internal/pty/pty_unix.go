//go:build !windows

package pty

import (
	"io"
	"os/exec"
	"syscall"

	"github.com/charmbracelet/x/xpty"
)

// xpty starts the shell without a controlling terminal, so job control is
// off and Ctrl+C never reaches the foreground program. Start it in a session
// of its own with the PTY (its stdin, fd 0) as the controlling terminal.
func init() {
	prepareCmd = func(cmd *exec.Cmd) {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	}
}

// masterIO returns the PTY master for reading and writing. On Unix it is
// already an *os.File, which is safe to close while a read is in flight.
func masterIO(p xpty.Pty) (io.ReadCloser, io.WriteCloser, error) {
	return nopCloser{p}, nopCloser{p}, nil
}

// nopCloser leaves closing the master to the PTY itself.
type nopCloser struct{ io.ReadWriter }

func (nopCloser) Close() error { return nil }
