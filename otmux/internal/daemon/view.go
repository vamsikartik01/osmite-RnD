package daemon

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

// Several clients can view one workspace (the terminal at home, a browser on
// a phone), but its programs draw for one size. The workspace takes the size
// of its driver: the client that joined first, then whichever one the user
// last typed in. Other clients only watch, so opening a workspace somewhere
// else doesn't squeeze it everywhere.

// maxSnapshotScrollback caps the history a client can ask for per Snapshot.
const maxSnapshotScrollback = 10000

type paneSize struct{ cols, rows int }

// drive makes c the workspace's driver and gives the workspace its size.
// Called with s.mu held.
func (s *Server) drive(c *client) {
	ws := c.ws
	if ws.driver == c {
		return
	}
	ws.driver = c
	if s.resize(ws, c.cols, c.rows) {
		s.broadcastView(ws)
	}
}

// leave takes c out of its workspace, handing the size to another client if
// c was driving. Called with s.mu held.
func (s *Server) leave(c *client) {
	ws := c.ws
	delete(ws.clients, c)
	if ws.driver != c {
		return
	}
	ws.driver = nil
	for o := range ws.clients {
		s.drive(o)
		break
	}
}

// sendState sends c the workspace state. Clients drop their copy of panes
// the state doesn't show, so c.shown forgets them too.
func (s *Server) sendState(c *client, st protocol.State) {
	for id := range c.shown {
		if !slices.ContainsFunc(st.Panes, func(p protocol.PaneInfo) bool { return p.ID == id }) {
			delete(c.shown, id)
		}
	}
	c.queueJSON(protocol.TypeState, st)
}

// needsSnapshot reports whether c has no current copy of p's screen, and
// records that it is about to get one.
func (c *client) needsSnapshot(p *Pane) bool {
	size := paneSize{p.emu.Width(), p.emu.Height()}
	if c.keepsCopies && c.shown[p.id] == size {
		return false
	}
	c.shown[p.id] = size
	return true
}

// snapshotWithHistory is p.snapshot with up to lines of scrollback in front
// of the screen, for clients that scroll back themselves. Full-screen
// programs (the alternate screen) get a plain snapshot: their history isn't
// what's on screen.
func (p *Pane) snapshotWithHistory(lines int) protocol.Snapshot {
	snap := p.snapshot()
	lines = min(lines, maxSnapshotScrollback, p.emu.ScrollbackLen())
	if lines <= 0 || p.emu.IsAltScreen() {
		return snap
	}
	sb := p.emu.Scrollback()
	total, w := sb.Len(), p.emu.Width()

	var b strings.Builder
	b.WriteString("\x1b[0m\x1b[H\x1b[2J")
	for i := total - lines; i < total; i++ {
		// Truncated to the pane: a line that wrapped would push the screen down.
		b.WriteString(ansi.Truncate(sb.Line(i).Render(), w, ""))
		b.WriteString("\x1b[0m\r\n")
	}
	// Exactly one line per row, so the history ends just above the screen.
	screen := strings.Split(p.emu.Render(), "\n")
	for y := range p.emu.Height() {
		if y > 0 {
			b.WriteString("\x1b[0m\r\n")
		}
		if y < len(screen) {
			b.WriteString(screen[y])
		}
	}
	b.WriteString("\x1b[0m")
	snap.Data = b.String()
	snap.Scrollback = lines
	return snap
}
