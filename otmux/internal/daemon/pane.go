package daemon

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/pty"
)

// scrollbackLines is how much history each pane keeps.
const scrollbackLines = 10000

// Pane is one shell running on a PTY, plus the daemon's emulator copy of its
// screen. The emulator is the source of truth for what the pane looks like:
// clients that attach (or reattach) get a snapshot of it.
//
// All fields are guarded by Server.mu.
type Pane struct {
	id  uint32
	ws  *Workspace
	tab *Tab
	pty pty.PTY
	emu *vt.Emulator

	cols, rows   int
	cursorHidden bool
	title        string
	program      string // the shell's name, e.g. "pwsh"
	cwd          string // reported by the shell via OSC 7, if it does
	mouseModes   map[ansi.Mode]bool
	stateChanged bool      // title or mouse mode changed; clients need a new State
	agent        string    // coding agent running in the pane, if any
	notified     bool      // rang the bell or sent a desktop notification
	lastOutput   time.Time // when the program last produced output by itself
	lastPoke     time.Time // when the user last typed into or resized the pane
	typed        bool      // the user has typed or pasted into the pane
	exited       bool
}

func newPane(id uint32, argv []string, dir string, cols, rows int) (*Pane, error) {
	if dir != "" {
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			dir = ""
		}
	}
	p, err := pty.Start(pty.Options{
		Argv: argv,
		Dir:  dir,
		Env:  []string{fmt.Sprintf("OTMUX_PANE=%d", id)},
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		return nil, err
	}
	pane := &Pane{id: id, pty: p, emu: vt.NewEmulator(cols, rows), cols: cols, rows: rows, mouseModes: map[ansi.Mode]bool{}}
	if len(argv) > 0 {
		pane.program = strings.TrimSuffix(filepath.Base(argv[0]), filepath.Ext(argv[0]))
	}
	pane.emu.SetScrollbackSize(scrollbackLines)
	pane.emu.SetCallbacks(vt.Callbacks{
		Title: func(t string) {
			pane.title = t
			pane.stateChanged = true
		},
		EnableMode:       func(m ansi.Mode) { pane.setMouseMode(m, true) },
		DisableMode:      func(m ansi.Mode) { pane.setMouseMode(m, false) },
		CursorVisibility: func(visible bool) { pane.cursorHidden = !visible },
		Bell:             func() { pane.notified = true },
		WorkingDirectory: func(u string) {
			pane.cwd = parseFileURL(u)
			pane.stateChanged = true
		},
	})
	// Desktop notifications: OSC 9 (iTerm2, Windows Terminal) and OSC 777
	// (urxvt, VTE). Agents send these when they finish or need permission.
	// OSC 9;4 is Windows Terminal's progress bar, not a notification.
	pane.emu.RegisterOscHandler(9, func(data []byte) bool {
		if !strings.HasPrefix(string(data), "9;4;") {
			pane.notified = true
		}
		return true
	})
	pane.emu.RegisterOscHandler(777, func(data []byte) bool {
		if strings.HasPrefix(string(data), "777;notify") {
			pane.notified = true
		}
		return true
	})
	return pane, nil
}

func (p *Pane) setMouseMode(m ansi.Mode, on bool) {
	switch m {
	case ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent:
		if p.mouseModes[m] != on {
			p.mouseModes[m] = on
			p.stateChanged = true
		}
	}
}

// wantsMouse reports whether the program asked for mouse events.
func (p *Pane) wantsMouse() bool {
	for _, on := range p.mouseModes {
		if on {
			return true
		}
	}
	return false
}

// parseFileURL turns an OSC 7 "file://host/path" into a local path.
func parseFileURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	path := u.Path
	if runtime.GOOS == "windows" {
		path = strings.TrimPrefix(path, "/") // "/C:/src" -> "C:/src"
	}
	return path
}

// drainInput copies bytes the emulator produces (encoded keys, paste, mouse,
// and replies to terminal queries such as cursor-position reports) into the
// PTY. It must run for the pane's whole life: the emulator's input side is an
// unbuffered pipe, so writes to it block until they are read here.
func (p *Pane) drainInput() {
	buf := make([]byte, 4096)
	failed := false
	for {
		n, err := p.emu.Read(buf)
		if err != nil {
			return
		}
		if failed {
			continue // keep draining so emulator writes never block
		}
		if _, err := p.pty.Write(buf[:n]); err != nil {
			failed = true
		}
	}
}

// echoWindow: output this soon after the user typed or resized is the
// program reacting (echo, redraw), not working on its own.
const echoWindow = 400 * time.Millisecond

// noteOutput records output, unless it's an echo of the user's own input.
func (p *Pane) noteOutput(now time.Time) {
	if now.Sub(p.lastPoke) > echoWindow {
		p.lastOutput = now
	}
}

func (p *Pane) resize(cols, rows int) {
	if cols == p.cols && rows == p.rows {
		return
	}
	p.lastPoke = time.Now()
	p.cols, p.rows = cols, rows
	_ = p.pty.Resize(cols, rows)
	p.emu.Resize(cols, rows)
}

// sendInput encodes an input event for this pane's terminal modes.
func (p *Pane) sendInput(in protocol.Input) {
	p.lastPoke = time.Now()
	switch {
	case in.Key != nil:
		p.sendKey(*in.Key)
	case in.Paste != "":
		p.emu.Paste(in.Paste)
	case in.Mouse != nil:
		m := uv.Mouse{X: in.Mouse.X, Y: in.Mouse.Y, Button: uv.MouseButton(in.Mouse.Button), Mod: uv.KeyMod(in.Mouse.Mod)}
		switch in.Mouse.Kind {
		case protocol.MouseClick:
			p.emu.SendMouse(uv.MouseClickEvent(m))
		case protocol.MouseRelease:
			p.emu.SendMouse(uv.MouseReleaseEvent(m))
		case protocol.MouseWheel:
			p.emu.SendMouse(uv.MouseWheelEvent(m))
		case protocol.MouseMotion:
			p.emu.SendMouse(uv.MouseMotionEvent(m))
		}
	}
}

func (p *Pane) sendKey(k protocol.Key) {
	mod := uv.KeyMod(k.Mod)
	ctrl, alt := mod&uv.ModCtrl != 0, mod&uv.ModAlt != 0
	switch {
	case k.Text != "" && (!ctrl && !alt || ctrl && alt):
		// Plain or shifted text. Ctrl+Alt+text is AltGr on international
		// layouts (e.g. AltGr+Q = '@' on German keyboards).
		p.emu.SendText(k.Text)
	case k.Text != "" && alt:
		p.emu.SendText("\x1b" + k.Text)
	default:
		// Control keys, arrows, function keys and so on. The emulator knows
		// whether the app enabled application cursor/keypad modes.
		p.emu.SendKey(uv.KeyPressEvent{Code: k.Code, Mod: mod})
	}
}

// history renders the view offset lines above the live screen, mixing
// scrollback and screen lines, as VT sequences for a pane-sized emulator.
func (p *Pane) history(offset int) protocol.History {
	sb := p.emu.Scrollback()
	total := sb.Len()
	offset = min(max(offset, 0), total)
	w, h := p.emu.Width(), p.emu.Height()
	screen := strings.Split(p.emu.Render(), "\n")

	var b strings.Builder
	b.WriteString("\x1b[0m\x1b[H\x1b[2J")
	for r := 0; r < h; r++ {
		var line string
		if i := total - offset + r; i < total {
			line = sb.Line(i).Render()
		} else if j := i - total; j < len(screen) {
			line = screen[j]
		}
		fmt.Fprintf(&b, "\x1b[%d;1H", r+1)
		b.WriteString(ansi.Truncate(line, w, ""))
	}
	return protocol.History{Pane: p.id, Offset: offset, Total: total, Cols: w, Rows: h, Data: b.String()}
}

// snapshot renders the current screen as VT sequences that redraw it from
// blank on a terminal of the same size.
func (p *Pane) snapshot() protocol.Snapshot {
	var b strings.Builder
	b.WriteString("\x1b[0m\x1b[H\x1b[2J")
	for y, line := range strings.Split(p.emu.Render(), "\n") {
		fmt.Fprintf(&b, "\x1b[%d;1H", y+1)
		b.WriteString(line)
	}
	pos := p.emu.CursorPosition()
	return protocol.Snapshot{
		Pane:         p.id,
		Cols:         p.emu.Width(),
		Rows:         p.emu.Height(),
		Data:         b.String(),
		CursorX:      pos.X,
		CursorY:      pos.Y,
		CursorHidden: p.cursorHidden,
	}
}
