// Package pty starts processes attached to a pseudo-terminal: ConPTY on
// Windows, a classic PTY on Linux and macOS. The OS differences are handled by
// charmbracelet/x/xpty; this package gives the rest of otmux one small
// interface so the backend can be swapped without touching the daemon.
package pty

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"github.com/charmbracelet/x/xpty"
)

// PTY is a running process attached to a pseudo-terminal.
type PTY interface {
	io.ReadWriteCloser
	// Resize changes the terminal size seen by the process.
	Resize(cols, rows int) error
	// Wait blocks until the process exits.
	Wait() error
	// Kill terminates the process.
	Kill() error
	// Pid is the process ID of the program started on the PTY.
	Pid() int
}

// Options describes the process to start.
type Options struct {
	Argv []string
	Dir  string
	Env  []string // added on top of the daemon's environment
	Cols int
	Rows int
}

// Start launches opts.Argv on a new pseudo-terminal.
func Start(opts Options) (PTY, error) {
	if len(opts.Argv) == 0 {
		return nil, fmt.Errorf("pty: empty command")
	}
	p, err := xpty.NewPty(opts.Cols, opts.Rows)
	if err != nil {
		return nil, fmt.Errorf("pty: create: %w", err)
	}

	cmd := exec.Command(opts.Argv[0], opts.Argv[1:]...)
	cmd.Dir = opts.Dir
	cmd.Env = append(os.Environ(), opts.Env...)
	if runtime.GOOS != "windows" {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color", "COLORTERM=truecolor")
	}
	r, w, err := masterIO(p)
	if err != nil {
		_ = p.Close()
		return nil, err
	}
	if err := p.Start(cmd); err != nil {
		_ = p.Close()
		_, _ = r.Close(), w.Close()
		return nil, fmt.Errorf("pty: start %s: %w", opts.Argv[0], err)
	}
	return &proc{Pty: p, r: r, w: w, cmd: cmd}, nil
}

type proc struct {
	xpty.Pty
	r         io.ReadCloser  // master side, safe against close/read races
	w         io.WriteCloser // see masterIO
	cmd       *exec.Cmd
	closeOnce sync.Once
	closeErr  error
}

func (p *proc) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *proc) Write(b []byte) (int, error) { return p.w.Write(b) }

func (p *proc) Resize(cols, rows int) error { return p.Pty.Resize(cols, rows) }

func (p *proc) Wait() error {
	// xpty.WaitProcess works around exec.Cmd.Wait not supporting ConPTY
	// processes on Windows.
	return xpty.WaitProcess(context.Background(), p.cmd)
}

func (p *proc) Pid() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *proc) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func (p *proc) Close() error {
	p.closeOnce.Do(func() {
		// Close the PTY first: that ends the far side of the pipes, so a read
		// blocked on p.r returns before its handle is released.
		p.closeErr = p.Pty.Close()
		_, _ = p.r.Close(), p.w.Close()
	})
	return p.closeErr
}
