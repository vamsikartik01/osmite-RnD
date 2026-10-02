package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/pty"
)

// TestTUI builds otmux, runs it inside a pseudo-terminal like a user would,
// and drives it with keystrokes: run a command, open a tab, detach, and check
// the workspace is still listed afterwards.
func TestTUI(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the real binary")
	}
	dir := shortTempDir(t)
	bin := filepath.Join(dir, "otmux")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	shell := "/bin/sh"
	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
	}
	// A private settings file with one saved workspace, so the test never
	// touches the real one.
	cfgPath := filepath.Join(dir, "config.json")
	labDir := filepath.Join(dir, "lab")
	if err := os.MkdirAll(labDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgJSON, _ := json.Marshal(map[string]any{"workspaces": []map[string]string{{"name": "lab", "path": labDir}}})
	if err := os.WriteFile(cfgPath, cfgJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"OTMUX_SOCKET=" + filepath.Join(dir, "s.sock"), "OTMUX_SHELL=" + shell, "OTMUX_PANE=",
		"OTMUX_CONFIG=" + cfgPath, "OTMUX_PREFIX=", "OTMUX_NO_UPDATE=1",
	}
	t.Cleanup(func() { runOtmux(t, env, bin, "kill-server") })

	term := startTerm(t, bin, env, 100, 30)

	term.waitFor(t, "status bar", func(s string) bool { return currentWS(s, "smoke") && strings.Contains(s, "for shortcuts") })
	if !term.modeEnabled(ansi.ModeMouseExtSgr) {
		t.Fatal("client must request SGR mouse encoding; legacy encoding prints junk in Windows Terminal")
	}

	// A fresh tab shows the welcome splash; typing makes it go away.
	term.waitFor(t, "splash", func(s string) bool {
		return strings.Contains(s, "terminal workspaces") && strings.Contains(s, "from osmite RnD") && strings.Contains(s, "█")
	})
	t.Logf("splash:\n%s", term.screen())

	term.typ("echo tui-check-7\r")
	term.waitFor(t, "splash gone", func(s string) bool { return !strings.Contains(s, "terminal workspaces") })
	term.waitFor(t, "command output", func(s string) bool { return strings.Count(s, "tui-check-7") >= 2 })

	// Resizing the outer terminal resizes otmux: the status bar moves to the
	// new last row.
	term.resize(90, 24)
	term.waitFor(t, "resize", func(s string) bool {
		lines := strings.Split(s, "\n")
		return len(lines) == 24 && strings.Contains(lines[23], " smoke ")
	})

	// The prefix turns the bar into a key guide.
	term.typ("\x02") // ctrl+b
	term.waitFor(t, "prefix hints", func(s string) bool { return strings.Contains(s, "v split │") && strings.Contains(s, "h split ─") })

	// The cursor stays at the shell prompt even though the bar at the bottom
	// was drawn last. (ultraviolet used to leave it after the last drawn
	// cell until the next frame, so it showed up in the wrong place.)
	time.Sleep(300 * time.Millisecond)
	if x, y, line := term.cursorLine(); y >= 23 || x < len([]rune(strings.TrimRight(line, " "))) {
		t.Fatalf("cursor at x=%d y=%d, want at the end of the prompt line %q", x, y, line)
	}

	// v splits side by side: a vertical divider appears, old output stays left.
	term.typ("v")
	term.waitFor(t, "split", func(s string) bool {
		return strings.Count(s, "│") >= 20 && strings.Contains(s, "tui-check-7") && !strings.Contains(s, "h split")
	})
	t.Logf("screen after split:\n%s", term.screen())

	// New tab asks for a name.
	term.typ("\x02c")
	term.waitFor(t, "new tab prompt", func(s string) bool { return strings.Contains(s, "New tab") })
	term.typ("api\r")
	term.waitFor(t, "named tab", func(s string) bool {
		return strings.Contains(s, "1 api") && !strings.Contains(s, "tui-check-7")
	})

	// Prefix + space opens the command palette.
	term.typ("\x02 ")
	term.waitFor(t, "palette", func(s string) bool {
		return strings.Contains(s, "Commands") && strings.Contains(s, "Split side by side")
	})
	t.Logf("palette:\n%s", term.screen())
	term.typ("\x1b")
	term.waitFor(t, "palette closed", func(s string) bool { return !strings.Contains(s, "Split side by side") })

	// Mouse: clicking tab 0 in the status bar switches back to it.
	term.clickStatus(t, " 0 ")
	term.waitFor(t, "click switches tab", func(s string) bool { return strings.Contains(s, "tui-check-7") })
	term.clickStatus(t, "1 api")
	term.waitFor(t, "click back to api", func(s string) bool { return !strings.Contains(s, "tui-check-7") })

	// The sidebar lists workspaces (running and saved) and pinned tabs.
	term.waitFor(t, "sidebar", func(s string) bool {
		w, ws := strings.Index(s, "WATCH"), strings.Index(s, "WORKSPACES")
		return w >= 0 && ws > w && strings.Contains(s, "saved") // watch list on top
	})

	// Pin the api tab by hand: it shows in the pinned list.
	term.typ("\x02m")
	term.waitFor(t, "pinned", func(s string) bool { return strings.Contains(s, "○ api") })

	// Workspace picker: typing a new name creates and switches to it.
	term.typ("\x02w")
	term.waitFor(t, "picker", func(s string) bool { return strings.Contains(s, "Workspaces") && strings.Contains(s, "saved") })
	term.typ("web\r")
	term.waitFor(t, "new workspace", func(s string) bool {
		return currentWS(s, "web") && strings.Contains(s, " smoke ")
	})
	t.Logf("sidebar with two workspaces:\n%s", term.screen())

	// From another workspace, clicking the pinned tab jumps straight to it.
	term.click(t, "○ api")
	term.waitFor(t, "click pinned tab", func(s string) bool {
		return currentWS(s, "smoke") && strings.Contains(lastLine(s), "api")
	})

	// Clicking a workspace in the sidebar switches to it, on its last tab.
	term.click(t, " web ")
	term.waitFor(t, "click web", func(s string) bool { return currentWS(s, "web") })
	term.click(t, " smoke ")
	term.waitFor(t, "click switches workspace", func(s string) bool {
		return currentWS(s, "smoke") && strings.Contains(lastLine(s), "api")
	})

	// Settings: Appearance › calcite applies the theme and saves it.
	term.typ("\x02s")
	term.waitFor(t, "settings", func(s string) bool { return strings.Contains(s, "Settings") && strings.Contains(s, "Appearance") })
	term.typ("\x1b[B") // down: Appearance
	term.waitFor(t, "appearance section", func(s string) bool { return strings.Count(s, "Appearance") >= 2 && strings.Contains(s, "calcite") })
	term.click(t, "      calcite") // the theme row (a tab may be named calcite too)
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, _ := os.ReadFile(cfgPath)
		if strings.Contains(string(data), `"theme": "calcite"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("settings file after choosing calcite: %s\nscreen:\n%s", data, term.screen())
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("settings:\n%s", term.screen())
	term.typ("\x1b")
	term.waitFor(t, "settings closed", func(s string) bool { return !strings.Contains(s, "Appearance") })

	// A saved workspace opens from the sidebar, in its folder.
	term.click(t, "saved")
	term.waitFor(t, "saved workspace", func(s string) bool { return currentWS(s, "lab") })

	// Remove it from the saved list in settings: it's forgotten, but the
	// running workspace carries on.
	term.typ("\x02s")
	term.waitFor(t, "settings again", func(s string) bool { return strings.Contains(s, "removes it from the list") })
	term.typ("\r") // into the list: lab
	term.waitFor(t, "remove hint", func(s string) bool { return strings.Contains(s, "d remove") && strings.Contains(s, " ✕ ") })
	term.typ("d")
	term.waitFor(t, "removed", func(s string) bool { return strings.Contains(s, "Removed lab") })
	if data, _ := os.ReadFile(cfgPath); strings.Contains(string(data), `"lab"`) {
		t.Fatalf("lab still saved after removing it: %s", data)
	}
	term.typ("\x1b")
	term.waitFor(t, "lab still running", func(s string) bool {
		return !strings.Contains(s, "Appearance") && currentWS(s, "lab") && !strings.Contains(s, "saved")
	})

	// Narrower than 90 columns, the sidebar gives way to two bars: tabs on
	// top, every workspace at the bottom.
	term.resize(80, 24)
	term.waitFor(t, "two bars", func(s string) bool {
		lines := strings.Split(s, "\n")
		last := lastLine(s)
		return !strings.Contains(s, "WORKSPACES") && strings.HasSuffix(strings.TrimSpace(lines[0]), "+") &&
			strings.Contains(last, " lab ") && strings.Contains(last, "smoke") && strings.Contains(last, "web")
	})
	t.Logf("two bars:\n%s", term.screen())

	// The detach button.
	term.clickStatus(t, "detach")
	select {
	case <-term.exited:
	case <-time.After(10 * time.Second):
		t.Fatalf("client did not exit on detach; screen:\n%s", term.screen())
	}

	out := runOtmux(t, env, bin, "ls")
	if !strings.Contains(out, "smoke: 2 tabs") || !strings.Contains(out, "web: 1 tab") || !strings.Contains(out, "lab: 1 tab") {
		t.Fatalf("ls after detach = %q, want smoke (2 tabs), web and lab\n%s", out, daemonLog(dir))
	}
	if log := daemonLog(dir); !strings.Contains(log, `workspace "lab" created in `+labDir) {
		t.Fatalf("lab should start in its saved folder %s\n%s", labDir, log)
	}
}

// currentWS reports whether name is the current workspace, read from the
// bottom bar, which starts with it in the sidebar layout.
func currentWS(s, name string) bool {
	return strings.HasPrefix(strings.TrimSpace(lastLine(s)), name+" ")
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

func runOtmux(t *testing.T, env []string, bin string, args ...string) string {
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("%s[exit: %v]", out, err)
	}
	return string(out)
}

type term struct {
	p      pty.PTY
	mu     sync.Mutex
	emu    *vt.Emulator
	modes  map[ansi.Mode]bool // modes the client enabled on its terminal
	exited chan struct{}
}

func startTerm(t *testing.T, bin string, env []string, cols, rows int) *term {
	t.Helper()
	return startTermArgs(t, bin, []string{"new", "smoke"}, env, cols, rows)
}

func startTermArgs(t *testing.T, bin string, args, env []string, cols, rows int) *term {
	t.Helper()
	p, err := pty.Start(pty.Options{Argv: append([]string{bin}, args...), Env: env, Cols: cols, Rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	tm := &term{p: p, emu: vt.NewEmulator(cols, rows), modes: map[ansi.Mode]bool{}, exited: make(chan struct{})}
	tm.emu.SetCallbacks(vt.Callbacks{
		EnableMode:  func(m ansi.Mode) { tm.modes[m] = true },
		DisableMode: func(m ansi.Mode) { tm.modes[m] = false },
	})
	// Answer the client's terminal queries (it asks about features at start).
	go func() { _, _ = io.Copy(p, tm.emu) }()
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := p.Read(buf)
			tm.mu.Lock()
			_, _ = tm.emu.Write(buf[:n])
			tm.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	go func() {
		_ = p.Wait()
		close(tm.exited)
		_ = p.Close()
	}()
	t.Cleanup(func() { _ = p.Kill(); _ = p.Close() })
	return tm
}

func (tm *term) typ(s string) { _, _ = tm.p.Write([]byte(s)) }

// cursorLine returns the cursor position and the text of its line.
func (tm *term) cursorLine() (x, y int, line string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	pos := tm.emu.CursorPosition()
	lines := strings.Split(tm.emu.String(), "\n")
	if pos.Y < len(lines) {
		line = lines[pos.Y]
	}
	return pos.X, pos.Y, line
}

func (tm *term) modeEnabled(m ansi.Mode) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.modes[m]
}

// resize changes the size of the terminal otmux runs in, like dragging the
// window edge.
func (tm *term) resize(cols, rows int) {
	tm.mu.Lock()
	tm.emu.Resize(cols, rows)
	tm.mu.Unlock()
	_ = tm.p.Resize(cols, rows)
}

// clickStatus clicks text in the status bar (the last row).
func (tm *term) clickStatus(t *testing.T, text string) {
	t.Helper()
	lines := strings.Split(tm.screen(), "\n")
	y := len(lines) - 1
	i := strings.Index(lines[y], text)
	if i < 0 {
		t.Fatalf("clickStatus: %q not in status bar %q", text, lines[y])
	}
	x := utf8.RuneCountInString(lines[y][:i]) + 1 + utf8.RuneCountInString(text)/2
	tm.typ(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x, y+1, x, y+1))
}

// click sends a left click (SGR mouse encoding) on the first occurrence of
// text on screen.
func (tm *term) click(t *testing.T, text string) {
	t.Helper()
	for y, line := range strings.Split(tm.screen(), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			x := utf8.RuneCountInString(line[:i]) + 1
			tm.typ(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x, y+1, x, y+1))
			return
		}
	}
	t.Fatalf("click: %q not on screen:\n%s", text, tm.screen())
}

func (tm *term) screen() string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.emu.String()
}

func (tm *term) waitFor(t *testing.T, what string, ok func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ok(tm.screen()) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; screen:\n%s", what, tm.screen())
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

// daemonLog returns the test daemon's log for failure messages.
func daemonLog(dir string) string {
	logs, err := os.ReadFile(filepath.Join(dir, "otmux.log"))
	if err != nil {
		entries, _ := os.ReadDir(dir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return fmt.Sprintf("daemon log unreadable: %v (dir has %v)", err, names)
	}
	return "daemon log:\n" + string(logs)
}
