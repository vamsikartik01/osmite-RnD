package daemon_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/daemon"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

// TestEndToEnd runs a real daemon with a real shell on a real PTY (ConPTY on
// Windows), types a command, detaches, reattaches and checks the output
// survived.
func TestEndToEnd(t *testing.T) {
	sock := filepath.Join(shortTempDir(t), "t.sock")
	t.Setenv("OTMUX_SOCKET", sock)
	t.Setenv("OTMUX_CONFIG", filepath.Join(t.TempDir(), "config.json")) // keeps the real remote.json out
	if runtime.GOOS == "windows" {
		t.Setenv("OTMUX_SHELL", "cmd.exe")
	} else {
		t.Setenv("OTMUX_SHELL", "/bin/sh")
	}

	ran := make(chan error, 1)
	go func() { ran <- daemon.Run() }()

	c := attach(t, sock)
	st := c.waitState(t, func(s protocol.State) bool { return len(s.Tabs) == 1 && len(s.Panes) == 1 })
	pane := st.ActivePane
	if name := st.Tabs[0].Name; !strings.HasSuffix(name, "ite") {
		t.Fatalf("first tab should get a mineral name ending in -ite, got %q", name)
	}

	const marker = "otmux-e2e-42"
	for _, r := range "echo " + marker {
		c.send(t, protocol.TypeInput, protocol.Input{Pane: pane, Key: &protocol.Key{Code: r, Text: string(r)}})
	}
	c.send(t, protocol.TypeInput, protocol.Input{Pane: pane, Key: &protocol.Key{Code: uv.KeyEnter}})
	// The marker appears twice: once as the typed command, once as output.
	c.waitScreen(t, pane, func(s string) bool { return strings.Count(s, marker) >= 2 })

	// Split: two panes side by side with a divider, the new one focused.
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionSplitRight})
	st = c.waitState(t, func(s protocol.State) bool { return len(s.Panes) == 2 && len(s.Dividers) == 1 })
	if st.ActivePane == pane || !st.Dividers[0].Vertical {
		t.Fatalf("after split-right: %+v", st)
	}
	right := st.ActivePane
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionFocusLeft})
	c.waitState(t, func(s protocol.State) bool { return s.ActivePane == pane })
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionZoom})
	c.waitState(t, func(s protocol.State) bool { return s.Zoomed && len(s.Panes) == 1 })
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionZoom})
	c.waitState(t, func(s protocol.State) bool { return !s.Zoomed && len(s.Panes) == 2 })
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionFocusPane, Arg: fmt.Sprint(right)})
	c.waitState(t, func(s protocol.State) bool { return s.ActivePane == right })
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionClosePane})
	c.waitState(t, func(s protocol.State) bool { return len(s.Panes) == 1 && s.ActivePane == pane })

	// Wheel up over a plain shell scrolls back through history.
	for i := 0; i < 60; i++ {
		typeLine(t, c, pane, "echo scroll-"+fmt.Sprint(i))
	}
	c.waitScreen(t, pane, func(s string) bool { return strings.Contains(s, "scroll-59") })
	wheelUp := &protocol.Mouse{Kind: protocol.MouseWheel, Button: int(uv.MouseWheelUp)}
	c.send(t, protocol.TypeInput, protocol.Input{Pane: pane, Mouse: wheelUp})
	c.wait(t, "history view", func() bool { return c.history.Offset == 3 && c.history.Total > 0 })
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionScrollReset, Arg: fmt.Sprint(pane)})

	// A named tab, then a second workspace, then back: the workspace comes
	// back on the tab that was in use, not tab 0.
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionNewTab, Arg: "api"})
	c.waitState(t, func(s protocol.State) bool { return len(s.Tabs) == 2 && s.Active == 1 && s.Tabs[1].Name == "api" })
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionNewWorkspace, Arg: "other"})
	c.waitState(t, func(s protocol.State) bool { return s.Workspace == "other" && len(s.Workspaces) == 2 })
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionSwitchWorkspace, Arg: "e2e"})
	c.waitState(t, func(s protocol.State) bool { return s.Workspace == "e2e" })
	if c.state.Active != 1 {
		t.Fatalf("switching back should restore tab 1, got tab %d", c.state.Active)
	}
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionSelectTab, Arg: "0"})
	c.waitState(t, func(s protocol.State) bool { return s.Active == 0 })
	c.conn.Close() // detach

	// Reattach: the shell kept running and the screen comes back.
	c = attach(t, sock)
	c.waitState(t, func(s protocol.State) bool { return len(s.Tabs) == 2 })
	c.waitScreen(t, pane, func(s string) bool { return strings.Contains(s, "scroll-59") })
	t.Logf("screen after reattach:\n%s", stripANSI(c.screen[pane].text.String()))
	c.conn.Close()

	k := attachRaw(t, sock)
	if err := k.Send(protocol.TypeCommand, protocol.Command{Action: protocol.ActionKillServer}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ran:
		if err != nil {
			t.Fatalf("daemon: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not exit after kill-server")
	}
}

type testClient struct {
	conn      *protocol.Conn
	frames    chan protocol.Frame
	screen    map[uint32]*mirror
	state     protocol.State
	history   protocol.History
	snapshots int
}

// mirror rebuilds pane text from snapshots and output, like the real client.
type mirror struct{ text strings.Builder }

func attachRaw(t *testing.T, sock string) *protocol.Conn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		nc, err := net.Dial("unix", sock)
		if err == nil {
			return protocol.NewConn(nc)
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func attach(t *testing.T, sock string) *testClient {
	t.Helper()
	return attachSized(t, sock, 100, 30)
}

func attachSized(t *testing.T, sock string, cols, rows int) *testClient {
	t.Helper()
	conn := attachRaw(t, sock)
	c := &testClient{conn: conn, frames: make(chan protocol.Frame, 1024), screen: map[uint32]*mirror{}}
	c.send(t, protocol.TypeHello, protocol.Hello{Version: protocol.Version, Workspace: "e2e", Cols: cols, Rows: rows})
	go func() {
		defer close(c.frames)
		for {
			f, err := conn.Read()
			if err != nil {
				return
			}
			c.frames <- f
		}
	}()
	return c
}

// typeLine types text and Enter into pane.
func typeLine(t *testing.T, c *testClient, pane uint32, text string) {
	t.Helper()
	for _, r := range text {
		c.send(t, protocol.TypeInput, protocol.Input{Pane: pane, Key: &protocol.Key{Code: r, Text: string(r)}})
	}
	c.send(t, protocol.TypeInput, protocol.Input{Pane: pane, Key: &protocol.Key{Code: uv.KeyEnter}})
}

func (c *testClient) send(t *testing.T, typ protocol.Type, v any) {
	t.Helper()
	if err := c.conn.Send(typ, v); err != nil {
		t.Fatal(err)
	}
}

func (c *testClient) apply(t *testing.T, f protocol.Frame) {
	switch f.Type {
	case protocol.TypeWelcome:
		var w protocol.Welcome
		if f.Decode(&w) == nil && w.Error != "" {
			t.Fatalf("welcome error: %s", w.Error)
		}
	case protocol.TypeState:
		var st protocol.State
		if f.Decode(&st) == nil {
			c.state = st
		}
	case protocol.TypeSnapshot:
		var s protocol.Snapshot
		if f.Decode(&s) == nil {
			c.snapshots++
			m := &mirror{}
			m.text.WriteString(s.Data)
			c.screen[s.Pane] = m
		}
	case protocol.TypeHistory:
		_ = f.Decode(&c.history)
	case protocol.TypeOutput:
		if pane, data, err := protocol.DecodeOutput(f.Payload); err == nil {
			if m := c.screen[pane]; m != nil {
				m.text.Write(data)
			}
		}
	}
}

func (c *testClient) wait(t *testing.T, what string, ok func() bool) {
	t.Helper()
	timeout := time.After(20 * time.Second)
	for !ok() {
		select {
		case f, open := <-c.frames:
			if !open {
				t.Fatalf("connection closed waiting for %s", what)
			}
			c.apply(t, f)
		case <-timeout:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func (c *testClient) waitState(t *testing.T, ok func(protocol.State) bool) protocol.State {
	t.Helper()
	c.wait(t, "state", func() bool { return ok(c.state) })
	return c.state
}

func (c *testClient) waitScreen(t *testing.T, pane uint32, ok func(string) bool) {
	t.Helper()
	c.wait(t, "screen text", func() bool {
		m := c.screen[pane]
		return m != nil && ok(stripANSI(m.text.String()))
	})
}

// stripANSI removes escape sequences well enough to search for plain text.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
		} else if i+1 < len(s) && s[i+1] == ']' {
			for i < len(s) && s[i] != 0x07 && !(s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\') {
				i++
			}
		} else {
			i++
		}
	}
	return b.String()
}

// shortTempDir returns a temp dir with a short path: unix socket paths are
// limited to ~104 bytes on macOS, and t.TempDir() can exceed that.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ot")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// TestSynchronizedUpdateArrivesWhole has a real shell write one synchronized
// update (mode 2026) in pieces with pauses between them, as agents redrawing
// a large screen do, and checks clients get it as one Output frame instead of
// drawing it half done.
func TestSynchronizedUpdateArrivesWhole(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	sock := filepath.Join(shortTempDir(t), "t.sock")
	t.Setenv("OTMUX_SOCKET", sock)
	t.Setenv("OTMUX_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("OTMUX_SHELL", "/bin/sh")
	go func() { _ = daemon.Run() }()
	t.Cleanup(func() {
		k := attachRaw(t, sock)
		_ = k.Send(protocol.TypeCommand, protocol.Command{Action: protocol.ActionKillServer})
	})

	c := attach(t, sock)
	pane := c.waitState(t, func(s protocol.State) bool { return len(s.Panes) == 1 }).ActivePane
	typeLine(t, c, pane, `printf '\033[?2026h'; for i in 1 2 3 4 5; do printf "pt$i. "; sleep 0.01; done; printf '\033[?2026l\n'`)

	timeout := time.After(20 * time.Second)
	for {
		select {
		case f, open := <-c.frames:
			if !open {
				t.Fatal("connection closed")
			}
			if f.Type != protocol.TypeOutput {
				continue
			}
			_, data, _ := protocol.DecodeOutput(f.Payload)
			if s := string(data); strings.Contains(s, "pt1. ") {
				if !strings.Contains(s, "pt5. ") {
					t.Fatalf("update arrived in parts; first frame: %q", s)
				}
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for the update")
		}
	}
}

// TestSizeFollowsTyping attaches a big client and then a small one: the
// workspace keeps the big size until someone types in the small one, and
// commands that don't change the layout send no fresh snapshots.
func TestSizeFollowsTyping(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	sock := filepath.Join(shortTempDir(t), "t.sock")
	t.Setenv("OTMUX_SOCKET", sock)
	t.Setenv("OTMUX_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("OTMUX_SHELL", "/bin/sh")
	go func() { _ = daemon.Run() }()
	t.Cleanup(func() {
		k := attachRaw(t, sock)
		_ = k.Send(protocol.TypeCommand, protocol.Command{Action: protocol.ActionKillServer})
	})

	big := attachSized(t, sock, 100, 30)
	pane := big.waitState(t, func(s protocol.State) bool { return len(s.Panes) == 1 }).ActivePane
	bigW := big.state.Panes[0].W

	small := attachSized(t, sock, 60, 20)
	small.waitState(t, func(s protocol.State) bool { return len(s.Panes) == 1 })
	if w := small.state.Panes[0].W; w != bigW {
		t.Fatalf("attaching a second client resized the workspace: width %d, want %d", w, bigW)
	}

	typeLine(t, small, pane, "echo from-small")
	small.waitScreen(t, pane, func(s string) bool { return strings.Count(s, "from-small") >= 2 })
	big.waitState(t, func(s protocol.State) bool { return len(s.Panes) == 1 && s.Panes[0].W < bigW })
	typeLine(t, big, pane, "echo from-big")
	big.waitState(t, func(s protocol.State) bool { return len(s.Panes) == 1 && s.Panes[0].W == bigW })
	big.waitScreen(t, pane, func(s string) bool { return strings.Count(s, "from-big") >= 2 })

	// Focusing the pane that's already focused redraws nothing.
	before := big.snapshots
	big.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionFocusPane, Arg: fmt.Sprint(pane)})
	typeLine(t, big, pane, "echo after-focus")
	big.waitScreen(t, pane, func(s string) bool { return strings.Count(s, "after-focus") >= 2 })
	if big.snapshots != before {
		t.Fatalf("%d snapshots for a command that changed nothing", big.snapshots-before)
	}
}
