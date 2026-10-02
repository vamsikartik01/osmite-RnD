//go:build windows

package pty

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/xpty"
	"golang.org/x/sys/windows"
)

// masterIO returns safe reader/writer handles for the ConPTY's master side.
//
// x/conpty reads and writes its raw pipe HANDLEs directly. A read that races
// with Close can then land on a handle value Windows has already recycled for
// some unrelated pipe or socket, and steal its data. We instead duplicate the
// two master handles into *os.File, whose reference counting never closes a
// handle while a read or write on it is still in flight. All four original
// pipe handles are also made non-inheritable: x/conpty creates them
// inheritable, which would leak every pane's pipes into any process the
// daemon starts.
func masterIO(p xpty.Pty) (io.ReadCloser, io.WriteCloser, error) {
	cp, ok := p.(*xpty.ConPty)
	if !ok {
		return nil, nil, fmt.Errorf("pty: unexpected type %T", p)
	}
	for _, h := range []uintptr{cp.InPipeReadFd(), cp.InPipeWriteFd(), cp.OutPipeReadFd(), cp.OutPipeWriteFd()} {
		_ = windows.SetHandleInformation(windows.Handle(h), windows.HANDLE_FLAG_INHERIT, 0)
	}
	out, err := dup(cp.OutPipeReadFd())
	if err != nil {
		return nil, nil, err
	}
	in, err := dup(cp.InPipeWriteFd())
	if err != nil {
		windows.CloseHandle(out)
		return nil, nil, err
	}
	return os.NewFile(uintptr(out), "conpty-out"), os.NewFile(uintptr(in), "conpty-in"), nil
}

func dup(h uintptr) (windows.Handle, error) {
	self := windows.CurrentProcess()
	var d windows.Handle
	if err := windows.DuplicateHandle(self, windows.Handle(h), self, &d, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return 0, fmt.Errorf("pty: duplicate handle: %w", err)
	}
	return d, nil
}
