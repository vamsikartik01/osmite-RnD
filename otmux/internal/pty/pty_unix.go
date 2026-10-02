//go:build !windows

package pty

import (
	"io"

	"github.com/charmbracelet/x/xpty"
)

// masterIO returns the PTY master for reading and writing. On Unix it is
// already an *os.File, which is safe to close while a read is in flight.
func masterIO(p xpty.Pty) (io.ReadCloser, io.WriteCloser, error) {
	return nopCloser{p}, nopCloser{p}, nil
}

// nopCloser leaves closing the master to the PTY itself.
type nopCloser struct{ io.ReadWriter }

func (nopCloser) Close() error { return nil }
