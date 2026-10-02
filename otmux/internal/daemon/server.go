// Package daemon is the otmux session server. It owns workspaces, tabs and
// panes, keeps every shell running while no client is attached, and streams
// pane output to attached clients over the protocol package's wire format.
package daemon

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/layout"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/platform"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/version"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/vtx"
)

// ErrAlreadyRunning is returned by Run when another daemon owns the socket.
var ErrAlreadyRunning = errors.New("daemon already running")

// clientQueue is how many frames may be waiting for a client before it is
// considered too slow and disconnected. A disconnected client can reattach
// and will get a fresh snapshot, so dropping it is always safe.
const clientQueue = 4096

// Server is the daemon. One big lock (mu) guards the whole model, including
// every pane's emulator. That keeps ordering simple: a snapshot and the
// output that follows it are always queued to clients in the right order.
type Server struct {
	mu         sync.Mutex
	ln         net.Listener
	workspaces map[string]*Workspace
	nextID     uint32
	pinSeq     uint64
	everUsed   bool

	shutdownOnce sync.Once
	done         chan struct{}
	writers      sync.WaitGroup // client writer goroutines, flushed on exit
}

// Run listens on the platform socket and serves until the last workspace
// closes or kill-server is requested.
func Run() error {
	if err := platform.EnsureRuntimeDir(); err != nil {
		return err
	}
	path := platform.SocketPath()
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		return ErrAlreadyRunning
	}
	_ = os.Remove(path) // stale socket from a crashed daemon

	ln, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("listen %s: %w", path, err)
	}
	s := &Server{ln: ln, workspaces: map[string]*Workspace{}, done: make(chan struct{})}
	go s.watchAgents()
	log.Printf("otmux daemon listening on %s (pid %d)", path, os.Getpid())

	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(protocol.NewConn(nc))
		}
	}()
	<-s.done
	// Give clients a moment to receive their Bye before the process exits.
	flushed := make(chan struct{})
	go func() { s.writers.Wait(); close(flushed) }()
	select {
	case <-flushed:
	case <-time.After(time.Second):
	}
	_ = os.Remove(path)
	log.Printf("otmux daemon exiting")
	return nil
}

func (s *Server) shutdown() {
	s.shutdownOnce.Do(func() {
		s.ln.Close()
		close(s.done)
	})
}

func (s *Server) newID() uint32 {
	s.nextID++
	return s.nextID
}

// --- clients ---------------------------------------------------------------

type client struct {
	conn       *protocol.Conn
	out        chan []byte
	ws         *Workspace
	dir        string
	cols, rows int
	scroll     map[uint32]int // pane -> lines scrolled back with the wheel
	closed     bool
}

func (s *Server) newClient(conn *protocol.Conn, h protocol.Hello) *client {
	c := &client{conn: conn, out: make(chan []byte, clientQueue), dir: h.Dir, cols: h.Cols, rows: h.Rows, scroll: map[uint32]int{}}
	s.writers.Add(1)
	go func() {
		defer s.writers.Done()
		for frame := range c.out {
			if err := conn.WriteRaw(frame); err != nil {
				break
			}
		}
		conn.Close()
		for range c.out { // let pending sends complete
		}
	}()
	return c
}

// queue sends an encoded frame. Called with s.mu held; never blocks.
func (c *client) queue(frame []byte) {
	if c.closed {
		return
	}
	select {
	case c.out <- frame:
	default:
		log.Printf("client too slow, disconnecting")
		c.close()
	}
}

func (c *client) queueJSON(t protocol.Type, v any) {
	frame, err := protocol.EncodeJSON(t, v)
	if err != nil {
		log.Printf("encode: %v", err)
		return
	}
	c.queue(frame)
}

// close flushes queued frames and then closes the connection. Called with
// s.mu held.
func (c *client) close() {
	if c.closed {
		return
	}
	c.closed = true
	close(c.out)
}

func (s *Server) serve(conn *protocol.Conn) {
	first, err := conn.Read()
	if err != nil {
		conn.Close()
		return
	}
	switch first.Type {
	case protocol.TypeList:
		s.mu.Lock()
		reply := protocol.ListReply{Workspaces: s.workspaceInfos()}
		s.mu.Unlock()
		_ = conn.Send(protocol.TypeListReply, reply)
		conn.Close()
		return
	case protocol.TypeCommand:
		// One-shot commands from the CLI, e.g. `otmux kill <name>`. The reply
		// is a Welcome whose Error says whether it worked.
		var cmd protocol.Command
		var err error
		if err = first.Decode(&cmd); err == nil {
			s.mu.Lock()
			err = s.oneShot(cmd)
			s.mu.Unlock()
		}
		w := protocol.Welcome{Version: protocol.Version}
		if err != nil {
			w.Error = err.Error()
		}
		_ = conn.Send(protocol.TypeWelcome, w)
		conn.Close()
		return
	case protocol.TypeHello:
	default:
		conn.Close()
		return
	}

	var hello protocol.Hello
	if err := first.Decode(&hello); err != nil {
		conn.Close()
		return
	}
	if hello.Version != protocol.Version {
		_ = conn.Send(protocol.TypeWelcome, protocol.Welcome{
			Version: protocol.Version,
			Error:   fmt.Sprintf("protocol mismatch: daemon v%d, client v%d; run `otmux kill-server` and retry", protocol.Version, hello.Version),
		})
		conn.Close()
		return
	}

	c := s.newClient(conn, hello)
	s.mu.Lock()
	err = s.attach(c, hello)
	if err != nil {
		c.queueJSON(protocol.TypeWelcome, protocol.Welcome{Version: protocol.Version, Error: err.Error()})
		c.close()
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	for {
		f, err := conn.Read()
		if err != nil {
			break
		}
		s.mu.Lock()
		s.handle(c, f)
		s.mu.Unlock()
	}

	s.mu.Lock()
	if c.ws != nil {
		delete(c.ws.clients, c)
		s.broadcastStates()
	}
	c.close()
	s.mu.Unlock()
}

// attach joins c to a workspace according to h.Mode.
func (s *Server) attach(c *client, h protocol.Hello) error {
	name := strings.TrimSpace(h.Workspace)
	if name == "" && h.Mode != protocol.CreateNew {
		name = "default"
	}
	ws := s.workspaces[name]
	switch {
	case h.Mode == protocol.AttachExisting && ws == nil:
		return fmt.Errorf("no workspace named %q (see `otmux ls`)", name)
	case h.Mode == protocol.CreateNew && ws != nil:
		return fmt.Errorf("workspace %q already exists; use `otmux attach %s`", name, name)
	}
	if ws == nil {
		var err error
		if ws, err = s.createWorkspace(name, h.Dir, h.Cols, h.Rows); err != nil {
			return err
		}
	}
	c.queueJSON(protocol.TypeWelcome, protocol.Welcome{Version: protocol.Version, Daemon: version.Version})
	s.join(c, ws)
	return nil
}

func (s *Server) oneShot(cmd protocol.Command) error {
	switch cmd.Action {
	case protocol.ActionKillServer:
		s.killServer()
	case protocol.ActionKillWorkspace:
		ws := s.workspaces[cmd.Arg]
		if ws == nil {
			return fmt.Errorf("no workspace named %q", cmd.Arg)
		}
		s.killWorkspace(ws)
	default:
		return fmt.Errorf("unknown command %q", cmd.Action)
	}
	return nil
}

// --- workspaces ------------------------------------------------------------

func (s *Server) sortedWorkspaces() []*Workspace {
	out := make([]*Workspace, 0, len(s.workspaces))
	for _, ws := range s.workspaces {
		out = append(out, ws)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func (s *Server) workspaceInfos() []protocol.WorkspaceInfo {
	var out []protocol.WorkspaceInfo
	for _, ws := range s.sortedWorkspaces() {
		out = append(out, ws.info())
	}
	return out
}

// uniqueName derives a workspace name: the requested one, or the directory's
// base name, with a numeric suffix if taken.
func (s *Server) uniqueName(name, dir string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.ToLower(filepath.Base(dir))
		if name == "" || name == "." || name == string(filepath.Separator) || strings.HasSuffix(name, ":") {
			name = "workspace"
		}
	}
	name = truncate(name, 32)
	if s.workspaces[name] == nil {
		return name
	}
	for i := 2; ; i++ {
		if n := fmt.Sprintf("%s-%d", name, i); s.workspaces[n] == nil {
			return n
		}
	}
}

func (s *Server) createWorkspace(name, dir string, cols, rows int) (*Workspace, error) {
	name = s.uniqueName(name, dir)
	ws := &Workspace{name: name, dir: dir, clients: map[*client]struct{}{}, cols: cols, rows: rows}
	if err := s.newTab(ws, ""); err != nil {
		return nil, err
	}
	s.workspaces[name] = ws
	s.everUsed = true
	log.Printf("workspace %q created in %s", name, dir)
	return ws, nil
}

// join moves c into ws (out of its current workspace, if any) and sends it
// the full view.
func (s *Server) join(c *client, ws *Workspace) {
	if c.ws != nil {
		delete(c.ws.clients, c)
	}
	c.ws = ws
	ws.clients[c] = struct{}{}
	s.resize(ws, c.cols, c.rows)
	s.broadcastStates() // client counts and workspace lists changed everywhere
	s.sendView(c)
}

// killPane stops a pane's process. The PTY itself is closed by the pane's
// wait goroutine, outside s.mu: on Windows, closing a pseudo console can
// block until its remaining output is read, and the reader needs s.mu.
func killPane(p *Pane) {
	p.exited = true
	_ = p.pty.Kill()
	vtx.Stop(p.emu)
}

func (s *Server) killWorkspace(ws *Workspace) {
	for _, t := range ws.tabs {
		for _, p := range t.panes {
			killPane(p)
		}
	}
	ws.tabs = nil
	s.closeWorkspace(ws, "workspace "+ws.name+" killed")
}

// closeWorkspace removes ws. Its clients move to another workspace if there
// is one (like tmux with detach-on-destroy off); otherwise they're told why
// they're being disconnected.
func (s *Server) closeWorkspace(ws *Workspace, reason string) {
	delete(s.workspaces, ws.name)
	log.Printf("%s", reason)
	others := s.sortedWorkspaces()
	for c := range ws.clients {
		if len(others) > 0 {
			c.ws = nil
			s.join(c, others[0])
			continue
		}
		c.queueJSON(protocol.TypeBye, protocol.Bye{Reason: reason})
		c.close()
	}
	ws.clients = nil
	if s.everUsed && len(s.workspaces) == 0 {
		s.shutdown()
		return
	}
	s.broadcastStates()
}

func (s *Server) killServer() {
	for _, ws := range s.workspaces {
		for _, t := range ws.tabs {
			for _, p := range t.panes {
				killPane(p)
			}
		}
		for c := range ws.clients {
			c.queueJSON(protocol.TypeBye, protocol.Bye{Reason: "server killed"})
			c.close()
		}
	}
	s.workspaces = map[string]*Workspace{}
	s.shutdown()
}

// --- requests from attached clients -----------------------------------------

func (s *Server) handle(c *client, f protocol.Frame) {
	ws := c.ws
	if ws == nil || len(ws.tabs) == 0 {
		return // workspace closed; the client is being moved or disconnected
	}
	switch f.Type {
	case protocol.TypeInput:
		var in protocol.Input
		if f.Decode(&in) != nil {
			return
		}
		p := ws.activeTab().panes[in.Pane]
		if p == nil {
			return
		}
		if in.Mouse != nil && in.Mouse.Kind == protocol.MouseWheel {
			s.wheel(c, p, in)
			return
		}
		if in.Key != nil || in.Paste != "" {
			delete(c.scroll, p.id) // typing returns to the live screen
			if !p.typed {
				p.typed = true
				defer s.broadcastState(ws) // the pane is no longer fresh
			}
		}
		p.sendInput(in)
	case protocol.TypeResize:
		var r protocol.Resize
		if f.Decode(&r) == nil {
			c.cols, c.rows = r.Cols, r.Rows
			s.resize(ws, r.Cols, r.Rows)
			s.broadcastView(ws)
		}
	case protocol.TypeCommand:
		var cmd protocol.Command
		if f.Decode(&cmd) == nil {
			s.command(c, cmd)
		}
	}
}

func (s *Server) command(c *client, cmd protocol.Command) {
	ws := c.ws
	tab := ws.activeTab()
	area := ws.area()
	n := len(ws.tabs)

	switch cmd.Action {
	// Tabs.
	case protocol.ActionNewTab:
		ws.lastTab = tab.id
		if err := s.newTab(ws, cmd.Arg); err != nil {
			log.Printf("new tab: %v", err)
			return
		}
	case protocol.ActionNextTab:
		s.selectTab(ws, (ws.active+1)%n)
	case protocol.ActionPrevTab:
		s.selectTab(ws, (ws.active-1+n)%n)
	case protocol.ActionLastTab:
		for i, t := range ws.tabs {
			if t.id == ws.lastTab {
				s.selectTab(ws, i)
			}
		}
	case protocol.ActionSelectTab:
		if i, err := strconv.Atoi(cmd.Arg); err == nil && i >= 0 && i < n {
			s.selectTab(ws, i)
		}
	case protocol.ActionCloseTab:
		// Each pane's wait goroutine notices its exit and cleans up.
		for _, p := range tab.panes {
			_ = p.pty.Kill()
		}
		return
	case protocol.ActionRenameTab:
		if name := truncate(strings.TrimSpace(cmd.Arg), 30); name != "" {
			tab.name = name
		}
	case protocol.ActionTogglePin:
		s.togglePin(tab)
		return
	case protocol.ActionGotoTab:
		s.gotoTab(c, parseID(cmd.Arg))
		return
	case protocol.ActionNextPinned:
		s.cyclePinned(c, 1)
		return
	case protocol.ActionPrevPinned:
		s.cyclePinned(c, -1)
		return
	case protocol.ActionScrollReset:
		id, _ := strconv.ParseUint(cmd.Arg, 10, 32)
		delete(c.scroll, uint32(id))
		return

	// Panes.
	case protocol.ActionSplitRight, protocol.ActionSplitDown:
		dir := layout.Row
		if cmd.Action == protocol.ActionSplitDown {
			dir = layout.Column
		}
		if err := s.splitPane(ws, tab, dir); err != nil {
			log.Printf("split: %v", err)
			return
		}
	case protocol.ActionFocusLeft, protocol.ActionFocusRight, protocol.ActionFocusUp, protocol.ActionFocusDown:
		d := map[string]layout.Direction{
			protocol.ActionFocusLeft: layout.Left, protocol.ActionFocusRight: layout.Right,
			protocol.ActionFocusUp: layout.Up, protocol.ActionFocusDown: layout.Down,
		}[cmd.Action]
		rects, _ := tab.tree.Layout(area)
		if id := layout.Neighbor(rects, tab.active, d); id != 0 {
			s.unzoom(ws, tab)
			tab.focus(id)
		}
	case protocol.ActionFocusPane:
		id, _ := strconv.ParseUint(cmd.Arg, 10, 32)
		tab.focus(uint32(id))
	case protocol.ActionNextPane:
		ids := tab.tree.Panes()
		for i, id := range ids {
			if id == tab.active {
				s.unzoom(ws, tab)
				tab.focus(ids[(i+1)%len(ids)])
				break
			}
		}
	case protocol.ActionClosePane:
		_ = tab.activePane().pty.Kill()
		return
	case protocol.ActionZoom:
		if len(tab.panes) > 1 || tab.zoomed {
			tab.zoomed = !tab.zoomed
			tab.relayout(area)
		}
	case protocol.ActionResizeL, protocol.ActionResizeR, protocol.ActionResizeU, protocol.ActionResizeD:
		d := map[string]layout.Direction{
			protocol.ActionResizeL: layout.Left, protocol.ActionResizeR: layout.Right,
			protocol.ActionResizeU: layout.Up, protocol.ActionResizeD: layout.Down,
		}[cmd.Action]
		cells, err := strconv.Atoi(cmd.Arg)
		if err != nil || cells <= 0 {
			cells = 1
		}
		s.unzoom(ws, tab)
		if tab.tree.Resize(tab.active, d, cells, area) {
			tab.relayout(area)
		}
	case protocol.ActionDragSplit:
		var node uint32
		var pos int
		if _, err := fmt.Sscanf(cmd.Arg, "%d %d", &node, &pos); err == nil && !tab.zoomed {
			if tab.tree.SetDivider(node, pos, area) {
				tab.relayout(area)
			}
		}

	// Workspaces.
	case protocol.ActionNewWorkspace:
		if existing := s.workspaces[strings.TrimSpace(cmd.Arg)]; existing != nil {
			s.join(c, existing) // opening a saved workspace that's already running
			return
		}
		dir := c.dir
		if cmd.Dir != "" {
			dir = cmd.Dir
		}
		nws, err := s.createWorkspace(cmd.Arg, dir, c.cols, c.rows)
		if err != nil {
			log.Printf("new workspace: %v", err)
			return
		}
		s.join(c, nws)
		return
	case protocol.ActionSwitchWorkspace:
		if nws := s.workspaces[cmd.Arg]; nws != nil && nws != ws {
			s.join(c, nws)
		}
		return
	case protocol.ActionNextWorkspace, protocol.ActionPrevWorkspace:
		all := s.sortedWorkspaces()
		for i, w := range all {
			if w == ws {
				step := 1
				if cmd.Action == protocol.ActionPrevWorkspace {
					step = len(all) - 1
				}
				if next := all[(i+step)%len(all)]; next != ws {
					s.join(c, next)
				}
				break
			}
		}
		return
	case protocol.ActionRenameWorkspace:
		name := truncate(strings.TrimSpace(cmd.Arg), 32)
		if name == "" || name == ws.name || s.workspaces[name] != nil {
			return
		}
		delete(s.workspaces, ws.name)
		ws.name = name
		s.workspaces[name] = ws
		s.broadcastStates()
		return
	case protocol.ActionKillWorkspace:
		target := ws
		if cmd.Arg != "" {
			target = s.workspaces[cmd.Arg]
		}
		if target != nil {
			s.killWorkspace(target)
		}
		return
	case protocol.ActionKillServer:
		s.killServer()
		return
	default:
		return
	}
	s.broadcastView(ws)
}

// wheelLines is how far one wheel notch scrolls.
const wheelLines = 3

// wheel handles a mouse wheel notch over pane p. Programs that track the
// mouse get the event; full-screen programs without mouse support (less,
// man) get arrow keys, as most terminals do; a plain shell scrolls back
// through history.
func (s *Server) wheel(c *client, p *Pane, in protocol.Input) {
	up := uv.MouseButton(in.Mouse.Button) == uv.MouseWheelUp
	switch {
	case p.wantsMouse():
		p.sendInput(in)
	case p.emu.IsAltScreen():
		code := uv.KeyDown
		if up {
			code = uv.KeyUp
		}
		for range wheelLines {
			p.emu.SendKey(uv.KeyPressEvent{Code: code})
		}
	default:
		off := c.scroll[p.id]
		if up {
			off += wheelLines
		} else {
			off -= wheelLines
		}
		off = min(max(off, 0), p.emu.ScrollbackLen())
		if off == c.scroll[p.id] {
			return
		}
		if off == 0 {
			delete(c.scroll, p.id)
		} else {
			c.scroll[p.id] = off
		}
		c.queueJSON(protocol.TypeHistory, p.history(off))
	}
}

func (s *Server) selectTab(ws *Workspace, i int) {
	if i == ws.active {
		return
	}
	ws.lastTab = ws.tabs[ws.active].id
	ws.active = i
}

func (s *Server) unzoom(ws *Workspace, t *Tab) {
	if t.zoomed {
		t.zoomed = false
		t.relayout(ws.area())
	}
}

func (s *Server) resize(ws *Workspace, cols, rows int) {
	if cols <= 0 || rows <= 0 || cols == ws.cols && rows == ws.rows {
		return
	}
	ws.cols, ws.rows = cols, rows
	for _, t := range ws.tabs {
		t.relayout(ws.area())
	}
}

// --- tabs and panes ----------------------------------------------------------

// shellDir picks where a new shell starts: the focused pane's directory if
// the shell reports it (OSC 7), otherwise the workspace's directory.
func shellDir(ws *Workspace) string {
	if t := ws.activeTab(); t != nil {
		if p := t.activePane(); p != nil && p.cwd != "" {
			return p.cwd
		}
	}
	return ws.dir
}

// newTab starts a shell in a new tab placed after the active one, and makes
// it active. The caller broadcasts the change.
func (s *Server) newTab(ws *Workspace, name string) error {
	name = truncate(strings.TrimSpace(name), 30)
	if name == "" {
		name = randomTabName(ws)
	}
	argv := platform.Shell()
	area := ws.area()
	p, err := newPane(s.newID(), argv, shellDir(ws), area.W, area.H)
	if err != nil {
		return err
	}
	t := &Tab{id: s.newID(), name: name, tree: layout.New(p.id), panes: map[uint32]*Pane{p.id: p}, active: p.id}
	at := 0
	if len(ws.tabs) > 0 {
		at = ws.active + 1
	}
	ws.tabs = append(ws.tabs[:at], append([]*Tab{t}, ws.tabs[at:]...)...)
	ws.active = at
	s.startPane(ws, t, p)
	return nil
}

// splitPane splits the tab's focused pane and focuses the new one.
func (s *Server) splitPane(ws *Workspace, t *Tab, dir layout.Dir) error {
	s.unzoom(ws, t)
	argv := platform.Shell()
	id := s.newID()
	if !t.tree.Split(t.active, id, dir) {
		return errors.New("focused pane not in layout")
	}
	rects, _ := t.tree.Layout(ws.area())
	r := rects[id]
	p, err := newPane(id, argv, shellDir(ws), max(r.W, 1), max(r.H, 1))
	if err != nil {
		t.tree.Remove(id)
		return err
	}
	t.panes[id] = p
	t.focus(id)
	t.relayout(ws.area())
	s.startPane(ws, t, p)
	return nil
}

func (s *Server) startPane(ws *Workspace, t *Tab, p *Pane) {
	p.ws, p.tab = ws, t
	go p.drainInput()
	go s.pumpOutput(p)
	go func() {
		err := p.pty.Wait()
		log.Printf("pane %d exited: %v", p.id, err)
		// On Windows the ConPTY output pipe stays open until the pseudo
		// console is closed, so close it to unblock pumpOutput.
		_ = p.pty.Close()
		s.mu.Lock()
		s.paneExited(p)
		s.mu.Unlock()
	}()
}

// visible reports whether p is on screen for its workspace's clients.
func (p *Pane) visible() bool {
	t := p.ws.activeTab()
	return t == p.tab && (!t.zoomed || t.active == p.id)
}

// pumpOutput feeds PTY output into the pane's emulator and forwards it to
// clients viewing the pane.
func (s *Server) pumpOutput(p *Pane) {
	buf := make([]byte, 32<<10)
	for {
		n, err := p.pty.Read(buf)
		if n > 0 {
			s.mu.Lock()
			if !p.exited {
				_, _ = p.emu.Write(buf[:n])
				p.noteOutput(time.Now())
				if p.visible() {
					frame := protocol.EncodeFrame(protocol.TypeOutput, protocol.EncodeOutput(p.id, buf[:n]))
					for c := range p.ws.clients {
						c.queue(frame)
					}
				}
				if p.stateChanged {
					p.stateChanged = false
					s.broadcastState(p.ws)
				}
				if p.notified {
					s.noticeBell(p)
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) paneExited(p *Pane) {
	if p.exited {
		return
	}
	p.exited = true
	vtx.Stop(p.emu)

	ws, t := p.ws, p.tab
	t.tree.Remove(p.id)
	delete(t.panes, p.id)

	if t.tree.Empty() {
		i := ws.tabIndex(t)
		if i < 0 {
			return
		}
		ws.tabs = append(ws.tabs[:i], ws.tabs[i+1:]...)
		if len(ws.tabs) == 0 {
			s.closeWorkspace(ws, "workspace "+ws.name+" exited")
			return
		}
		if ws.active > i || ws.active >= len(ws.tabs) {
			ws.active = max(ws.active-1, 0)
		}
	} else {
		if t.active == p.id {
			t.active = 0
			t.focus(t.lastPane)
			if t.active == 0 {
				t.active = t.tree.Panes()[0]
			}
		}
		t.zoomed = false
		t.relayout(ws.area())
	}
	s.broadcastView(ws)
}

// --- broadcasting ------------------------------------------------------------

func (s *Server) state(ws *Workspace) protocol.State {
	st := protocol.State{Workspace: ws.name, Workspaces: s.workspaceInfos(), Active: ws.active, Pinned: s.pinnedList()}
	for _, t := range ws.tabs {
		info := protocol.TabInfo{ID: t.id, Name: t.name, Panes: len(t.panes), Pinned: t.pinned()}
		if info.Pinned {
			info.Status = t.status()
		}
		st.Tabs = append(st.Tabs, info)
	}
	t := ws.activeTab()
	if t == nil {
		return st
	}
	rects, divs := t.view(ws.area())
	for _, id := range t.tree.Panes() {
		if r, ok := rects[id]; ok {
			p := t.panes[id]
			st.Panes = append(st.Panes, protocol.PaneInfo{ID: id, X: r.X, Y: r.Y, W: r.W, H: r.H, Title: p.title,
				Program: p.program, Agent: p.agent, Cwd: p.cwd, Mouse: p.wantsMouse(), Fresh: !p.typed})
		}
	}
	for _, d := range divs {
		st.Dividers = append(st.Dividers, protocol.Divider{Node: d.Node, X: d.X, Y: d.Y, Len: d.Len, Vertical: d.Vertical})
	}
	st.ActivePane = t.active
	st.Zoomed = t.zoomed
	return st
}

func (s *Server) broadcastState(ws *Workspace) {
	st := s.state(ws)
	for c := range ws.clients {
		c.queueJSON(protocol.TypeState, st)
	}
}

// broadcastStates refreshes every workspace's clients, e.g. after the list
// of workspaces changed.
func (s *Server) broadcastStates() {
	for _, ws := range s.workspaces {
		s.broadcastState(ws)
	}
}

// sendView sends c the layout and a snapshot of every visible pane.
func (s *Server) sendView(c *client) {
	ws := c.ws
	c.scroll = map[uint32]int{} // fresh snapshots replace any history view
	if t := ws.activeTab(); t != nil {
		t.attention = false // the user is looking at it now
	}
	c.queueJSON(protocol.TypeState, s.state(ws))
	t := ws.activeTab()
	if t == nil {
		return
	}
	rects, _ := t.view(ws.area())
	for id := range rects {
		if p := t.panes[id]; p != nil {
			c.queueJSON(protocol.TypeSnapshot, p.snapshot())
		}
	}
}

func (s *Server) broadcastView(ws *Workspace) {
	for c := range ws.clients {
		s.sendView(c)
	}
}
