// Package vtx holds small helpers around charmbracelet/x/vt.
package vtx

import (
	"io"

	"github.com/charmbracelet/x/vt"
)

// Stop ends an emulator's input side, so a goroutine blocked in its Read
// returns io.EOF. Emulator.Close does the same but also sets an internal
// flag that Read checks without a lock, which is a data race when another
// goroutine is reading; closing the input pipe directly avoids it.
func Stop(e *vt.Emulator) {
	if pw, ok := e.InputPipe().(*io.PipeWriter); ok {
		_ = pw.CloseWithError(io.EOF)
		return
	}
	_ = e.Close()
}
