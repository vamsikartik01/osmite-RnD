// Package client is the otmux terminal UI. It attaches to a workspace on the
// daemon, mirrors every visible pane in a local emulator, draws the panes,
// dividers, status bar and overlays, and turns keyboard and mouse input into
// pane input or workspace commands.
//
// The client holds no state the daemon can't rebuild: detaching (or
// crashing) loses nothing, and reattaching gets a fresh snapshot.
package client

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/config"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/keys"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/platform"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/update"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/vtx"
)

// frameInterval caps redraws at ~120fps; output arriving faster is coalesced.
const frameInterval = 8 * time.Millisecond

// Options configures Attach.
type Options struct {
	Workspace string
	Mode      string // protocol.AttachOrCreate, AttachExisting or CreateNew
	Dir       string // where a new workspace's shells start; default: cwd
	// Prefix from OTMUX_PREFIX. It overrides (and locks) the configured one.
	Prefix string
	Config *config.Config
}

// Client is one attached terminal UI.
type Client struct {
	conn  *protocol.Conn
	term  *uv.Terminal
	keys  *keys.Machine
	theme Theme
	cfg   *config.Config
	// prefixLocked is set when OTMUX_PREFIX overrides the configured prefix.
	prefixLocked bool

	mu         sync.Mutex
	state      protocol.State
	mirrors    map[uint32]*mirror
	cols, rows int
	sentCols   int // pane-area size last sent to the daemon
	sentRows   int
	history    map[uint32]*histView // panes scrolled back with the wheel
	overlay    *overlay
	settings   *settings

	// Updates.
	updateInfo   update.State
	updateBusy   bool
	updateBadge  string            // short note for the status bar, e.g. "1.1.0 installed · restart to finish"
	updateStatus string            // longer note for Settings › Updates
	drag         *protocol.Divider // divider being dragged with the mouse
	sel          *selection        // mouse text selection
	hits         []hit             // clickable status bar regions, set by render
	byeReason    string

	dirty chan struct{}
	done  chan struct{}
}

// mirror is the client's copy of one visible pane.
type mirror struct {
	emu          *vt.Emulator
	cursorHidden bool
}

// histView is a pane's scrollback as the daemon rendered it for us.
type histView struct {
	*mirror
	offset, total int
}

// hit is a clickable region of the status bar.
type hit struct {
	x0, x1, y int
	action    keys.Action
}

// Attach connects to the daemon, attaches to a workspace and runs the UI
// until the user detaches or the last workspace ends. It returns a message
// describing why it ended.
func Attach(opts Options) (string, error) {
	nc, err := net.Dial("unix", platform.SocketPath())
	if err != nil {
		return "", fmt.Errorf("connect to daemon: %w", err)
	}
	conn := protocol.NewConn(nc)
	defer conn.Close()

	term := uv.DefaultTerminal()
	cols, rows, err := term.GetSize()
	if err != nil {
		return "", fmt.Errorf("otmux must run in a terminal: %w", err)
	}
	cfg := opts.Config
	if cfg == nil {
		cfg = &config.Config{}
	}
	prefix := opts.Prefix
	if prefix == "" {
		prefix = cfg.Prefix
	}
	c := &Client{
		term:         term,
		keys:         keys.NewMachine(keys.Default(prefix)),
		theme:        ThemeByName(cfg.Theme),
		cfg:          cfg,
		prefixLocked: opts.Prefix != "",
		mirrors:      map[uint32]*mirror{},
		history:      map[uint32]*histView{},
		cols:         cols,
		rows:         rows,
		dirty:        make(chan struct{}, 1),
		done:         make(chan struct{}),
	}
	dir := opts.Dir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if err := conn.Send(protocol.TypeHello, protocol.Hello{
		Version: protocol.Version, Workspace: opts.Workspace, Mode: opts.Mode, Dir: dir, Cols: c.paneCols(), Rows: c.daemonRows(),
	}); err != nil {
		return "", err
	}
	f, err := conn.Read()
	if err != nil {
		return "", fmt.Errorf("handshake: %w", err)
	}
	var w protocol.Welcome
	if f.Type != protocol.TypeWelcome || f.Decode(&w) != nil {
		return "", errors.New("handshake: unexpected reply from daemon")
	}
	if w.Error != "" {
		return "", errors.New(w.Error)
	}

	c.conn = conn
	c.sentCols, c.sentRows = c.paneCols(), c.daemonRows()

	scr := term.Screen()
	scr.EnterAltScreen()
	scr.SetMouseMode(uv.MouseModeDrag)
	// SGR encoding: without it terminals send the legacy X10 format, whose raw
	// bytes break past column 95 and show up as junk input.
	scr.SetMouseEncoding(uv.MouseEncodingSGR)
	scr.EnableBracketedPaste()
	scr.SetWindowTitle("otmux")
	if err := term.Start(); err != nil {
		return "", err
	}

	c.startUpdates(w.Daemon)
	go c.readLoop()
	go c.renderLoop()
	reason := c.eventLoop()

	_ = term.Stop()
	return reason, nil
}

func (c *Client) markDirty() {
	select {
	case c.dirty <- struct{}{}:
	default:
	}
}

// readLoop applies frames from the daemon to the local model.
func (c *Client) readLoop() {
	defer close(c.done)
	for {
		f, err := c.conn.Read()
		if err != nil {
			c.mu.Lock()
			if c.byeReason == "" {
				c.byeReason = "lost connection to daemon"
			}
			c.mu.Unlock()
			return
		}
		c.mu.Lock()
		switch f.Type {
		case protocol.TypeState:
			var st protocol.State
			if f.Decode(&st) == nil {
				c.applyState(st)
			}
		case protocol.TypeSnapshot:
			var snap protocol.Snapshot
			if f.Decode(&snap) == nil {
				c.applySnapshot(snap)
			}
		case protocol.TypeOutput:
			if pane, data, err := protocol.DecodeOutput(f.Payload); err == nil {
				if m := c.mirrors[pane]; m != nil {
					_, _ = m.emu.Write(data)
				}
			}
		case protocol.TypeHistory:
			var h protocol.History
			if f.Decode(&h) == nil {
				c.applyHistory(h)
			}
		case protocol.TypeBye:
			var bye protocol.Bye
			_ = f.Decode(&bye)
			c.byeReason = bye.Reason
		}
		c.mu.Unlock()
		c.markDirty()
	}
}

// applyState stores the layout and drops mirrors of panes no longer shown.
// Called with c.mu held.
func (c *Client) applyState(st protocol.State) {
	visible := map[uint32]bool{}
	for _, p := range st.Panes {
		visible[p.ID] = true
	}
	for id, m := range c.mirrors {
		if !visible[id] {
			vtx.Stop(m.emu)
			delete(c.mirrors, id)
		}
	}
	for id, h := range c.history {
		if !visible[id] {
			vtx.Stop(h.emu)
			delete(c.history, id)
		}
	}
	if c.sel != nil && !visible[c.sel.pane] {
		c.sel = nil
	}
	if c.drag != nil && st.Zoomed {
		c.drag = nil
	}
	c.state = st
}

// newMirror returns an emulator showing data, ready for live output.
func newMirror(cols, rows int, data string) *mirror {
	m := &mirror{emu: vt.NewEmulator(max(cols, 1), max(rows, 1))}
	// The emulator answers terminal queries on its input pipe. The daemon's
	// emulator already answered them for the real program, so discard ours;
	// the pipe is unbuffered and must be drained or writes block.
	go func() { _, _ = io.Copy(io.Discard, m.emu) }()
	m.emu.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(visible bool) { m.cursorHidden = !visible },
	})
	_, _ = m.emu.WriteString(data)
	return m
}

// applyHistory shows (or, at offset 0, drops) a pane's scrollback view.
// Called with c.mu held.
func (c *Client) applyHistory(h protocol.History) {
	if old := c.history[h.Pane]; old != nil {
		vtx.Stop(old.emu)
		delete(c.history, h.Pane)
	}
	if h.Offset > 0 {
		c.history[h.Pane] = &histView{mirror: newMirror(h.Cols, h.Rows, h.Data), offset: h.Offset, total: h.Total}
	}
	c.sel = nil
}

// applySnapshot replaces a pane's mirror. Called with c.mu held.
func (c *Client) applySnapshot(s protocol.Snapshot) {
	if old := c.mirrors[s.Pane]; old != nil {
		vtx.Stop(old.emu)
	}
	if h := c.history[s.Pane]; h != nil { // a fresh view replaces history
		vtx.Stop(h.emu)
		delete(c.history, s.Pane)
	}
	m := newMirror(s.Cols, s.Rows, s.Data)
	_, _ = fmt.Fprintf(m.emu, "\x1b[%d;%dH", s.CursorY+1, s.CursorX+1)
	m.cursorHidden = s.CursorHidden
	c.mirrors[s.Pane] = m
}

func (c *Client) renderLoop() {
	clock := time.NewTicker(15 * time.Second) // keeps the status bar clock current
	defer clock.Stop()
	blink := time.NewTicker(blinkInterval) // animates working agents' dots
	defer blink.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-clock.C:
		case <-blink.C:
			c.mu.Lock()
			working := c.anyWorking()
			c.mu.Unlock()
			if !working {
				continue
			}
		case <-c.dirty:
		}
		c.render()
		time.Sleep(frameInterval)
	}
}

// eventLoop handles terminal input until the client should exit.
func (c *Client) eventLoop() string {
	// Not every platform reports window resizes as events (Windows consoles
	// don't, through ultraviolet), so also poll the size.
	sizePoll := time.NewTicker(200 * time.Millisecond)
	defer sizePoll.Stop()
	for {
		select {
		case <-c.done:
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.byeReason
		case <-sizePoll.C:
			w, h, err := c.term.GetSize()
			c.mu.Lock()
			changed := err == nil && w > 0 && h > 0 && (w != c.cols || h != c.rows)
			c.mu.Unlock()
			if changed {
				c.handleEvent(uv.WindowSizeEvent{Width: w, Height: h})
				c.markDirty()
			}
		case ev := <-c.term.Events():
			if reason, quit := c.handleEvent(ev); quit {
				return reason
			}
			c.markDirty()
		}
	}
}

func (c *Client) handleEvent(ev uv.Event) (string, bool) {
	switch ev := ev.(type) {
	case uv.WindowSizeEvent:
		c.mu.Lock()
		if ev.Width == c.cols && ev.Height == c.rows {
			c.mu.Unlock()
			return "", false
		}
		c.cols, c.rows = ev.Width, ev.Height
		cols, rows := c.paneCols(), c.daemonRows()
		c.sentCols, c.sentRows = cols, rows
		c.mu.Unlock()
		_ = c.conn.Send(protocol.TypeResize, protocol.Resize{Cols: cols, Rows: rows})
	case uv.KeyPressEvent:
		return c.handleKey(ev)
	case uv.PasteEvent:
		c.mu.Lock()
		ov := c.overlay
		if ov != nil {
			ov.insert(ev.Content)
		}
		c.mu.Unlock()
		if ov == nil {
			c.sendInput(c.activePane(), protocol.Input{Paste: ev.Content})
		}
	case uv.MouseClickEvent:
		return c.handleMouse(protocol.MouseClick, ev.Mouse())
	case uv.MouseReleaseEvent:
		return c.handleMouse(protocol.MouseRelease, ev.Mouse())
	case uv.MouseWheelEvent:
		return c.handleMouse(protocol.MouseWheel, ev.Mouse())
	case uv.MouseMotionEvent:
		return c.handleMouse(protocol.MouseMotion, ev.Mouse())
	}
	return "", false
}

func (c *Client) handleKey(k uv.KeyPressEvent) (string, bool) {
	c.mu.Lock()
	ov, set := c.overlay, c.settings
	c.mu.Unlock()
	if set != nil {
		if act, ok := c.settingsKey(k); ok {
			return c.run(act, k)
		}
		c.resizeIfNeeded()
		return "", false
	}
	if ov != nil {
		if act, ok := c.overlayKey(k); ok {
			return c.run(act, k)
		}
		return "", false
	}

	c.mu.Lock() // the renderer reads the prefix state for the hint bar
	res, act := c.keys.Handle(k)
	c.sel = nil
	c.mu.Unlock()
	switch res {
	case keys.Forward:
		c.leaveHistory(c.activePane())
		c.sendKey(k)
	case keys.Run:
		return c.run(act, k)
	}
	return "", false
}

// run performs an action from a key binding, the palette or a click.
func (c *Client) run(act keys.Action, k uv.KeyPressEvent) (string, bool) {
	c.mu.Lock()
	tabName, wsName := "", c.state.Workspace
	if i := c.state.Active; i >= 0 && i < len(c.state.Tabs) {
		tabName = c.state.Tabs[i].Name
	}
	c.mu.Unlock()

	switch act.Name {
	case "":
	case keys.ActionDetach:
		return "detached from workspace " + wsName, true
	case keys.ActionSendPrefix:
		c.sendKey(k)
	case keys.ActionPalette:
		c.openOverlay(c.paletteOverlay())
	case keys.ActionChooseWorkspace:
		c.openOverlay(c.workspaceOverlay())
	case keys.ActionSettings:
		c.openSettings()
		if act.Arg == "updates" {
			c.mu.Lock()
			if c.settings != nil {
				c.settings.section, c.settings.inMenu = secUpdates, false
				c.settings.sel[secUpdates] = 1 // "Update now"
			}
			c.mu.Unlock()
		}
	case keys.ActionToggleSidebar:
		c.mu.Lock()
		if c.cfg.LayoutName() == config.LayoutSidebar {
			c.cfg.SetLayout(config.LayoutTwoBars)
		} else {
			c.cfg.SetLayout(config.LayoutSidebar)
		}
		c.saveConfig()
		c.mu.Unlock()
		c.resizeIfNeeded()
	case keys.ActionPromptNewTab:
		o := promptOverlay("New tab", "", protocol.ActionNewTab)
		o.placeholder = "Name it, or press enter for a surprise"
		c.openOverlay(o)
	case keys.ActionPromptRenameTab:
		c.openOverlay(promptOverlay("Rename tab", tabName, protocol.ActionRenameTab))
	case keys.ActionPromptRenameWorkspace:
		c.openOverlay(promptOverlay("Rename workspace", wsName, protocol.ActionRenameWorkspace))
	case keys.ActionPromptNewWorkspace:
		c.openOverlay(promptOverlay("New workspace", "", protocol.ActionNewWorkspace))
	default:
		_ = c.conn.Send(protocol.TypeCommand, protocol.Command{Action: act.Name, Arg: act.Arg, Dir: act.Dir})
	}
	return "", false
}

// resizeIfNeeded tells the daemon the pane area's size after the sidebar
// was shown or hidden.
func (c *Client) resizeIfNeeded() {
	c.mu.Lock()
	cols, rows := c.paneCols(), c.daemonRows()
	changed := cols != c.sentCols || rows != c.sentRows
	c.sentCols, c.sentRows = cols, rows
	c.mu.Unlock()
	if changed {
		_ = c.conn.Send(protocol.TypeResize, protocol.Resize{Cols: cols, Rows: rows})
	}
}

func (c *Client) openOverlay(o *overlay) {
	c.mu.Lock()
	c.overlay = o
	c.mu.Unlock()
}

func (c *Client) handleMouse(kind protocol.MouseKind, m uv.Mouse) (string, bool) {
	c.mu.Lock()
	st := c.state
	ov, set := c.overlay, c.settings
	drag := c.drag
	sel := c.sel
	statusRow := m.Y == c.rows-1
	hits := c.hits
	ox, oy := c.ox(), c.oy()
	c.mu.Unlock()
	left := m.Button == uv.MouseLeft
	right := m.Button == uv.MouseRight

	if ov != nil {
		return c.overlayMouse(ov, kind, m)
	}
	if set != nil {
		if kind == protocol.MouseClick {
			if act, ok := c.settingsClick(m.X, m.Y); ok {
				return c.run(act, uv.KeyPressEvent{})
			}
			c.resizeIfNeeded()
		}
		return "", false
	}
	// Status bar and sidebar buttons, in screen coordinates.
	if kind == protocol.MouseClick && left && drag == nil && sel == nil {
		for _, h := range hits {
			if m.Y == h.y && m.X >= h.x0 && m.X < h.x1 {
				return c.run(h.action, uv.KeyPressEvent{})
			}
		}
	}
	if drag == nil && sel == nil && (statusRow || m.X < ox || m.Y < oy) {
		return "", false // other clicks on chrome do nothing
	}
	// From here on, coordinates are relative to the pane area.
	m.X -= ox
	m.Y -= oy

	// Dragging a divider.
	if drag != nil {
		switch kind {
		case protocol.MouseMotion:
			pos := m.Y
			if drag.Vertical {
				pos = m.X
			}
			c.command(protocol.ActionDragSplit, fmt.Sprintf("%d %d", drag.Node, pos))
		case protocol.MouseRelease:
			c.mu.Lock()
			c.drag = nil
			c.mu.Unlock()
		}
		return "", false
	}

	// Selecting text: extend while dragging, copy on release.
	if sel != nil && (kind == protocol.MouseMotion || kind == protocol.MouseRelease) {
		if p, ok := paneByID(st, sel.pane); ok {
			c.mu.Lock()
			sel.bx = min(max(m.X-p.X, 0), p.W-1)
			sel.by = min(max(m.Y-p.Y, 0), p.H-1)
			if sel.bx != sel.ax || sel.by != sel.ay {
				sel.moved = true
			}
			text := ""
			if kind == protocol.MouseRelease {
				text = sel.text(c.shown(sel.pane), p.W)
				if !sel.moved {
					c.sel = nil
				}
			}
			c.mu.Unlock()
			c.copyText(text)
		}
		return "", false
	}

	if kind == protocol.MouseClick && left {
		for _, d := range st.Dividers {
			if onDivider(d, m.X, m.Y) {
				c.mu.Lock()
				dd := d
				c.drag = &dd
				c.mu.Unlock()
				return "", false
			}
		}
	}

	pane, ok := paneAt(st, m.X, m.Y)
	if !ok {
		return "", false
	}
	rel := &protocol.Mouse{Kind: kind, X: m.X - pane.X, Y: m.Y - pane.Y, Button: int(m.Button), Mod: int(m.Mod)}

	// The wheel always goes to the daemon, which scrolls history or passes
	// it to the program, whichever fits.
	if kind == protocol.MouseWheel {
		c.sendInput(pane.ID, protocol.Input{Mouse: rel})
		return "", false
	}
	if kind == protocol.MouseClick && pane.ID != st.ActivePane {
		c.command(protocol.ActionFocusPane, fmt.Sprint(pane.ID))
	}
	// Programs that track the mouse (vim, htop) get it, as long as nothing
	// is selected and their pane is focused.
	if pane.Mouse && pane.ID == st.ActivePane && sel == nil {
		c.sendInput(pane.ID, protocol.Input{Mouse: rel})
		return "", false
	}
	switch {
	case kind == protocol.MouseClick && left:
		c.mu.Lock()
		c.sel = &selection{pane: pane.ID, ax: rel.X, ay: rel.Y, bx: rel.X, by: rel.Y}
		c.mu.Unlock()
	case kind == protocol.MouseClick && right:
		// Like Windows Terminal: right-click copies a selection, or pastes.
		c.mu.Lock()
		text := ""
		if sel != nil && sel.moved {
			if p, ok := paneByID(st, sel.pane); ok {
				text = sel.text(c.shown(sel.pane), p.W)
			}
		}
		c.sel = nil
		c.mu.Unlock()
		if text != "" {
			c.copyText(text)
		} else if clip := pasteText(); clip != "" {
			c.leaveHistory(st.ActivePane)
			c.sendInput(st.ActivePane, protocol.Input{Paste: clip})
		}
	}
	return "", false
}

// overlayMouse handles the mouse while a palette or prompt is open: clicks
// pick items, clicks outside close it, the wheel moves the selection.
func (c *Client) overlayMouse(ov *overlay, kind protocol.MouseKind, m uv.Mouse) (string, bool) {
	switch kind {
	case protocol.MouseWheel:
		c.mu.Lock()
		if n := len(ov.view); n > 0 {
			if m.Button == uv.MouseWheelUp {
				ov.sel = max(ov.sel-1, 0)
			} else {
				ov.sel = min(ov.sel+1, n-1)
			}
		}
		c.mu.Unlock()
	case protocol.MouseClick:
		c.mu.Lock()
		act, ok, closed := ov.click(m.X, m.Y)
		if closed || ok {
			c.overlay = nil
		}
		c.mu.Unlock()
		if ok {
			return c.run(act, uv.KeyPressEvent{})
		}
	}
	return "", false
}

// shown returns what pane id is displaying: its history view if scrolled
// back, otherwise the live mirror. Called with c.mu held.
func (c *Client) shown(id uint32) *mirror {
	if h := c.history[id]; h != nil {
		return h.mirror
	}
	return c.mirrors[id]
}

// leaveHistory returns pane to its live screen before input is sent to it.
func (c *Client) leaveHistory(pane uint32) {
	c.mu.Lock()
	h := c.history[pane]
	if h != nil {
		vtx.Stop(h.emu)
		delete(c.history, pane)
	}
	c.mu.Unlock()
	if h != nil {
		c.command(protocol.ActionScrollReset, fmt.Sprint(pane))
	}
}

func paneByID(st protocol.State, id uint32) (protocol.PaneInfo, bool) {
	for _, p := range st.Panes {
		if p.ID == id {
			return p, true
		}
	}
	return protocol.PaneInfo{}, false
}

func onDivider(d protocol.Divider, x, y int) bool {
	if d.Vertical {
		return x == d.X && y >= d.Y && y < d.Y+d.Len
	}
	return y == d.Y && x >= d.X && x < d.X+d.Len
}

func paneAt(st protocol.State, x, y int) (protocol.PaneInfo, bool) {
	for _, p := range st.Panes {
		if x >= p.X && x < p.X+p.W && y >= p.Y && y < p.Y+p.H {
			return p, true
		}
	}
	return protocol.PaneInfo{}, false
}

func (c *Client) activePane() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.ActivePane
}

func (c *Client) sendKey(k uv.KeyPressEvent) {
	c.sendInput(c.activePane(), protocol.Input{Key: &protocol.Key{
		Code: k.Code, Text: k.Text, Mod: int(k.Mod), ShiftedCode: k.ShiftedCode, BaseCode: k.BaseCode,
	}})
}

func (c *Client) sendInput(pane uint32, in protocol.Input) {
	if pane == 0 {
		return
	}
	in.Pane = pane
	_ = c.conn.Send(protocol.TypeInput, in)
}

func (c *Client) command(action, arg string) {
	_ = c.conn.Send(protocol.TypeCommand, protocol.Command{Action: action, Arg: arg})
}
